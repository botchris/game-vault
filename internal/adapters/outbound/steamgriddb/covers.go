// Package steamgriddb implements a cover provider backed by SteamGridDB
// (https://www.steamgriddb.com), a community database of game artwork, through its official API v2
// and the user's own API key, the way open-source launchers (Playnite, Heroic, Lutris) use it.
//
// SteamGridDB's Terms of Use allow only personal, non-commercial use "as permitted by the normal
// functionality of the Service" and forbid any automated use of the site or the service. The
// provider therefore starts disabled, needs the user to accept that risk explicitly (a consent
// setting) besides the key, is asked only on the cover fallback pass (after every other provider
// found nothing), and sends one request at a time, at most one a second.
package steamgriddb

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies SteamGridDB in the cover chain.
const ID provider.ID = "steamgriddb"

// TermsURL is SteamGridDB's Terms of Use, linked from the risk notice.
const TermsURL = "https://www.steamgriddb.com/terms"

const (
	settingConsent = "accept_risk"
	settingAPIKey  = "api_key"
	defaultBaseURL = "https://www.steamgriddb.com/api/v2"

	// keyURL is where a signed-in user generates an API key (Preferences → API).
	keyURL = "https://www.steamgriddb.com/profile/preferences/api"

	// maxCandidates is how many grids are proposed, highest score first.
	maxCandidates = 3

	// imageHost serves the grids and their thumbnails.
	imageHost = "cdn2.steamgriddb.com"
)

var (
	// ErrNotConfigured means the API key is missing or the risk was not accepted.
	ErrNotConfigured = provider.BadSettings("enter your SteamGridDB API key and accept the risk in the provider settings")

	// ErrBadKey means SteamGridDB rejected the API key (HTTP 401 or 403).
	ErrBadKey = provider.BadSettings("SteamGridDB rejected the API key: check it in the provider settings")
)

// errTooManyRequests is SteamGridDB's HTTP 429.
var errTooManyRequests = fmt.Errorf("HTTP %d: %w", http.StatusTooManyRequests, provider.ErrRateLimited)

// errNotFound is SteamGridDB's HTTP 404: it does not know the game (or the search found nothing).
var errNotFound = errors.New("not found")

// Covers implements media.CoverProvider with SteamGridDB's portrait grids (600×900, Steam's library
// capsule size, which is box art proportions).
type Covers struct {
	// API calls SteamGridDB's API v2; tests point it at a fake server.
	API *apiclient.Client

	// throttle keeps the requests one at a time and at least a second apart.
	throttle *throttle
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the provider with SteamGridDB's production endpoint.
func NewCovers() *Covers {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 20 * time.Second

	return &Covers{
		API:      api,
		throttle: newThrottle(minGap, time.Now, sleep),
	}
}

// Descriptor implements media.CoverProvider.
func (p *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ID,
		Kind:           provider.KindCover,
		Name:           "SteamGridDB",
		DescriptionKey: "providers.steamgriddb.description",
		Fields: schema.Fields{
			{
				Key:      settingConsent,
				LabelKey: "providers.steamgriddb.consent",
				HelpKey:  "providers.steamgriddb.consentHelp",
				HelpURL:  TermsURL,
				Kind:     schema.FieldConsent,
				Required: true,
			},
			{
				Key:      settingAPIKey,
				LabelKey: "providers.steamgriddb.apiKey",
				HelpKey:  "providers.steamgriddb.apiKeyHelp",
				HelpURL:  keyURL,
				Kind:     schema.FieldSecret,
				Required: true,
			},
		},
		// Disabled until the user accepts the risk and enters a key: its terms forbid automated use.
		// Last in the chain, after IGDB (85): community art is the last resort.
		DefaultOrder: 90,
	}
}

// Applies implements media.CoverProvider: only on the fallback pass, for any system, and never for
// a virtual PC edition (store links only): SteamGridDB's art is the community's, not a store's box.
// The key and the consent are checked by Covers, since a provider cannot be enabled without them.
func (p *Covers) Applies(q media.CoverQuery) bool { return q.Fallback && !q.LinksOnly }

// ImageHosts implements media.ImageHoster.
func (p *Covers) ImageHosts() []string { return []string{imageHost} }

