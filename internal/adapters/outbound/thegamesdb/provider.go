// Package thegamesdb implements a cover provider backed by TheGamesDB (https://thegamesdb.net),
// which has platform-specific box art: the Xbox 360 case for an Xbox 360 copy, the PS3 case for
// a PS3 copy, and so on.
//
// The API key has a monthly request allowance, so the provider only applies to games that
// Steam cannot cover (physical copies or games without a Steam AppID), and every answer is cached
// by the media service.
package thegamesdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/ratelimit"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies this provider in settings and provider chains.
const ID provider.ID = "thegamesdb"

const (
	settingAPIKey  = "api_key"
	defaultBaseURL = "https://api.thegamesdb.net"
	maxCandidates  = 6
)

// platformNames maps every Game Vault system to its TheGamesDB platform names. Names are compared
// normalised, against both TheGamesDB's name and its alias.
var platformNames = map[string][]string{
	game.SystemPC:                  {"PC"},
	game.SystemPS5:                 {"Sony Playstation 5"},
	game.SystemPS4:                 {"Sony Playstation 4"},
	game.SystemPS3:                 {"Sony Playstation 3"},
	game.SystemPS2:                 {"Sony Playstation 2"},
	game.SystemPS1:                 {"Sony Playstation"},
	game.SystemPSVita:              {"Sony Playstation Vita"},
	game.SystemPSP:                 {"Sony Playstation Portable", "Sony PSP"},
	game.SystemXboxSeries:          {"Microsoft Xbox Series X", "Microsoft Xbox Series X|S"},
	game.SystemXboxOne:             {"Microsoft Xbox One"},
	game.SystemXbox360:             {"Microsoft Xbox 360"},
	game.SystemXbox:                {"Microsoft Xbox"},
	"Switch 2":                     {"Nintendo Switch 2"},
	game.SystemSwitch:              {"Nintendo Switch"},
	game.SystemWiiU:                {"Nintendo Wii U"},
	game.SystemWii:                 {"Nintendo Wii"},
	game.SystemGameCube:            {"Nintendo GameCube"},
	game.SystemN64:                 {"Nintendo 64"},
	"SNES":                         {"Super Nintendo (SNES)"},
	"Famicom Disk System":          {"Famicom Disk System"},
	"NES":                          {"Nintendo Entertainment System (NES)"},
	game.System3DS:                 {"Nintendo 3DS"},
	game.SystemDS:                  {"Nintendo DS"},
	"Game Boy Advance":             {"Nintendo Game Boy Advance"},
	"Pokémon Mini":                 {"Nintendo Pokémon Mini"},
	"Game Boy Color":               {"Nintendo Game Boy Color"},
	"Virtual Boy":                  {"Nintendo Virtual Boy"},
	game.SystemGameBoy:             {"Nintendo Game Boy"},
	"Game & Watch":                 {"Game & Watch"},
	"Dreamcast":                    {"Sega Dreamcast"},
	"32X":                          {"Sega 32X"},
	"Saturn":                       {"Sega Saturn"},
	"Pico":                         {"Sega Pico"},
	"Mega-CD / Sega CD":            {"Sega CD"},
	"Mega Drive / Genesis":         {"Sega Genesis", "Sega Mega Drive"},
	"Master System":                {"Sega Master System"},
	"SG-1000":                      {"SEGA SG-1000"},
	"Game Gear":                    {"Sega Game Gear"},
	"Atari Jaguar CD":              {"Atari Jaguar CD"},
	"Atari Jaguar":                 {"Atari Jaguar"},
	"Atari 7800":                   {"Atari 7800"},
	"Atari 5200":                   {"Atari 5200"},
	"Atari 2600":                   {"Atari 2600"},
	"Atari Lynx":                   {"Atari Lynx"},
	"Atari ST":                     {"Atari ST"},
	"Atari 8-bit":                  {"Atari 800", "Atari XE"},
	"PC-FX":                        {"PC-FX"},
	"PC Engine CD / TurboGrafx-CD": {"TurboGrafx CD"},
	"PC Engine / TurboGrafx-16":    {"TurboGrafx 16"},
	"PC-98":                        {"PC-98"},
	"PC-88":                        {"PC-88"},
	"Neo Geo CD":                   {"Neo Geo CD"},
	"Neo Geo AES":                  {"Neo Geo"},
	"Neo Geo Pocket Color":         {"Neo Geo Pocket Color"},
	"Neo Geo Pocket":               {"Neo Geo Pocket"},
	"Playdia":                      {"Playdia"},
	"WonderSwan Color":             {"WonderSwan Color"},
	"WonderSwan":                   {"WonderSwan"},
	"Acorn Archimedes":             {"Acorn Archimedes"},
	"Acorn Electron":               {"Acorn Electron"},
	"BBC Micro":                    {"BBC Micro"},
	"Amstrad GX4000":               {"Amstrad GX4000"},
	"Amstrad CPC":                  {"Amstrad CPC"},
	"Apple Pippin":                 {"Apple Pippin"},
	"Apple II":                     {"Apple II"},
	"Evercade":                     {"Evercade"},
	"ColecoVision":                 {"Colecovision"},
	"Amiga CD32":                   {"Amiga CD32"},
	"Amiga":                        {"Amiga"},
	"Commodore 16 / Plus/4":        {"Commodore 16", "Commodore Plus/4"},
	"Commodore 64":                 {"Commodore 64", "Commodore 128"},
	"VIC-20":                       {"Commodore VIC-20"},
	"Commodore PET":                {"Commodore PET"},
	"Dragon 32/64":                 {"Dragon 32/64"},
	"Channel F":                    {"Fairchild Channel F"},
	"FM Towns":                     {"FM Towns Marty"},
	"Fujitsu FM-7":                 {"Fujitsu FM-7"},
	"Vectrex":                      {"Vectrex"},
	"MSX":                          {"MSX"},
	"Odyssey 2":                    {"Magnavox Odyssey 2"},
	"Magnavox Odyssey":             {"Magnavox Odyssey 1"},
	"HyperScan":                    {"HyperScan"},
	"Intellivision":                {"Intellivision"},
	"N-Gage":                       {"N-Gage"},
	"Ouya":                         {"Ouya"},
	"3DO":                          {"3DO"},
	"Playdate":                     {"Playdate"},
	"CD-i":                         {"Philips CD-i"},
	"Sharp X68000":                 {"Sharp X68000"},
	"Sharp X1":                     {"Sharp X1"},
	"Sinclair QL":                  {"Sinclair QL"},
	"ZX Spectrum":                  {"Sinclair ZX Spectrum"},
	"ZX81":                         {"Sinclair ZX81"},
	"Game.com":                     {"Game.com"},
	"R-Zone":                       {"R-Zone"},
	"V.Smile":                      {"V.Smile"},
	"Nuon":                         {"Nuon"},
	"Casio Loopy":                  {"Casio Loopy"},
	"LaserActive":                  {"Pioneer LaserActive"},
	"Super Cassette Vision":        {"Epoch Super Cassette Vision"},
	"Emerson Arcadia 2001":         {"Emerson Arcadia 2001"},
	"Epoch Cassette Vision":        {"Epoch Cassette Vision"},
	"Interton VC 4000":             {"Interton VC 4000"},
	"Bally Astrocade":              {"Bally Astrocade"},
	"Gizmondo":                     {"Gizmondo"},
	"Tapwave Zodiac":               {"Tapwave Zodiac"},
	"Mega Duck":                    {"Mega Duck"},
	"Watara Supervision":           {"Watara Supervision"},
	"Gamate":                       {"Gamate"},
	"Microvision":                  {"Milton Bradley Microvision"},
	"Thomson MO5":                  {"Thomson M05"},
	"Texas Instruments TI-99/4A":   {"Texas Instruments TI-99/4A"},
	"TRS-80 Color Computer":        {"TRS-80 Color Computer"},
}

