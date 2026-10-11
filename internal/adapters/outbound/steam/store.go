package steam

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

const (
	defaultStoreURL  = "https://store.steampowered.com"
	defaultCDNURL    = "https://cdn.cloudflare.steamstatic.com"
	defaultAPIURL    = "https://api.steampowered.com"
	defaultAssetsURL = "https://shared.akamai.steamstatic.com/store_item_assets/"
)

// LinkedStore is Steam as a store games are linked to by AppID.
var LinkedStore = game.Store{
	Key:     game.LinkSteam,
	Name:    "Steam",
	PageURL: StorePageURL + "{id}",
}

// StorePageURL is the address of a Steam store page, without the AppID.
const StorePageURL = "https://store.steampowered.com/app/"

// AppIDOf returns the Steam AppID the game is linked to, or 0 when it is not (or the link is not a
// valid AppID).
func AppIDOf(q media.CoverQuery) int64 {
	id, err := strconv.ParseInt(q.Links[game.LinkSteam], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}

	return id
}

// CoverProviderID identifies Steam in the cover provider chain.
const CoverProviderID provider.ID = "steam"

// Store uses Steam's public store endpoints (no API key needed). It implements
// media.CoverProvider and media.LinkSearcher.
type Store struct {
	// Site calls the store's own JSON endpoints (search, app details) on store.steampowered.com.
	Site *apiclient.Client

	// API calls IStoreBrowseService on api.steampowered.com, which knows the real (hashed) image
	// paths.
	API *apiclient.Client

	// CDNURL serves the legacy predictable image paths.
	CDNURL string

	// AssetsURL is the CDN prefix the hashed paths are relative to.
	AssetsURL string

	// throttle paces the requests to the store (see storeGap); nil sends them at once.
	throttle *throttle

	// now is the clock of the search cache; nil means time.Now.
	now func() time.Time

	mu sync.Mutex

	// searches caches recent store searches by query (see searchTTL).
	searches map[string]cachedSearch
}

// searchTTL is how long a store search's answer is reused. Within one cover lookup the same title
// may be searched twice (Battle.net's covers, then Steam's title search on the fallback pass): the
// second one costs nothing. Short enough that the user's own link searches stay current.
const searchTTL = 10 * time.Minute

// maxSearches bounds the search cache.
const maxSearches = 500

// cachedSearch is a store search's answer and when it was received.
type cachedSearch struct {
	matches []media.LinkMatch
	at      time.Time
}

var (
	_ media.CoverProvider = (*Store)(nil)
	_ media.LinkSearcher  = (*Store)(nil)
)

// Descriptor implements media.CoverProvider.
func (s *Store) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "Steam",
		DescriptionKey:   "providers.steam.description",
		EnabledByDefault: true,
		DefaultOrder:     20,
	}
}

// ImageHosts implements media.ImageHoster.
func (s *Store) ImageHosts() []string {
	return []string{"cdn.cloudflare.steamstatic.com", "shared.cloudflare.steamstatic.com",
		"cdn.akamai.steamstatic.com", "shared.akamai.steamstatic.com"}
}

// Applies reports whether the query is for a PC edition (or names no system) of a game linked to
// Steam, or, on the fallback pass, of any game: Steam's store is then searched by title. The search
// waits for that pass so it is only spent on PC editions no other provider had art for (Steam allows
// about 200 store requests per 5 minutes).
func (s *Store) Applies(q media.CoverQuery) bool {
	return q.ForPC() && (AppIDOf(q) != 0 || q.Fallback)
}