// Test implements media.Provider with one search, the cheapest authenticated request.
func (p *Covers) Test(ctx context.Context, s schema.Settings) error {
	key, err := apiKey(s)
	if err != nil {
		return err
	}

	var out []gameJSON

	err = p.get(ctx, key, "/search/autocomplete/"+url.PathEscape("example"), nil, &out)
	if errors.Is(err, errNotFound) {
		return nil // the key was accepted; the search just found nothing
	}

	return err
}

// apiKey returns the settings' API key, provided the risk was accepted.
func apiKey(s schema.Settings) (string, error) {
	key := strings.TrimSpace(s[settingAPIKey])
	if key == "" || s[settingConsent] != schema.ConsentGiven {
		return "", ErrNotConfigured
	}

	return key, nil
}

// reAppID matches a Steam AppID.
var reAppID = regexp.MustCompile(`^[0-9]{1,10}$`)

// Covers implements media.CoverProvider. A game linked to Steam is looked up by its AppID; any other,
// or one SteamGridDB does not know by its AppID, by its title, which must match exactly one game.
func (p *Covers) Covers(ctx context.Context, q media.CoverQuery, s schema.Settings) ([]media.CoverCandidate, error) {
	key, err := apiKey(s)
	if err != nil {
		return nil, err
	}

	if appID := q.Links[game.LinkSteam]; reAppID.MatchString(appID) {
		grids, err := p.grids(ctx, key, "/grids/steam/"+appID)
		if !errors.Is(err, errNotFound) {
			if err != nil {
				return nil, err
			}

			return candidates(grids, ""), nil
		}
	}

	if q.LinksOnly {
		return nil, nil
	}

	g, err := p.findByTitle(ctx, key, q.Title)
	if err != nil || g == nil {
		return nil, err
	}

	grids, err := p.grids(ctx, key, "/grids/game/"+strconv.FormatInt(g.ID, 10))
	if errors.Is(err, errNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return candidates(grids, g.Name), nil
}

// gameJSON is one game of a search (/search/autocomplete/{term}), as the API documentation
// (openapi.yml, version 2.10.0) describes it.
type gameJSON struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Types    []string `json:"types"`
	Verified bool     `json:"verified"`
}