// unmappedSystems are the catalog systems TheGamesDB has no platform of its own for.
var unmappedSystems = []string{"Nintendo 64DD", "SuperGrafx", "Mac (classic)", "Commodore CDTV", "MSX2"}

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func norm(s string) string { return reNonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// Provider implements media.CoverProvider.
type Provider struct {
	// API calls TheGamesDB; tests point it at a fake server.
	API *apiclient.Client

	// throttle spaces the requests (see minGap); a Provider built without New sends them at once.
	throttle *ratelimit.Throttle

	// now is the clock; nil means time.Now.
	now func() time.Time

	// allowance remembers a used-up monthly allowance, so no request is sent until it renews.
	allowance allowance

	// platformsFlight shares one platform list request among the lookups that need it at once.
	platformsFlight singleflight.Group

	mu        sync.Mutex
	platforms map[string]int64 // normalised name or alias → TheGamesDB platform id

	// platformsFailed is when the platform list last failed; zero when it has not.
	platformsFailed time.Time
}

var (
	_ media.CoverProvider = (*Provider)(nil)
)

// New returns the TheGamesDB cover provider with its production endpoints.
func New() *Provider {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 20 * time.Second

	return &Provider{
		API:      api,
		throttle: newThrottle(minGap, nil, nil),
		now:      time.Now,
	}
}

// Descriptor implements media.CoverProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ID,
		Kind:           provider.KindCover,
		Name:           "TheGamesDB",
		SettingsGroup:  string(ID),
		DescriptionKey: "providers.thegamesdb.description",
		Fields: schema.Fields{{
			Key:      settingAPIKey,
			LabelKey: "providers.thegamesdb.apiKey",
			HelpKey:  "providers.thegamesdb.apiKeyHelp",
			HelpURL:  "https://forums.thegamesdb.net/viewforum.php?f=10",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
		DefaultOrder: 10,
	}
}

// ImageHosts implements media.ImageHoster: TheGamesDB's CDN rejects hotlinked images.
func (p *Provider) ImageHosts() []string { return []string{"cdn.thegamesdb.net"} }

// Applies keeps the monthly allowance for physical copies, games no store knows, and store games no
// store had art for (the fallback pass).
func (p *Provider) Applies(q media.GameQuery) bool {
	return !q.LinksOnly && (q.HasPhysical() || !q.HasStoreLink() || q.Fallback) // it searches by title
}

// apiError builds an error for a non-200 answer, using TheGamesDB's own message when present
// (e.g. "Invalid API key was provided."). A rejected key (provider.ErrBadSettings) and a server
// error (provider.ErrUnavailable) are problems of the provider, not of a game: they become its
// status instead of a warning on every sheet.
func apiError(se *apiclient.StatusError) error {
	var body struct {
		Status string `json:"status"`
	}
	// Best effort: without a readable message the status code alone describes the error.
	_ = json.Unmarshal([]byte(se.Body), &body)

	switch {
	case se.Status == http.StatusTooManyRequests: // its body only says "Unknown Error Code"
		return errTooManyRequests
	case se.Status == http.StatusForbidden || se.Status == http.StatusUnauthorized ||
		strings.Contains(strings.ToLower(body.Status), "api key"):
		msg := fmt.Sprintf("the API key was rejected (HTTP %d): check it in the settings", se.Status)
		if body.Status != "" {
			msg = fmt.Sprintf("%s (HTTP %d): check the API key in the settings", body.Status, se.Status)
		}

		return provider.BadSettings(msg)
	case se.Status >= 500:
		return fmt.Errorf("not answering (HTTP %d): try again later: %w", se.Status, provider.ErrUnavailable)
	case body.Status != "":
		return fmt.Errorf("%s (HTTP %d)", body.Status, se.Status)
	}

	return fmt.Errorf("HTTP %d", se.Status)
}

// errTooManyRequests is TheGamesDB's HTTP 429: a burst, or a used-up allowance (see allowance).
// Like the other errors here it does not name TheGamesDB: the warnings that show it already do.
var errTooManyRequests = fmt.Errorf("HTTP %d: %w", http.StatusTooManyRequests, provider.ErrRateLimited)

// get calls TheGamesDB through the throttle, so a burst of lookups never reaches it at once. The
// answer's error is translated inside the throttle, which backs off after a rate-limit answer.
// While the key's allowance is used up nothing is sent, not even queued: the lookup gets the
// allowance error at once. An HTTP 429 checks, still holding the throttle's turn, whether that is
// its cause, so lookups that queued meanwhile learn the answer when their turn comes; one refused
// by the throttle (busy, or resting) while the allowance is used up gets the allowance error
// instead.
func (p *Provider) get(ctx context.Context, path string, q url.Values, out any) error {
	key := q.Get("apikey")
	if err := p.usedUp(key); err != nil {
		return err
	}

	err := p.throttle.Do(ctx, func() error {
		// A lookup that queued while the allowance was being checked learns the result here.
		if err := p.usedUp(key); err != nil {
			return err
		}

		err := p.getNow(ctx, path, q, out)
		if errors.Is(err, errTooManyRequests) {
			if found := p.checkAllowance(ctx, key); found != nil {
				return found
			}
		}

		return err
	})
	if errors.Is(err, ratelimit.ErrBusy) || errors.Is(err, ratelimit.ErrResting) {
		if used := p.usedUp(key); used != nil {
			return used
		}
	}

	return err
}

// getNow calls TheGamesDB at once, bypassing the throttle.
func (p *Provider) getNow(ctx context.Context, path string, q url.Values, out any) error {
	err := p.API.Get(ctx, path, q, out)

	var se *apiclient.StatusError

	switch {
	case errors.As(err, &se):
		return apiError(se)
	case err == nil, errors.Is(err, apiclient.ErrNotJSON):
		return err
	}

	// No answer (no connection, a timeout) is an outage that says nothing about the game, unless
	// the caller gave up (canceled, or its deadline passed): then the error stays the caller's, and
	// the throttle starts no back-off for it.
	classified := apiclient.ClassifyError(ctx, err)
	if ctx.Err() != nil || errors.Is(classified, provider.ErrUnavailable) {
		return classified
	}

	// An answer that could not be read is taken for an outage too, as it always was.
	return fmt.Errorf("not answering, try again later (%w): %w", err, provider.ErrUnavailable)
}

// ErrUnknownKey means TheGamesDB does not know the API key. It matches provider.ErrBadSettings, so
// Test sets the provider's "settings" status as a lookup refused for the key does.
var ErrUnknownKey = provider.BadSettings("the API key was not recognized: check it in the provider settings")

// Test implements providers.Impl by reading the remaining allowance; that endpoint does not consume
// it. It answers 200 even for unknown keys, with no allowance and a zero refresh timer, which is how
// those are detected. A valid key that used up its allowance still has a refresh timer. The check
// bypasses the throttle: the user is waiting for it, and it must not queue behind cover lookups.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
	key := s[settingAPIKey]

	out, err := p.limit(ctx, key)
	if err != nil {
		return err
	}

	// What the check found also holds for lookups: allowance bought after it ran out lets them
	// through again at once.
	now := p.clock()
	p.note(key, out, now)

	return out.verdict(now)
}

