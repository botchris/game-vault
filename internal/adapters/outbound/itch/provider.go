package itch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of itch.io accounts.
const Type source.Type = "itch"

// Platform is the platform name of the copies, the one Humble's itch.io keys and the copy form use.
const Platform = "itch.io"

const (
	settingAPIKey  = "api_key"
	defaultBaseURL = "https://api.itch.io"

	// perPage is the largest page the API serves.
	perPage = 500

	// maxPages bounds a scan: 500 000 keys is far beyond any account, so reaching it means the API
	// keeps answering the same page.
	maxPages = 1000
)

var (
	// ErrNoKey means the API key setting is empty.
	ErrNoKey = errors.New("itch.io: paste an API key (see the help below the field)")

	// ErrBadKey means itch.io rejected the key.
	ErrBadKey = errors.New("itch.io did not accept the API key: create a new one in your itch.io settings (API keys) and paste it")
)

// Provider implements sync.Provider for itch.io accounts.
type Provider struct {
	// API calls api.itch.io; tests point it at a fake server.
	API *apiclient.Client
}

// NewProvider returns the itch.io source with its production endpoints.
func NewProvider() *Provider { return &Provider{API: apiclient.New(defaultBaseURL)} }

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "itch.io",
		DescriptionKey: "sources.itch.description",
		Fields: []source.Field{{
			Key:      settingAPIKey,
			LabelKey: "sources.itch.apiKey",
			HelpKey:  "sources.itch.apiKeyHelp",
			HelpURL:  "https://itch.io/user/settings/api-keys",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

// ownedKey is one entry of GET /profile/owned-keys: a download key the account holds, from a
// purchase (purchase_id set), a bundle, a claim of a free game or a gift.
type ownedKey struct {
	ID         int64     `json:"id"`
	GameID     int64     `json:"game_id"`
	PurchaseID int64     `json:"purchase_id"`
	CreatedAt  string    `json:"created_at"`
	Game       ownedGame `json:"game"`
}

// ownedGame is the game a key opens, as far as the importer reads it.
type ownedGame struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`

	// Classification is "game", "tool", "assets", "game_mod", "physical_game", "soundtrack",
	// "comic", "book" or "other"; missing is taken as a game rather than dropping it.
	Classification string `json:"classification"`
}

// page is one page of owned keys. The API signals errors with an errors list, sometimes with HTTP
// 200.
type page struct {
	OwnedKeys []ownedKey `json:"owned_keys"`
	Errors    []string   `json:"errors"`
}

// ownedKeys reads one page (from 1) of the account's keys.
func (p *Provider) ownedKeys(ctx context.Context, settings source.Settings, n, size int) ([]ownedKey, error) {
	key := strings.TrimSpace(settings[settingAPIKey])
	if key == "" {
		return nil, ErrNoKey
	}

	var out page

	err := p.API.Do(ctx, apiclient.Request{
		Path: "/profile/owned-keys",
		Query: url.Values{
			"page":     {strconv.Itoa(n)},
			"per_page": {strconv.Itoa(size)},
		},
		Header: http.Header{"Authorization": {"Bearer " + key}},
	}, &out)
	if apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
		return nil, ErrBadKey
	}

	if err != nil {
		return nil, fmt.Errorf("itch.io owned keys: %w", err)
	}

	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("itch.io owned keys: %s", strings.Join(out.Errors, "; "))
	}

	return out.OwnedKeys, nil
}

// Test implements sync.Provider: it reads one key.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, err := p.ownedKeys(ctx, settings, 1, 1)
	return err
}

// Fetch implements sync.Provider. The API documents that a short page is not the last one, so pages
// are read until one comes back empty.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	var keys []ownedKey

	for n := 1; ; n++ {
		if n > maxPages {
			return nil, nil, fmt.Errorf("itch.io owned keys: still answering after %d pages; try again later", maxPages)
		}

		got, err := p.ownedKeys(ctx, settings, n, perPage)
		if err != nil {
			return nil, nil, err
		}

		if len(got) == 0 {
			break
		}

		keys = append(keys, got...)
	}

	copies, warnings := mapKeys(keys)

	return copies, warnings, nil
}

// mapKeys turns keys into one library copy per game: a game bought twice, or bought and then
// claimed again in a bundle, is still one game in the library. The earliest key dates the copy.
// What is not a game (tools, assets, soundtracks, books…) is skipped and counted in a warning.
func mapKeys(keys []ownedKey) ([]game.ImportedCopy, []string) {
	var (
		copies  []game.ImportedCopy
		skipped int
	)

	index := map[int64]int{}

	for _, k := range keys {
		id := k.GameID
		if id == 0 {
			id = k.Game.ID
		}

		title := strings.TrimSpace(k.Game.Title)
		if id == 0 || title == "" || !isGame(k.Game.Classification) {
			skipped++
			continue
		}

		acquired := date(k.CreatedAt)

		if i, ok := index[id]; ok {
			if acquired != "" && (copies[i].Details.AcquiredOn == "" || acquired < copies[i].Details.AcquiredOn) {
				copies[i].Details.AcquiredOn = acquired
				copies[i].Details.Origin = origin(k)
			}

			continue
		}

		gameID := strconv.FormatInt(id, 10)
		index[id] = len(copies)
		copies = append(copies, game.ImportedCopy{
			ExternalID: "itch:" + gameID,
			Title:      title,
			Links:      game.Links{LinkedStore.Key: gameID},
			Details: game.CopyDetails{
				Kind:       game.KindLibrary,
				Platform:   Platform,
				Status:     game.StatusOwned,
				Origin:     origin(k),
				AcquiredOn: acquired,
			},
		})
	}

	var warnings []string
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d itch.io items were not imported: tools, assets, soundtracks, books and other things that are not games", skipped))
	}

	return copies, warnings
}

func isGame(classification string) bool {
	c := strings.TrimSpace(strings.ToLower(classification))
	return c == "" || c == "game"
}

// origin says how the key was obtained, as far as the API tells: only purchases are marked.
func origin(k ownedKey) string {
	if k.PurchaseID != 0 {
		return "itch.io purchase"
	}

	return "itch.io bundle, claim or gift"
}

// date reads the key's creation time. The API documents RFC 3339, but its older endpoints wrote
// "2006-01-02 15:04:05", so both are accepted.
func date(v string) game.Date {
	for _, layout := range []string{time.RFC3339, time.DateTime} {
		if t, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
			return game.DateOf(t)
		}
	}

	return ""
}