// Covers returns the portrait library art first, then the landscape header. Recent apps keep
// their images under hashed paths (…/apps/<id>/<hash>/library_600x900.jpg), so the real paths are
// asked to IStoreBrowseService (one free request); the legacy predictable URLs are the fallback.
func (s *Store) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	appID := AppIDOf(q)
	if appID == 0 {
		id, err := s.appIDByTitle(ctx, q.Title)
		if err != nil || id == 0 {
			return nil, err
		}

		appID = id
	}

	urls := s.CoverURLs(appID)
	if library, header, err := s.assetURLs(ctx, appID); err == nil && (library != "" || header != "") {
		urls = []string{library, header}
	}

	var out []media.CoverCandidate

	for i, u := range urls {
		if u == "" {
			continue
		}

		label := "Steam · library"
		if i == 1 {
			label = "Steam · header"
		}

		out = append(out, media.CoverCandidate{
			URL:      u,
			ThumbURL: u,
			Label:    label,
			Title:    q.Title,
			Provider: CoverProviderID,
		})
	}

	return out, nil
}

// appIDByTitle searches the store for title and returns the AppID of the only app with exactly that
// title (ignoring case and trademark signs), or, when none has it, of the only one with the same
// game.MatchKey; 0 when there is none or several. MatchKey alone would take "Mafia: Definitive
// Edition" for "Mafia". The AppID is only used for this cover: it is never written as the game's
// link, so a wrong guess cannot stick. An answer that says nothing about the game (429, 5xx, no
// connection) counts as rate-limited, so the edition is not marked missing for days.
func (s *Store) appIDByTitle(ctx context.Context, title string) (int64, error) {
	matches, err := s.SearchLinks(ctx, title)

	var se *apiclient.StatusError

	switch {
	case err == nil:
	case errors.Is(err, provider.ErrRateLimited):
		return 0, err
	case errors.As(err, &se) && se.Status != http.StatusTooManyRequests && se.Status < 500:
		return 0, err
	default:
		return 0, fmt.Errorf("steam store search is not answering, asking again later (%w): %w", err, provider.ErrRateLimited)
	}

	for _, same := range []func(string) bool{
		func(name string) bool { return strings.EqualFold(plainTitle(name), plainTitle(title)) },
		func(name string) bool { return game.MatchKey(name) == game.MatchKey(title) },
	} {
		var ids []int64

		for _, m := range matches {
			id, err := strconv.ParseInt(m.ID, 10, 64)
			if err != nil || id <= 0 || !same(m.Name) || slices.Contains(ids, id) {
				continue
			}

			ids = append(ids, id)
		}

		switch len(ids) {
		case 0:
			continue
		case 1:
			return ids[0], nil
		default:
			return 0, nil // two games with that title: neither is used
		}
	}

	return 0, nil
}

