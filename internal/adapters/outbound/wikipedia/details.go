// Package wikipedia implements a metadata provider backed by Wikidata and Wikipedia: the game's
// item on Wikidata gives its developers, publishers, release date, genres and its articles, and the
// article in the UI language (else English) gives the summary. Both are free and need no key; the
// Wikimedia User-Agent policy asks for a descriptive agent with contact information, and
// Wikipedia's texts are CC BY-SA, so the summary carries a credit linking to the article.
package wikipedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/ratelimit"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// DetailsID identifies Wikipedia in the metadata chain.
const DetailsID provider.ID = "wikipedia-details"

const (
	defaultWikidataURL  = "https://www.wikidata.org"
	defaultWikipediaURL = "https://{lang}.wikipedia.org"

	// requestGap spaces the requests: Wikimedia asks API clients to be polite and not to send
	// requests in parallel.
	requestGap = 200 * time.Millisecond

	// maxWait is the longest a request waits for its turn; longer queues fail as rate-limited, so a
	// library page opening many sheets does not hang (each sheet takes several requests).
	maxWait = 10 * time.Second

	// backoff is how long the Wikimedia servers are left alone after they answered HTTP 429 or a
	// server error: asking again at once only prolongs the refusal.
	backoff = 30 * time.Second

	// license is the license of Wikipedia's texts, shown with the summary and linked to licenseURL.
	license    = "CC BY-SA 4.0"
	licenseURL = "https://creativecommons.org/licenses/by-sa/4.0/"

	// videoGame is Wikidata's "video game" class (Q7889): only its instances are games.
	videoGame = "Q7889"

	maxCandidates = 10
)

// Wikidata properties read from a game's item.
const (
	propInstanceOf  = "P31"
	propPlatform    = "P400"
	propDeveloper   = "P178"
	propPublisher   = "P123"
	propPublication = "P577"
	propGenre       = "P136"
)

// platformItems maps Game Vault systems to the Wikidata items of their platforms (checked against
// Wikidata on 2026-10-11). PC covers Windows, macOS, Linux and the generic "personal computer".
var platformItems = map[string][]string{
	game.SystemPC:         {"Q1406", "Q14116", "Q388", "Q16338"},
	game.SystemPS5:        {"Q63184502"},
	game.SystemPS4:        {"Q5014725"},
	game.SystemPS3:        {"Q10683"},
	game.SystemPS2:        {"Q10680"},
	game.SystemPS1:        {"Q10677"},
	game.SystemPSP:        {"Q170325"},
	game.SystemPSVita:     {"Q188808"},
	game.SystemXboxSeries: {"Q98973368", "Q64513817", "Q98967383"},
	game.SystemXboxOne:    {"Q13361286"},
	game.SystemXbox360:    {"Q48263"},
	game.SystemXbox:       {"Q132020"},
	game.SystemSwitch:     {"Q19610114"},
	game.SystemWiiU:       {"Q56942"},
	game.SystemWii:        {"Q8079"},
	game.SystemGameCube:   {"Q182172"},
	game.SystemN64:        {"Q184839"},
	game.System3DS:        {"Q203597"},
	game.SystemDS:         {"Q170323"},
	game.SystemGameBoy:    {"Q186437"},
}

// Details implements media.MetadataProvider with Wikidata and Wikipedia.
type Details struct {
	// Wikidata calls Wikidata's API (https://www.wikidata.org); tests point it at a fake server.
	Wikidata *apiclient.Client

	// Wikipedia calls Wikipedia's API; "{lang}" in its BaseURL is replaced by the article's language
	// (https://{lang}.wikipedia.org).
	Wikipedia *apiclient.Client

	// throttle sends one request at a time, spaced; nil sends them at once.
	throttle *ratelimit.Throttle
}

var _ media.MetadataProvider = (*Details)(nil)