// ErrNoAllowance means the key works but this month's requests are used up. Test returns it as an
// error that also matches provider.ErrQuotaExhausted and says when the allowance renews.
var ErrNoAllowance = errkind.New(errkind.Unavailable, "the key has used this month's allowance: covers from it resume when the month renews")

// platformsRetry is how long lookups search every platform after the platform list failed, before
// asking for it again.
const platformsRetry = 5 * time.Minute

// platformIDs resolves Game Vault platform names to TheGamesDB ids, fetching the platform list
// once per process. Without the list (it failed, or the caller gave up waiting) it returns none,
// and the lookup searches every platform rather than fail.
func (p *Provider) platformIDs(ctx context.Context, apiKey string, names []string) []int64 {
	table := p.platformTable(ctx, apiKey)

	var ids []int64

	for _, n := range names {
		for _, candidate := range append(platformNames[n], n) {
			if id, ok := table[norm(candidate)]; ok {
				ids = append(ids, id)
				break
			}
		}
	}

	return ids
}

// platformTable returns the platform list by normalised name and alias, or nil without it. No lock
// is held while it is fetched: the request waits for its turn in the throttle, and lookups queued
// behind a lock would escape the throttle's limits on waiting. Lookups that need it meanwhile share
// the one request and stop waiting when their context ends. A failed list is not asked for again
// for platformsRetry, so an outage does not cost a request before every search.
func (p *Provider) platformTable(ctx context.Context, apiKey string) map[string]int64 {
	p.mu.Lock()
	table, failed := p.platforms, p.platformsFailed
	p.mu.Unlock()

	if table != nil || !failed.IsZero() && p.clock().Sub(failed) < platformsRetry {
		return table
	}

	// The request runs to its end even when the lookup that started it gives up: others may wait
	// for it, and the throttle and the client's timeout bound it.
	fetchCtx := context.WithoutCancel(ctx)
	ch := p.platformsFlight.DoChan("platforms", func() (any, error) {
		return p.fetchPlatforms(fetchCtx, apiKey), nil
	})

	select {
	case r := <-ch:
		table, _ := r.Val.(map[string]int64)

		return table
	case <-ctx.Done():
		return nil
	}
}