// plainTitle drops trademark signs and repeated spaces, which stores add or leave out.
func plainTitle(s string) string {
	s = strings.NewReplacer("™", "", "®", "", "©", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// Test implements media.Provider: it asks Steam's store browse API for the art of Portal 2 (app
// 620), so only transport errors and HTTP errors (including rate limits) fail it.
func (s *Store) Test(ctx context.Context, _ schema.Settings) error {
	if _, _, err := s.assetURLs(ctx, 620); err != nil {
		return fmt.Errorf("could not reach Steam, check the connection and try again: %w", err)
	}

	return nil
}

// assetURLs resolves the library (portrait) and header images of an app.
func (s *Store) assetURLs(ctx context.Context, appID int64) (library, header string, err error) {
	input := fmt.Sprintf(`{"ids":[{"appid":%d}],"context":{"language":"english","country_code":"US"},"data_request":{"include_assets":true}}`, appID)

	var out struct {
		Response struct {
			StoreItems []struct {
				Assets struct {
					Format         string `json:"asset_url_format"` // "steam/apps/<id>/${FILENAME}?t=…"
					LibraryCapsule string `json:"library_capsule"`
					Header         string `json:"header"`
				} `json:"assets"`
			} `json:"store_items"`
		} `json:"response"`
	}

	// The Web API (api.steampowered.com) has its own, much higher limits than the store host: not
	// throttled, so a fresh library's Steam covers fill in at once.
	if err := s.API.Get(ctx, "/IStoreBrowseService/GetItems/v1/", url.Values{"input_json": {input}}, &out); err != nil {
		return "", "", fmt.Errorf("steam store items: %w", err)
	}

	if len(out.Response.StoreItems) == 0 {
		return "", "", nil
	}

	a := out.Response.StoreItems[0].Assets
	if a.Format == "" {
		return "", "", nil
	}

	build := func(file string) string {
		if file == "" {
			return ""
		}

		return s.AssetsURL + strings.Replace(a.Format, "${FILENAME}", file, 1)
	}

	return build(a.LibraryCapsule), build(a.Header), nil
}

// NewStore returns the Steam store client with its production endpoints.
func NewStore() *Store {
	site, api := apiclient.New(defaultStoreURL), apiclient.New(defaultAPIURL)
	site.HTTP.Timeout, api.HTTP.Timeout = 15*time.Second, 15*time.Second

	return &Store{
		Site:      site,
		API:       api,
		CDNURL:    defaultCDNURL,
		AssetsURL: defaultAssetsURL,
		throttle:  newThrottle(storeGap, storeBurst, storeMaxWait, time.Now, sleep),
		now:       time.Now,
	}
}

// CoverURLs returns Steam's portrait and header art URLs for an app.
func (s *Store) CoverURLs(appID int64) []string {
	return []string{
		fmt.Sprintf("%s/steam/apps/%d/library_600x900.jpg", s.CDNURL, appID),
		fmt.Sprintf("%s/steam/apps/%d/header.jpg", s.CDNURL, appID),
	}
}

// LinkStore implements media.LinkSearcher.
func (s *Store) LinkStore() game.Store { return LinkedStore }

// SearchLinks implements media.LinkSearcher: it searches the Steam store by title. Answers are reused
// for searchTTL.
func (s *Store) SearchLinks(ctx context.Context, query string) ([]media.LinkMatch, error) {
	now := time.Now
	if s.now != nil {
		now = s.now
	}

	// The year Battle.net drops ("… (2016)"), case and spaces do not change the answer: one key, so
	// Battle.net's search and Steam's title search of one game share it.
	query = searchTerm(query)

	s.mu.Lock()
	hit, ok := s.searches[strings.ToLower(query)]
	s.mu.Unlock()

	if ok && now().Sub(hit.at) < searchTTL {
		return slices.Clone(hit.matches), nil
	}

	matches, err := s.search(ctx, query)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.searches == nil || len(s.searches) >= maxSearches {
		s.searches = map[string]cachedSearch{} // simplest bound: start over
	}

	s.searches[strings.ToLower(query)] = cachedSearch{
		matches: matches,
		at:      now(),
	}
	s.mu.Unlock()

	return slices.Clone(matches), nil
}

var reYearSuffix = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

// searchTerm is what the store is searched for: the query trimmed, without a trailing year in
// parentheses.
func searchTerm(query string) string {
	return strings.Join(strings.Fields(reYearSuffix.ReplaceAllString(query, "")), " ")
}

// search asks the store's search for query, through the throttle.
func (s *Store) search(ctx context.Context, query string) ([]media.LinkMatch, error) {
	q := url.Values{"term": {query}, "l": {"english"}, "cc": {"US"}}

	var out struct {
		Items []struct {
			Type      string `json:"type"`
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			TinyImage string `json:"tiny_image"`
		} `json:"items"`
	}

	err := s.throttle.do(ctx, func() error { return s.Site.Get(ctx, "/api/storesearch/", q, &out) })
	if errors.Is(err, errBusy) {
		return nil, err
	}

	if err != nil {
		return nil, fmt.Errorf("steam store search: %w", err)
	}

	matches := make([]media.LinkMatch, 0, len(out.Items))
	for _, it := range out.Items {
		if it.Type == "app" {
			matches = append(matches, media.LinkMatch{
				ID:       strconv.FormatInt(it.ID, 10),
				Name:     it.Name,
				ImageURL: it.TinyImage,
			})
		}
	}

	return matches, nil
}
