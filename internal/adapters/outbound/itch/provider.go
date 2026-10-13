package itch

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of itch.io accounts.
const Type source.Type = "itch"

// Platform is the platform name of the copies, the one Humble's itch.io keys and the copy form use.
const Platform = game.PlatformItch

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
	// ErrNoCredentials means the API key setting is empty.
	ErrNoCredentials = errkind.New(errkind.Invalid, "itch.io: paste an API key (see the help below the field)")

	// ErrSignedOut means itch.io rejected the key.
	ErrSignedOut = source.SignedOut("itch.io did not accept the API key: create a new one in your itch.io settings (API keys) and paste it")
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
		Fields: []schema.Field{{
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
	OwnedKeys keyList  `json:"owned_keys"`
	Errors    []string `json:"errors"`
}

// keyList decodes owned_keys. itch.io's backend writes an empty list as an empty object ({}), so
// the empty page that ends every scan comes as one; an object with entries is read in key order.
type keyList []ownedKey

// UnmarshalJSON accepts a JSON array, an object of keys, or null.
func (l *keyList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		var list []ownedKey
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}

		*l = list

		return nil
	}

	var byIndex map[string]ownedKey
	if err := json.Unmarshal(b, &byIndex); err != nil {
		return err
	}

	indexes := make([]string, 0, len(byIndex))
	for k := range byIndex {
		indexes = append(indexes, k)
	}

	// Lua arrays become objects keyed "1", "2"…: numeric order, not string order ("10" after "9").
	slices.SortFunc(indexes, func(a, b string) int {
		na, errA := strconv.Atoi(a)
		nb, errB := strconv.Atoi(b)

		if errA == nil && errB == nil {
			return cmp.Compare(na, nb)
		}

		return strings.Compare(a, b)
	})

	list := make([]ownedKey, 0, len(indexes))
	for _, k := range indexes {
		list = append(list, byIndex[k])
	}

	*l = list

	return nil
}

// ownedKeys reads one page (from 1) of the account's keys.
func (p *Provider) ownedKeys(ctx context.Context, settings schema.Settings, n, size int) ([]ownedKey, error) {
	key := strings.TrimSpace(settings[settingAPIKey])
	if key == "" {
		return nil, ErrNoCredentials
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
		return nil, ErrSignedOut
	}

	if err != nil {
		// A 429, an outage, no answer or a web page is a passing trouble, not a refused key.
		return nil, fmt.Errorf("itch.io owned keys: %w", apiclient.ClassifyError(ctx, err))
	}

	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("itch.io owned keys: %s", strings.Join(out.Errors, "; "))
	}

	return out.OwnedKeys, nil
}

// Test implements sync.Provider: it reads one key.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.ownedKeys(ctx, settings, 1, 1)
	return err
}

// Fetch implements sync.Provider. The API documents that a short page is not the last one, so pages
// are read until one comes back empty.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
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
	var copies []game.ImportedCopy

	skipped := source.Skipped{Store: Platform}
	index := map[int64]int{}

	for _, k := range keys {
		id := k.GameID
		if id == 0 {
			id = k.Game.ID
		}

		title := strings.TrimSpace(k.Game.Title)
		if id == 0 || title == "" || !isGame(k.Game.Classification) {
			skipped.Add("tools, assets, soundtracks, books and other things that are not games")
			continue
		}

		acquired := game.ParseStoreDate(k.CreatedAt)

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

	return copies, skipped.Warnings()
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
