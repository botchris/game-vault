package steam

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
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
var LinkedStore = game.Store{Key: game.LinkSteam, Name: "Steam", PageURL: StorePageURL + "{id}"}

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
}

var (
	_ media.CoverProvider = (*Store)(nil)
	_ media.LinkSearcher  = (*Store)(nil)
)

// Descriptor implements media.CoverProvider.
func (s *Store) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: CoverProviderID, Kind: provider.KindCover, Name: "Steam",
		DescriptionKey: "providers.steam.description", EnabledByDefault: true,
		DefaultOrder: 20,
	}
}

// ImageHosts implements media.ImageHoster.
func (s *Store) ImageHosts() []string {
	return []string{"cdn.cloudflare.steamstatic.com", "shared.cloudflare.steamstatic.com",
		"cdn.akamai.steamstatic.com", "shared.akamai.steamstatic.com"}
}

// Applies reports whether Steam knows the game: only those linked to a Steam AppID.
func (s *Store) Applies(q media.CoverQuery) bool { return AppIDOf(q) != 0 }

// Covers returns the portrait library art first, then the landscape header. Recent apps keep
// their images under hashed paths (…/apps/<id>/<hash>/library_600x900.jpg), so the real paths are
// asked to IStoreBrowseService (one free request); the legacy predictable URLs are the fallback.
func (s *Store) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	appID := AppIDOf(q)
	if appID == 0 {
		return nil, nil
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

		out = append(out, media.CoverCandidate{URL: u, ThumbURL: u, Label: label, Title: q.Title, Provider: CoverProviderID})
	}

	return out, nil
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

	return &Store{Site: site, API: api, CDNURL: defaultCDNURL, AssetsURL: defaultAssetsURL}
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

// SearchLinks implements media.LinkSearcher: it searches the Steam store by title.
func (s *Store) SearchLinks(ctx context.Context, query string) ([]media.LinkMatch, error) {
	q := url.Values{"term": {query}, "l": {"english"}, "cc": {"US"}}

	var out struct {
		Items []struct {
			Type      string `json:"type"`
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			TinyImage string `json:"tiny_image"`
		} `json:"items"`
	}
	if err := s.Site.Get(ctx, "/api/storesearch/", q, &out); err != nil {
		return nil, fmt.Errorf("steam store search: %w", err)
	}

	matches := make([]media.LinkMatch, 0, len(out.Items))
	for _, it := range out.Items {
		if it.Type == "app" {
			matches = append(matches, media.LinkMatch{ID: strconv.FormatInt(it.ID, 10), Name: it.Name, ImageURL: it.TinyImage})
		}
	}

	return matches, nil
}
