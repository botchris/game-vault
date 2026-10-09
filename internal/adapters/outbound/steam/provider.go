// Package steam implements the Steam source provider using the official Steam Web API.
package steam

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
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Steam accounts.
const Type source.Type = "steam"

const (
	settingAPIKey  = "api_key"
	settingProfile = "profile"
	defaultBaseURL = "https://api.steampowered.com"
)

var (
	reProfileURL = regexp.MustCompile(`steamcommunity\.com/(?:id|profiles)/([^/?#]+)`)
	reSteamID64  = regexp.MustCompile(`^7656\d{13}$`)

	// ErrPrivateProfile means Steam returned no games, almost always because of privacy settings.
	ErrPrivateProfile = errors.New("steam returned no games: set 'Game details' to Public in your profile privacy settings")
)

// Provider implements sync.Provider for Steam libraries.
type Provider struct {
	BaseURL string
	Client  *http.Client
}

// NewProvider returns the Steam source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{BaseURL: defaultBaseURL, Client: &http.Client{Timeout: 60 * time.Second}}
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Steam",
		DescriptionKey: "sources.steam.description",
		Fields: []source.Field{
			{Key: settingAPIKey, LabelKey: "sources.steam.apiKey", HelpKey: "sources.steam.apiKeyHelp",
				HelpURL: "https://steamcommunity.com/dev/apikey", Kind: source.FieldSecret, Required: true},
			{Key: settingProfile, LabelKey: "sources.steam.profile", HelpKey: "sources.steam.profileHelp",
				Kind: source.FieldText, Required: true},
		},
	}
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	key := strings.TrimSpace(settings[settingAPIKey])

	id, err := p.resolveSteamID(ctx, key, settings[settingProfile])
	if err != nil {
		return nil, nil, err
	}

	var out struct {
		Response struct {
			Games []struct {
				AppID int64  `json:"appid"`
				Name  string `json:"name"`
			} `json:"games"`
		} `json:"response"`
	}

	q := url.Values{
		"key": {key}, "steamid": {id}, "include_appinfo": {"1"},
		"include_played_free_games": {"1"}, "skip_unvetted_apps": {"0"}, "format": {"json"},
	}
	if err := p.get(ctx, "/IPlayerService/GetOwnedGames/v1/", q, &out); err != nil {
		return nil, nil, err
	}

	if len(out.Response.Games) == 0 {
		return nil, nil, ErrPrivateProfile
	}

	copies := make([]game.ImportedCopy, 0, len(out.Response.Games))
	for _, g := range out.Response.Games {
		name := g.Name
		if name == "" {
			name = fmt.Sprintf("Steam app %d", g.AppID)
		}

		copies = append(copies, game.ImportedCopy{
			ExternalID: fmt.Sprintf("steam:%d", g.AppID),
			Title:      name,
			Links:      game.Links{LinkedStore.Key: strconv.FormatInt(g.AppID, 10)},
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam", Status: game.StatusOwned, Origin: "Steam"},
		})
	}

	return copies, nil, nil
}

// Test implements sync.Provider with one light request: the number of owned games, without their
// details. It checks the key, the profile and that "Game details" are public.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	key := strings.TrimSpace(settings[settingAPIKey])

	id, err := p.resolveSteamID(ctx, key, settings[settingProfile])
	if err != nil {
		return err
	}

	var out struct {
		Response struct {
			GameCount int `json:"game_count"`
		} `json:"response"`
	}
	if err := p.get(ctx, "/IPlayerService/GetOwnedGames/v1/", url.Values{"key": {key}, "steamid": {id}, "format": {"json"}}, &out); err != nil {
		return err
	}

	if out.Response.GameCount == 0 {
		return ErrPrivateProfile
	}

	return nil
}

// resolveSteamID accepts a SteamID64, a vanity name or a profile URL.
func (p *Provider) resolveSteamID(ctx context.Context, key, profile string) (string, error) {
	profile = strings.TrimRight(strings.TrimSpace(profile), "/")
	if m := reProfileURL.FindStringSubmatch(profile); m != nil {
		profile = m[1]
	}

	if reSteamID64.MatchString(profile) {
		return profile, nil
	}

	var out struct {
		Response struct {
			SteamID string `json:"steamid"`
			Success int    `json:"success"`
		} `json:"response"`
	}
	if err := p.get(ctx, "/ISteamUser/ResolveVanityURL/v1/", url.Values{"key": {key}, "vanityurl": {profile}}, &out); err != nil {
		return "", err
	}

	if out.Response.Success != 1 {
		return "", fmt.Errorf("steam profile %q not found", profile)
	}

	return out.Response.SteamID, nil
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

	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return fmt.Errorf("steam rejected the API key (HTTP %d)", res.StatusCode)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("steam %s: HTTP %d", path, res.StatusCode)
	}

	return json.NewDecoder(res.Body).Decode(out)
}