// gridJSON is one grid of /grids/steam/{appid} or /grids/game/{id}. The tag flags are filtered out
// by the query already; they are checked again in case the filter is ignored.
type gridJSON struct {
	ID       int64  `json:"id"`
	Score    int    `json:"score"`
	Style    string `json:"style"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	NSFW     bool   `json:"nsfw"`
	Humor    bool   `json:"humor"`
	Epilepsy bool   `json:"epilepsy"`
	URL      string `json:"url"`
	Thumb    string `json:"thumb"`
	Author   struct {
		Name string `json:"name"`
	} `json:"author"`
}

// envelope wraps every answer: {"success": true, "data": …}, or {"success": false, "errors": ["…"]}.
type envelope struct {
	Success bool            `json:"success"`
	Errors  []string        `json:"errors"`
	Data    json.RawMessage `json:"data"`
}

// gridQuery asks for static portrait grids (box art proportions), without the images tagged NSFW,
// humor or epilepsy.
var gridQuery = url.Values{
	"dimensions": {"600x900"},
	"types":      {"static"},
	"nsfw":       {"false"},
	"humor":      {"false"},
	"epilepsy":   {"false"},
}

// grids returns the grids of the game at path; errNotFound when SteamGridDB does not know it.
func (p *Covers) grids(ctx context.Context, key, path string) ([]gridJSON, error) {
	var out []gridJSON
	if err := p.get(ctx, key, path, gridQuery, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// findByTitle searches the game's title, without its edition and store tags (game.SearchTitle),
// then as it is when that finds no match, and keeps the one game whose name is that title, ignoring
// case and trademark signs. Two games with that name give nothing: a wrong box is worse than none.
func (p *Covers) findByTitle(ctx context.Context, key, title string) (*gameJSON, error) {
	terms := []string{game.SearchTitle(title)}
	if raw := game.NormalizeTitle(title); !strings.EqualFold(raw, terms[0]) {
		terms = append(terms, raw)
	}

	for _, term := range terms {
		if term == "" {
			continue
		}

		var out []gameJSON

		err := p.get(ctx, key, "/search/autocomplete/"+url.PathEscape(term), nil, &out)
		if errors.Is(err, errNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		if g := pick(out, term); g != nil {
			return g, nil
		}
	}

	return nil, nil
}

// pick returns the one game named term among the results, or nil when there is none or more than
// one.
func pick(games []gameJSON, term string) *gameJSON {
	var match *gameJSON

	for i, g := range games {
		if !sameTitle(g.Name, term) || (match != nil && match.ID == g.ID) {
			continue
		}

		if match != nil {
			return nil // ambiguous
		}

		match = &games[i]
	}

	return match
}

// sameTitle compares titles ignoring case, trademark signs and repeated spaces.
func sameTitle(a, b string) bool {
	return strings.EqualFold(game.NormalizeTitle(a), game.NormalizeTitle(b))
}

// candidates turns grids into cover candidates: portrait ones only, highest score first, at most
// maxCandidates. title is SteamGridDB's name of the game, when known.
func candidates(grids []gridJSON, title string) []media.CoverCandidate {
	grids = slices.DeleteFunc(slices.Clone(grids), func(g gridJSON) bool {
		return g.URL == "" || g.NSFW || g.Humor || g.Epilepsy || (g.Width > 0 && g.Height > 0 && g.Width >= g.Height)
	})
	slices.SortStableFunc(grids, func(a, b gridJSON) int { return cmp.Compare(b.Score, a.Score) })

	out := make([]media.CoverCandidate, 0, min(len(grids), maxCandidates))
	for _, g := range grids[:min(len(grids), maxCandidates)] {
		// The label says plainly that this is community art, not the publisher's box, and credits
		// its author.
		label := "SteamGridDB: community art"
		if name := strings.TrimSpace(g.Author.Name); name != "" {
			label += " by " + name
		}

		thumb := g.Thumb
		if thumb == "" {
			thumb = g.URL
		}

		out = append(out, media.CoverCandidate{
			URL:      g.URL,
			ThumbURL: thumb,
			Label:    label,
			Title:    title,
			Provider: ID,
		})
	}

	return out
}

// get sends one GET request with the key, through the throttle, and decodes the answer's data into
// out. It maps SteamGridDB's failures to the errors the media service understands (see mapError).
func (p *Covers) get(ctx context.Context, key, path string, query url.Values, out any) error {
	return p.throttle.do(ctx, func() error {
		var env envelope

		err := p.API.Do(ctx, apiclient.Request{
			Method: http.MethodGet,
			Path:   path,
			Query:  query,
			Header: http.Header{"Authorization": {"Bearer " + key}},
		}, &env)
		if err := mapError(ctx, err, env); err != nil {
			return err
		}

		if len(env.Data) == 0 {
			return nil
		}

		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("SteamGridDB: unexpected answer (the service may have changed): %w; if it persists, report this message", err)
		}

		return nil
	})
}

// mapError turns the result of one request into the error the provider returns: a rejected key is
// a settings problem, a 429 a rate limit, an outage (5xx, no answer, a web page instead of data)
// provider.ErrUnavailable, and a 404 errNotFound.
func mapError(ctx context.Context, err error, env envelope) error {
	var se *apiclient.StatusError

	switch {
	case err == nil && env.Success:
		return nil
	case err == nil:
		// A 200 with "success": false: keep SteamGridDB's own words so the user can report them.
		return fmt.Errorf("SteamGridDB refused the request (%s): try again later; if it persists, report this message",
			strings.Join(env.Errors, ", "))
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, apiclient.ErrNotJSON):
		// Cloudflare's checks and maintenance pages are HTML: an outage, not an answer about the game.
		return fmt.Errorf("SteamGridDB answered with a web page instead of data: try again later: %w", provider.ErrUnavailable)
	case !errors.As(err, &se):
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("SteamGridDB is not answering, try again later (%w): %w", err, provider.ErrUnavailable)
		}

		// The answer could not be decoded: the API may have changed.
		return fmt.Errorf("SteamGridDB: %w; if it persists, report this message", err)
	case se.Status == http.StatusUnauthorized, se.Status == http.StatusForbidden:
		return ErrBadKey
	case se.Status == http.StatusNotFound:
		return errNotFound
	case se.Status == http.StatusTooManyRequests:
		return errTooManyRequests
	case se.Status >= 500:
		return fmt.Errorf("SteamGridDB is not answering (HTTP %d): try again later: %w", se.Status, provider.ErrUnavailable)
	}

	return fmt.Errorf("SteamGridDB: HTTP %d: %s: try again later; if it persists, report this message", se.Status, se.Body)
}