// New returns the provider with Wikimedia's production endpoints. version is Game Vault's, for the
// User-Agent.
func New(version string) *Details {
	agent := "GameVault/" + version + " (https://github.com/botchris/game-vault)"

	wikidata := apiclient.New(defaultWikidataURL)
	wikidata.HTTP.Timeout = 20 * time.Second
	wikidata.UserAgent = agent

	wikipedia := apiclient.New(defaultWikipediaURL)
	wikipedia.HTTP.Timeout = 20 * time.Second
	wikipedia.UserAgent = agent

	return &Details{
		Wikidata:  wikidata,
		Wikipedia: wikipedia,
		throttle:  newThrottle(requestGap),
	}
}

// Descriptor implements media.MetadataProvider.
func (d *Details) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               DetailsID,
		Kind:             provider.KindMetadata,
		Name:             "Wikipedia",
		DescriptionKey:   "providers.wikipediaDetails.description",
		EnabledByDefault: true,
		DefaultOrder:     12, // after Steam (10), before IGDB (15) and TheGamesDB (20)
	}
}

// Applies implements media.MetadataProvider. A game linked to Steam gets Steam's localized sheet,
// whose summary always wins, so Wikipedia's 4-5 requests would be wasted on it (the background scan
// asks for every game): it only helps there on the fallback pass, when Steam had nothing.
func (d *Details) Applies(q media.GameQuery) bool {
	return q.Links[game.LinkSteam] == "" || q.Fallback
}

// Test implements media.Provider with one search on Wikidata.
func (d *Details) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := d.search(ctx, "Portal", "en"); err != nil {
		return fmt.Errorf("could not reach Wikidata, check the connection and try again: %w", err)
	}

	return nil
}

var reLanguage = regexp.MustCompile(`^[a-z]{2,3}$`)

// wikiLanguage returns the Wikipedia language of a UI language: its primary subtag ("pt-BR" → "pt",
// "zh-Hans" → "zh"). The language names a host and a site, so anything but a plain code is English.
func wikiLanguage(language string) string {
	primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(language)), "-")
	primary, _, _ = strings.Cut(primary, "_")

	if !reLanguage.MatchString(primary) {
		return "en"
	}

	return primary
}

// Details implements media.MetadataProvider.
func (d *Details) Details(ctx context.Context, q media.GameQuery, language string, _ schema.Settings) (*media.GameDetails, error) {
	lang := wikiLanguage(language)

	item, err := d.findGame(ctx, q, lang)
	if err != nil || item == nil {
		return nil, err
	}

	det := &media.GameDetails{ReleaseDate: item.releaseDate()}

	labels, err := d.labels(ctx, item.refs(propDeveloper, propPublisher, propGenre), lang)
	if err != nil {
		return nil, err
	}

	det.Developers = pick(labels, item.refs(propDeveloper))
	det.Publishers = pick(labels, item.refs(propPublisher))

	for _, g := range pick(labels, item.refs(propGenre)) {
		det.Genres = append(det.Genres, capitalize(g))
	}

	if err := d.summary(ctx, item, lang, det); err != nil {
		return nil, err
	}

	return det, nil
}

// findGame returns the Wikidata item of the game q describes, or nil when none matches clearly:
// items titled q.Title that are video games, of which exactly one is on one of the game's systems
// (or, with no system known or none on Wikidata, exactly one at all). A disambiguation must never
// yield the wrong game.
func (d *Details) findGame(ctx context.Context, q media.GameQuery, lang string) (*item, error) {
	ids, exact, err := d.candidates(ctx, q.Title, lang)
	if err != nil || len(ids) == 0 {
		return nil, err
	}

	items, err := d.entities(ctx, ids, "claims|sitelinks", lang)
	if err != nil {
		return nil, err
	}

	var games, exactGames []*item

	for _, id := range ids {
		if it, ok := items[id]; ok && slices.Contains(it.refs(propInstanceOf), videoGame) {
			games = append(games, it)

			if exact[id] {
				exactGames = append(exactGames, it)
			}
		}
	}

	// A game with exactly the title wins over those that only match loosely ("Mafia" over "Mafia:
	// Definitive Edition", which game.MatchKey takes for the same).
	if len(exactGames) > 0 {
		games = exactGames
	}

	var wanted []string
	for _, system := range q.SearchSystems() {
		wanted = append(wanted, platformItems[system]...)
	}

	if len(wanted) > 0 {
		var on []*item

		for _, g := range games {
			if slices.ContainsFunc(g.refs(propPlatform), func(p string) bool { return slices.Contains(wanted, p) }) {
				on = append(on, g)
			}
		}

		switch {
		case len(on) == 1:
			return on[0], nil
		case len(on) > 1:
			return nil, nil
		}

		// None is on the game's systems: a lone game whose platforms Wikidata does not list is
		// still the one; one listed on other systems may be another game with that title.
		if len(games) == 1 && len(games[0].refs(propPlatform)) == 0 {
			return games[0], nil
		}

		return nil, nil
	}

	if len(games) == 1 {
		return games[0], nil
	}

	return nil, nil
}