// fetchPlatforms asks TheGamesDB for its platform list and remembers it, or remembers that it
// failed. A request the throttle refused (busy, or resting after a 429) was never sent, so it is not
// remembered as a failure: the next lookup may ask again.
func (p *Provider) fetchPlatforms(ctx context.Context, apiKey string) map[string]int64 {
	var out struct {
		Data struct {
			Platforms map[string]struct {
				ID    int64  `json:"id"`
				Name  string `json:"name"`
				Alias string `json:"alias"`
			} `json:"platforms"`
		} `json:"data"`
	}

	err := p.get(ctx, "/v1/Platforms", url.Values{"apikey": {apiKey}}, &out)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err != nil {
		if !errors.Is(err, ratelimit.ErrBusy) && !errors.Is(err, ratelimit.ErrResting) {
			p.platformsFailed = p.clock()
		}

		return nil
	}

	table := map[string]int64{}
	for _, pl := range out.Data.Platforms {
		table[norm(pl.Name)] = pl.ID
		table[norm(pl.Alias)] = pl.ID
	}

	p.platforms = table

	return table
}

type searchResponse struct {
	Data struct {
		Games []struct {
			ID          int64  `json:"id"`
			Title       string `json:"game_title"`
			ReleaseDate string `json:"release_date"`
			Platform    int64  `json:"platform"`
		} `json:"games"`
	} `json:"data"`
	Include struct {
		Boxart struct {
			BaseURL map[string]string `json:"base_url"`
			Data    map[string][]struct {
				Side     string `json:"side"`
				Filename string `json:"filename"`
			} `json:"data"`
		} `json:"boxart"`
		Platform struct {
			Data map[string]struct {
				Name string `json:"name"`
			} `json:"data"`
		} `json:"platform"`
	} `json:"include"`
}

