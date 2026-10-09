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
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gamevault/internal/application/media"
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

// platformNames maps Game Vault platform names to TheGamesDB platform names. Names are compared
// normalised, against both TheGamesDB's name and its alias.
var platformNames = map[string][]string{
	"PC":          {"PC"},
	"PS5":         {"Sony Playstation 5"},
	"PS4":         {"Sony Playstation 4"},
	"PS3":         {"Sony Playstation 3"},
	"PS2":         {"Sony Playstation 2"},
	"PS1":         {"Sony Playstation"},
	"PSP":         {"Sony Playstation Portable", "Sony PSP"},
	"PS Vita":     {"Sony Playstation Vita"},
	"Xbox Series": {"Microsoft Xbox Series X", "Microsoft Xbox Series X|S"},
	"Xbox One":    {"Microsoft Xbox One"},
	"Xbox 360":    {"Microsoft Xbox 360"},
	"Xbox":        {"Microsoft Xbox"},
	"Switch":      {"Nintendo Switch"},
	"Wii U":       {"Nintendo Wii U"},
	"Wii":         {"Nintendo Wii"},
	"GameCube":    {"Nintendo GameCube"},
	"N64":         {"Nintendo 64"},
	"3DS":         {"Nintendo 3DS"},
	"DS":          {"Nintendo DS"},
	"Game Boy":    {"Nintendo Game Boy"},
}

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func norm(s string) string { return reNonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// Provider implements media.CoverProvider.
type Provider struct {
	BaseURL string
	Client  *http.Client

	mu        sync.Mutex
	platforms map[string]int64 // normalised name or alias → TheGamesDB platform id
}

var (
	_ media.CoverProvider = (*Provider)(nil)
)

// New returns the TheGamesDB cover provider with its production endpoints.
func New() *Provider {
	return &Provider{BaseURL: defaultBaseURL, Client: &http.Client{Timeout: 20 * time.Second}}
}

// Descriptor implements media.CoverProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: ID, Kind: provider.KindCover, Name: "TheGamesDB", SettingsGroup: string(ID),
		DescriptionKey: "providers.thegamesdb.description",
		Fields: schema.Fields{{
			Key: settingAPIKey, LabelKey: "providers.thegamesdb.apiKey", HelpKey: "providers.thegamesdb.apiKeyHelp",
			HelpURL: "https://forums.thegamesdb.net/viewforum.php?f=10", Kind: schema.FieldSecret, Required: true,
		}},
	}
}

// ImageHosts implements media.ImageHoster: TheGamesDB's CDN rejects hotlinked images.
func (p *Provider) ImageHosts() []string { return []string{"cdn.thegamesdb.net"} }

// Applies keeps the monthly allowance for physical copies, games no store knows, and store games no
// store had art for (the fallback pass).
func (p *Provider) Applies(q media.CoverQuery) bool {
	return q.HasPhysical() || !q.HasStoreLink() || q.Fallback
}

// apiError builds an error for a non-200 answer, using TheGamesDB's own message when present
// (e.g. "Invalid API key was provided.").
func apiError(res *http.Response) error {
	var body struct {
		Status string `json:"status"`
	}
	// Best effort: without a readable message the status code alone describes the error.
	_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body)

	switch {
	case body.Status != "":
		return fmt.Errorf("TheGamesDB: %s (HTTP %d)", body.Status, res.StatusCode)
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("TheGamesDB rejected the API key or the monthly allowance is used up (HTTP %d)", res.StatusCode)
	}

	return fmt.Errorf("TheGamesDB: HTTP %d", res.StatusCode)
}

func (p *Provider) get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}

	res, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return apiError(res)
	}

	return json.NewDecoder(res.Body).Decode(out)
}

// ErrUnknownKey means TheGamesDB does not know the API key.
var ErrUnknownKey = errors.New("TheGamesDB did not recognize the API key")