// candidates searches Wikidata for items labeled (or also known as) the game's name, in English and
// in the UI language, and keeps those whose matched name is the title: exactly (ignoring case, accents
// and punctuation, reported in exact) or as game.MatchKey compares titles. The name searched is the title
// without edition and store tags (game.SearchTitle: Wikidata labels "Example West", not the
// "GOTY" disc); only when it finds no candidate is the raw title searched too.
func (d *Details) candidates(ctx context.Context, title, lang string) (ids []string, exact map[string]bool, err error) {
	langs := []string{"en"}
	if lang != "en" {
		langs = append(langs, lang)
	}

	terms := game.SearchTerms(title)

	exact = map[string]bool{}

	for _, term := range terms {
		for _, l := range langs {
			hits, err := d.search(ctx, term, l)
			if err != nil {
				return nil, nil, err
			}

			for _, h := range hits {
				strict := game.SameTitle(h.Label, title) || game.SameTitle(h.Match.Text, title)
				if !strict && !game.SameMatchKey(h.Label, title) && !game.SameMatchKey(h.Match.Text, title) {
					continue
				}

				if strict {
					exact[h.ID] = true
				}

				if !slices.Contains(ids, h.ID) && len(ids) < maxCandidates {
					ids = append(ids, h.ID)
				}
			}
		}

		if len(ids) > 0 {
			break
		}
	}

	return ids, exact, nil
}

// searchHit is one item wbsearchentities found.
type searchHit struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Match struct {
		Text string `json:"text"`
	} `json:"match"`
}

// search runs wbsearchentities: items whose label or alias matches text in a language.
func (d *Details) search(ctx context.Context, text, lang string) ([]searchHit, error) {
	var out struct {
		apiError

		Search []searchHit `json:"search"`
	}

	err := d.get(ctx, d.Wikidata, url.Values{
		"action":   {"wbsearchentities"},
		"search":   {text},
		"language": {lang},
		"uselang":  {lang},
		"type":     {"item"},
		"limit":    {"10"},
	}, &out, &out.apiError)

	return out.Search, err
}

// item is a Wikidata item: the claims, labels and sitelinks asked for.
type item struct {
	ID     string                            `json:"id"`
	Labels map[string]struct{ Value string } `json:"labels"`
	Claims map[string][]claim                `json:"claims"`
	Links  map[string]struct {
		Title string `json:"title"`
	} `json:"sitelinks"`
}

// claim is one statement of an item.
type claim struct {
	Rank     string `json:"rank"`
	Mainsnak struct {
		Datavalue struct {
			Value json.RawMessage `json:"value"`
		} `json:"datavalue"`
	} `json:"mainsnak"`
}

// refs returns the items the properties' statements point to, in order and without duplicates.
// Deprecated statements are wrong by Wikidata's own account and are skipped.
func (it *item) refs(props ...string) []string {
	var ids []string

	for _, p := range props {
		for _, c := range it.Claims[p] {
			var v struct {
				ID string `json:"id"`
			}
			if c.Rank == "deprecated" || json.Unmarshal(c.Mainsnak.Datavalue.Value, &v) != nil || v.ID == "" {
				continue
			}

			if !slices.Contains(ids, v.ID) {
				ids = append(ids, v.ID)
			}
		}
	}

	return ids
}