// Covers searches the game by title on the edition's system (or, without one, the platforms of its
// physical copies or all its platforms) and returns the front box art of the game itself: exact
// title matches first, then its other releases (same game.MatchKey). The picker (q.Browse) also
// gets the other games the search found, last. The game's name is searched first, then the raw
// title (game.SearchTerms) unless the name found a game titled exactly like the copy: stripping
// tags can go too far ("Gold Rush!" searched as "Rush!" finds "Rush", which only shares the
// MatchKey), so an exact title from the raw search wins over the name's looser match. A second
// request only for a title with tags, when its name gave no exact match.
func (p *Provider) Covers(ctx context.Context, q media.GameQuery, s schema.Settings) ([]media.CoverCandidate, error) {
	key := s[settingAPIKey]
	filter := p.platformFilter(ctx, key, q.SearchSystems())

	var best []media.CoverCandidate

	for _, term := range game.SearchTerms(q.Title) {
		params := url.Values{"apikey": {key}, "name": {term}, "include": {"boxart,platform"}}
		if filter != "" {
			params.Set("filter[platform]", filter)
		}

		var out searchResponse
		if err := p.get(ctx, "/v1.1/Games/ByGameName", params, &out); err != nil {
			return nil, err
		}

		found := coversOf(out, q)
		if len(found) > 0 && game.SameTitle(found[0].Title, q.Title) {
			return found, nil // exact matches come first
		}

		if best == nil {
			best = found
		}
	}

	return best, nil
}

// platformFilter returns the filter[platform] value of a search on systems: TheGamesDB's ids of
// them, comma separated, or empty to search every platform.
func (p *Provider) platformFilter(ctx context.Context, key string, systems []string) string {
	ids := p.platformIDs(ctx, key, systems)
	parts := make([]string, len(ids))

	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}

	return strings.Join(parts, ",")
}

// coversOf returns the front box art of the games in a search for q, best matches first.
func coversOf(out searchResponse, q media.GameQuery) []media.CoverCandidate {
	base := out.Include.Boxart.BaseURL

	large, thumb := base["large"], base["thumb"]
	if large == "" {
		large = base["original"]
	}

	if thumb == "" {
		thumb = large
	}

	var strict, exact, others []media.CoverCandidate

	for _, g := range out.Data.Games {
		for _, art := range out.Include.Boxart.Data[strconv.FormatInt(g.ID, 10)] {
			if art.Side != "front" {
				continue
			}

			label := g.Title
			if pl := out.Include.Platform.Data[strconv.FormatInt(g.Platform, 10)].Name; pl != "" {
				label += " · " + pl
			}

			if len(g.ReleaseDate) >= 4 {
				label += " · " + g.ReleaseDate[:4]
			}

			c := media.CoverCandidate{
				URL:      large + art.Filename,
				ThumbURL: thumb + art.Filename,
				Label:    label,
				Title:    g.Title,
				Provider: ID,
			}
			switch {
			case game.SameTitle(g.Title, q.Title):
				strict = append(strict, c)
			case game.SameMatchKey(g.Title, q.Title):
				exact = append(exact, c) // same game, other release: "[Platinum]", "(PAL)"...
			default:
				others = append(others, c)
			}

			break
		}
	}

	// Other games the search found ("Halo 3: ODST" for "Halo 3") are never the game: the automatic
	// resolution would store the first one as its box. Only the picker, where the user chooses,
	// lists them after the game's own releases.
	all := append(strict, exact...)
	if q.Browse {
		all = append(all, others...)
	}

	return all[:min(len(all), maxCandidates)]
}
