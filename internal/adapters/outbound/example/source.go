package example

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Example Store accounts. It is stored with every source, so it never
// changes.
const Type source.Type = "example"

// Platform is the platform name of the copies. A store that Humble or Fanatical sell keys for must
// reuse the name those keys use ("Steam", "GOG"…, see game.IsKnownPlatform): that is how a key is
// flagged as already owned.
const Platform = "Example Store"

const settingToken = "token"

var (
	// ErrNoToken means the token setting is empty.
	ErrNoToken = errors.New("example store: paste an API token (see the help below the field)")

	// ErrSignedOut means the store rejected the token. User-facing errors say what to do.
	ErrSignedOut = errors.New("example store did not accept the token: create a new one in your account settings and paste it")
)

// Provider implements sync.Provider for Example Store accounts.
type Provider struct {
	// API calls the store; tests point it at a fake server.
	API *apiclient.Client
}

// NewProvider returns the Example Store source with its production endpoints.
func NewProvider() *Provider { return &Provider{API: apiclient.New(defaultBaseURL)} }

// LinkStore implements sync.StoreLinker: the game page shows the store of every imported game.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider. Every key is a translation key that plugintest checks.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Example Store",
		DescriptionKey: "sources.example.description",
		Fields: []source.Field{{
			Key:      settingToken,
			LabelKey: "sources.example.token",
			HelpKey:  "sources.example.tokenHelp",
			HelpURL:  "https://example-store.invalid/account/tokens",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

// item is one entry of the library, as the store's API returns it.
type item struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"` // "game", "dlc", "soundtrack"…
	Owned bool   `json:"owned"`
}

// library reads one page of the account's library; limit 0 reads it all.
func (p *Provider) library(ctx context.Context, settings source.Settings, limit int) ([]item, error) {
	token := strings.TrimSpace(settings[settingToken])
	if token == "" {
		return nil, ErrNoToken
	}

	var out struct {
		Items []item `json:"items"`
	}

	err := p.API.Do(ctx, apiclient.Request{
		Path:   "/v1/library",
		Query:  url.Values{"limit": {strconv.Itoa(limit)}},
		Header: http.Header{"Authorization": {"Bearer " + token}},
	}, &out)
	if apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
		return nil, ErrSignedOut
	}

	if err != nil {
		return nil, fmt.Errorf("example store library: %w", err)
	}

	return out.Items, nil
}

// Test implements sync.Provider with the cheapest request that proves the token works.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, err := p.library(ctx, settings, 1)
	return err
}

// Fetch implements sync.Provider: every game the account owns, linked to the store. What is not a
// game, or no longer owned (a refund, an expired trial), is skipped and counted in a warning.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	items, err := p.library(ctx, settings, 0)
	if err != nil {
		return nil, nil, err
	}

	var (
		copies  = make([]game.ImportedCopy, 0, len(items))
		skipped int
	)

	for _, it := range items {
		title := strings.TrimSpace(it.Title)
		if it.ID == 0 || title == "" || it.Type != "game" || !it.Owned {
			skipped++
			continue
		}

		id := strconv.FormatInt(it.ID, 10)
		copies = append(copies, game.ImportedCopy{
			// Stable forever: scans match copies by it, so a new format would duplicate them.
			ExternalID: "example:" + id,
			Title:      title,
			Links:      game.Links{LinkedStore.Key: id},
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   "Example Store",
			},
		})
	}

	var warnings []string
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d Example Store items were not imported: DLC, soundtracks or no longer owned", skipped))
	}

	return copies, warnings, nil
}