// releaseDate returns the earliest publication date, as precise as Wikidata knows it ("2011-04-18",
// "2011-04" or "2011"). Dates are compared at the precision they share, and on a tie the more
// precise one wins: a bare "2011" never hides "2011-04-19".
func (it *item) releaseDate() string {
	best := ""

	for _, c := range it.Claims[propPublication] {
		var v struct {
			Time      string `json:"time"`
			Precision int    `json:"precision"`
		}
		// Times look like "+2011-04-19T00:00:00Z"; years before 1000 or BC are not games.
		if c.Rank == "deprecated" || json.Unmarshal(c.Mainsnak.Datavalue.Value, &v) != nil || len(v.Time) < 11 || v.Time[0] != '+' {
			continue
		}

		var date string

		switch t := v.Time[1:11]; {
		case v.Precision >= 11:
			date = t
		case v.Precision == 10:
			date = t[:7]
		case v.Precision == 9:
			date = t[:4]
		default:
			continue
		}

		if best == "" {
			best = date
			continue
		}

		n := min(len(date), len(best))
		if date[:n] < best[:n] || (date[:n] == best[:n] && len(date) > len(best)) {
			best = date
		}
	}

	return best
}

// entities runs wbgetentities for up to 50 ids, with labels in the UI language, English and "mul"
// (the language-neutral label Wikidata uses for names), and the two Wikipedia sitelinks.
func (d *Details) entities(ctx context.Context, ids []string, props, lang string) (map[string]*item, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var out struct {
		apiError

		Entities map[string]*item `json:"entities"`
	}

	err := d.get(ctx, d.Wikidata, url.Values{
		"action":     {"wbgetentities"},
		"ids":        {strings.Join(ids[:min(len(ids), 50)], "|")},
		"props":      {props},
		"languages":  {strings.Join(labelLanguages(lang), "|")},
		"sitefilter": {sitelink(lang) + "|enwiki"},
	}, &out, &out.apiError)

	return out.Entities, err
}

// labels returns the label of each item, in the UI language, else English, else "mul".
func (d *Details) labels(ctx context.Context, ids []string, lang string) (map[string]string, error) {
	items, err := d.entities(ctx, ids, "labels", lang)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for id, it := range items {
		for _, l := range labelLanguages(lang) {
			if v := it.Labels[l].Value; v != "" {
				out[id] = v
				break
			}
		}
	}

	return out, nil
}

// summary fills det's summary from the game's article in the UI language, else in English: its
// introduction in plain text, credited to the article. A disambiguation page is no summary.
func (d *Details) summary(ctx context.Context, it *item, lang string, det *media.GameDetails) error {
	site, title := lang, it.Links[sitelink(lang)].Title
	if title == "" {
		site, title = "en", it.Links["enwiki"].Title
	}

	if title == "" {
		return nil
	}

	wiki := *d.Wikipedia
	wiki.BaseURL = strings.ReplaceAll(wiki.BaseURL, "{lang}", site)

	var out struct {
		apiError

		Query struct {
			Pages []struct {
				Extract   string          `json:"extract"`
				FullURL   string          `json:"fullurl"`
				Missing   bool            `json:"missing"`
				PageProps json.RawMessage `json:"pageprops"`
			} `json:"pages"`
		} `json:"query"`
	}

	err := d.get(ctx, &wiki, url.Values{
		"action":        {"query"},
		"prop":          {"extracts|info|pageprops"},
		"ppprop":        {"disambiguation"},
		"exintro":       {"1"},
		"explaintext":   {"1"},
		"inprop":        {"url"},
		"redirects":     {"1"},
		"titles":        {title},
		"formatversion": {"2"},
	}, &out, &out.apiError)
	if err != nil {
		return err
	}

	for _, p := range out.Query.Pages {
		if p.Missing || p.Extract == "" || strings.Contains(string(p.PageProps), "disambiguation") {
			continue
		}

		det.Summary = paragraphs(p.Extract)
		det.SummarySource = media.Attribution{
			Name:       "Wikipedia",
			URL:        p.FullURL,
			License:    license,
			LicenseURL: licenseURL,
		}

		return nil
	}

	return nil
}

var reBreaks = regexp.MustCompile(`\s*\n\s*`)

// paragraphs turns an extract's line breaks (one per paragraph) into the blank lines the sheet
// separates paragraphs with.
func paragraphs(s string) string { return reBreaks.ReplaceAllString(strings.TrimSpace(s), "\n\n") }

