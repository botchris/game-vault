package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

const (
	defaultStoreURL  = "https://store.steampowered.com"
	defaultCDNURL    = "https://cdn.cloudflare.steamstatic.com"
	defaultAPIURL    = "https://api.steampowered.com"
	defaultAssetsURL = "https://shared.akamai.steamstatic.com/store_item_assets/"
)

// CoverProviderID identifies Steam in the cover provider chain.
const CoverProviderID provider.ID = "steam"

// Store uses Steam's public store endpoints (no API key needed). It implements
// media.CoverProvider and media.AppSearcher.
type Store struct {
	StoreURL string
	CDNURL   string
	// APIURL serves IStoreBrowseService, which knows the real (hashed) image paths.
	APIURL string
	// AssetsURL is the CDN prefix those paths are relative to.
	AssetsURL string
	Client    *http.Client
}

var (
	_ media.CoverProvider = (*Store)(nil)
	_ media.AppSearcher   = (*Store)(nil)
)

func (s *Store) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: CoverProviderID, Kind: provider.KindCover, Name: "Steam",
		DescriptionKey: "providers.steam.description", EnabledByDefault: true,
	}
}

// ImageHosts implements media.ImageHoster.
func (s *Store) ImageHosts() []string {
	return []string{"cdn.cloudflare.steamstatic.com", "shared.cloudflare.steamstatic.com",
		"cdn.akamai.steamstatic.com", "shared.akamai.steamstatic.com"}
}

// Applies: Steam only knows games linked to a Steam AppID.
func (s *Store) Applies(q media.CoverQuery) bool { return q.SteamAppID != 0 }

// Covers returns the portrait library art first, then the landscape header. Recent apps keep
// their images under hashed paths (…/apps/<id>/<hash>/library_600x900.jpg), so the real paths are
// asked to IStoreBrowseService (one free request); the legacy predictable URLs are the fallback.
func (s *Store) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	urls := s.CoverURLs(q.SteamAppID)
	if library, header, err := s.assetURLs(ctx, q.SteamAppID); err == nil && (library != "" || header != "") {
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

// assetURLs resolves the library (portrait) and header images of an app.
func (s *Store) assetURLs(ctx context.Context, appID int64) (library, header string, err error) {
	input := fmt.Sprintf(`{"ids":[{"appid":%d}],"context":{"language":"english","country_code":"US"},"data_request":{"include_assets":true}}`, appID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.APIURL+"/IStoreBrowseService/GetItems/v1/?"+url.Values{"input_json": {input}}.Encode(), nil)
	if err != nil {
		return "", "", err
	}
	res, err := s.Client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("steam store items: HTTP %d", res.StatusCode)
	}
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
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", "", err
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

func NewStore() *Store {
	return &Store{StoreURL: defaultStoreURL, CDNURL: defaultCDNURL, APIURL: defaultAPIURL, AssetsURL: defaultAssetsURL,
		Client: &http.Client{Timeout: 15 * time.Second}}
}

// CoverURLs returns Steam's portrait and header art URLs for an app.
func (s *Store) CoverURLs(appID int64) []string {
	return []string{
		fmt.Sprintf("%s/steam/apps/%d/library_600x900.jpg", s.CDNURL, appID),
		fmt.Sprintf("%s/steam/apps/%d/header.jpg", s.CDNURL, appID),
	}
}

// SearchApps searches the Steam store by title.
func (s *Store) SearchApps(ctx context.Context, query string) ([]media.AppMatch, error) {
	q := url.Values{"term": {query}, "l": {"english"}, "cc": {"US"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.StoreURL+"/api/storesearch/?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam store search: HTTP %d", res.StatusCode)
	}
	var out struct {
		Items []struct {
			Type      string `json:"type"`
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			TinyImage string `json:"tiny_image"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	matches := make([]media.AppMatch, 0, len(out.Items))
	for _, it := range out.Items {
		if it.Type == "app" {
			matches = append(matches, media.AppMatch{AppID: it.ID, Name: it.Name, ImageURL: it.TinyImage})
		}
	}
	return matches, nil
}