// Test implements media.Provider by reading the remaining allowance; that endpoint does not consume
// it. It answers 200 even for unknown keys, with no allowance and no refresh timer, which is how
// those are detected. A valid key that used up its allowance still has a refresh timer.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
	var out struct {
		Remaining *int `json:"remaining_monthly_allowance"`
		Extra     int  `json:"extra_allowance"`
		Refresh   int  `json:"allowance_refresh_timer"`
	}
	if err := p.get(ctx, "/v1/API/Limit", url.Values{"apikey": {s[settingAPIKey]}}, &out); err != nil {
		return err
	}

	if out.Remaining == nil || (*out.Remaining+out.Extra == 0 && out.Refresh == 0) {
		return ErrUnknownKey
	}

	if *out.Remaining+out.Extra == 0 {
		return ErrNoAllowance
	}

	return nil
}

// ErrNoAllowance means the key works but this month's requests are used up.
var ErrNoAllowance = errors.New("TheGamesDB key has used this month's allowance: covers from it resume when the month renews")

// platformIDs resolves Game Vault platform names to TheGamesDB ids, fetching the platform list
// once per process.
func (p *Provider) platformIDs(ctx context.Context, apiKey string, names []string) []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.platforms == nil {
		var out struct {
			Data struct {
				Platforms map[string]struct {
					ID    int64  `json:"id"`
					Name  string `json:"name"`
					Alias string `json:"alias"`
				} `json:"platforms"`
			} `json:"data"`
		}
		if err := p.get(ctx, "/v1/Platforms", url.Values{"apikey": {apiKey}}, &out); err != nil {
			return nil // search without a platform filter rather than fail
		}

		p.platforms = map[string]int64{}
		for _, pl := range out.Data.Platforms {
			p.platforms[norm(pl.Name)] = pl.ID
			p.platforms[norm(pl.Alias)] = pl.ID
		}
	}

	var ids []int64

	for _, n := range names {
		for _, candidate := range append(platformNames[n], n) {
			if id, ok := p.platforms[norm(candidate)]; ok {
				ids = append(ids, id)
				break
			}
		}
	}

	return ids
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

// Covers searches the game by title on the platforms of its physical copies (or all its
// platforms) and returns front box art, exact title matches first.
func (p *Provider) Covers(ctx context.Context, q media.CoverQuery, s schema.Settings) ([]media.CoverCandidate, error) {
	key := s[settingAPIKey]

	platforms := q.PhysicalPlatforms
	if len(platforms) == 0 {
		platforms = q.Platforms
	}

	params := url.Values{"apikey": {key}, "name": {q.Title}, "include": {"boxart,platform"}}
	if ids := p.platformIDs(ctx, key, platforms); len(ids) > 0 {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.FormatInt(id, 10)
		}

		params.Set("filter[platform]", strings.Join(parts, ","))
	}

	var out searchResponse
	if err := p.get(ctx, "/v1.1/Games/ByGameName", params, &out); err != nil {
		return nil, err
	}

	base := out.Include.Boxart.BaseURL

	large, thumb := base["large"], base["thumb"]
	if large == "" {
		large = base["original"]
	}

	if thumb == "" {
		thumb = large
	}

	want, wantStrict := game.MatchKey(q.Title), strictKey(q.Title)

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

			c := media.CoverCandidate{URL: large + art.Filename, ThumbURL: thumb + art.Filename, Label: label, Title: g.Title, Provider: ID}
			switch {
			case strictKey(g.Title) == wantStrict:
				strict = append(strict, c)
			case game.MatchKey(g.Title) == want:
				exact = append(exact, c) // same game, other release: "[Platinum]", "(PAL)"...
			default:
				others = append(others, c)
			}

			break
		}
	}

	all := append(append(strict, exact...), others...)

	return all[:min(len(all), maxCandidates)], nil
}

// strictKey compares titles ignoring case and punctuation only, so "Call of Duty: Modern Warfare 2"
// equals "Call Of Duty Modern Warfare 2" but not "... (Platinum)".
func strictKey(title string) string { return norm(title) }