// apiError is MediaWiki's error, which comes with HTTP 200.
type apiError struct {
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

// get calls a MediaWiki API (api.php) through the throttle and decodes the answer into out, turning the
// API's own error (apiErr, part of out) into an error.
func (d *Details) get(ctx context.Context, c *apiclient.Client, q url.Values, out any, apiErr *apiError) error {
	q.Set("format", "json")

	sent := false

	err := d.throttle.Do(ctx, func() error {
		sent = true

		return c.Get(ctx, "/w/api.php", q, out)
	})
	if !sent && ctx.Err() != nil {
		return fmt.Errorf("%w: %w", errCanceled, ctx.Err())
	}

	switch {
	case apiclient.IsStatus(err, http.StatusTooManyRequests):
		return fmt.Errorf("HTTP %d: %w", http.StatusTooManyRequests, provider.ErrRateLimited)
	case apiclient.IsStatus(err, http.StatusForbidden):
		// A refused client (blocked or without a User-Agent) is the provider's problem, not the game's.
		return fmt.Errorf("the Wikimedia servers refused the request (HTTP %d): try again later; if it persists, report this message: %w",
			http.StatusForbidden, provider.ErrUnavailable)
	case err == nil:
	case errors.Is(err, provider.ErrRateLimited), errors.Is(err, context.Canceled), errors.Is(err, errCanceled):
		return err
	default:
		var se *apiclient.StatusError
		if errors.As(err, &se) && se.Status < 500 {
			return err
		}

		// A server error or no answer says nothing about the game.
		return fmt.Errorf("the Wikimedia servers are not answering, try again later (%w): %w", err, provider.ErrUnavailable)
	}

	switch {
	case apiErr.Error == nil:
		return nil
	case apiErr.Error.Code == "maxlag" || apiErr.Error.Code == "ratelimited":
		// The servers are lagging or the client is too fast: like a rate limit, ask again later.
		return fmt.Errorf("%s: %w", apiErr.Error.Info, provider.ErrRateLimited)
	}

	return fmt.Errorf("%s (%s): try again later; if it persists, report this message", apiErr.Error.Info, apiErr.Error.Code)
}

// errCanceled is returned when the context ends while a request waits for its turn.
var errCanceled = errors.New("the lookup was canceled while waiting for its turn")

// newThrottle returns the throttle of Wikimedia's requests with the given gap: one at a time.
func newThrottle(gap time.Duration) *ratelimit.Throttle {
	return ratelimit.New(ratelimit.Config{
		Gap:       gap,
		MaxWait:   maxWait,
		Backoff:   backoff,
		BackoffOn: serverTrouble,
	})
}

// serverTrouble reports whether a request's error is the servers asking to slow down or failing (a
// 429, a 5xx, or no answer at all: a dropped connection, a timeout), which starts the throttle's
// back-off. It sees the request's own error, a *apiclient.StatusError whose Retry-After the
// throttle honors. A request whose caller gave up also gets no answer, but the throttle starts no
// back-off for it.
func serverTrouble(err error) bool {
	var se *apiclient.StatusError
	if errors.As(err, &se) {
		return apiclient.Transient(se.Status)
	}

	var ue *url.Error

	return errors.As(err, &ue)
}

// labelLanguages are the languages labels are looked for in, best first.
func labelLanguages(lang string) []string {
	if lang == "en" {
		return []string{"en", "mul"}
	}

	return []string{lang, "en", "mul"}
}

// sitelink names the Wikipedia of a language ("es" → "eswiki").
func sitelink(lang string) string { return lang + "wiki" }

// pick returns the labels of ids that have one, in order.
func pick(labels map[string]string, ids []string) []string {
	var out []string

	for _, id := range ids {
		if l := labels[id]; l != "" {
			out = append(out, l)
		}
	}

	return out
}

// capitalize upper-cases the first letter: Wikidata labels genres in lower case ("puzzle video
// game"), the sheet shows them like the other providers' ("Puzzle video game").
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}

	return string(unicode.ToUpper(r)) + s[size:]
}
