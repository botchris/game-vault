package ubisoft

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Ubisoft cover provider.
const CoverProviderID provider.ID = "ubisoft-covers"

const (
	defaultCDNURL = "https://ubiservices.cdn.ubi.com"

	// The Ubisoft Store's product search (Algolia). The key is the public, search-only key the store
	// gives every visitor's browser.
	defaultStoreSearchURL = "https://xely3u4lod-dsn.algolia.net/1/indexes/production__us_ubisoft__products__en_US__best_sellers/query"
	storeSearchAppID      = "XELY3U4LOD"
	storeSearchKey        = "dc39136c7548697cbee2fbc171d04133"
)

// Covers is a cover provider for Ubisoft games: the box art Ubisoft Connect shows for games imported
// from a Ubisoft library (by space id, on Ubisoft's CDN), else the Ubisoft Store's packshot, found
// by title. No sign-in needed.
type Covers struct {
	// CDNURL serves the box art of a space id; CDN checks those images exist.
	CDNURL string
	CDN    *http.Client

	// Search calls the Ubisoft Store's search (Algolia), with the key every store visitor gets.
	Search *apiclient.Client
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the Ubisoft Connect cover provider with its production endpoints.
func NewCovers() *Covers {
	search := apiclient.New(defaultStoreSearchURL)
	search.HTTP.Timeout = 20 * time.Second
	search.Header.Set("X-Algolia-Application-Id", storeSearchAppID)
	search.Header.Set("X-Algolia-API-Key", storeSearchKey)

	return &Covers{
		CDNURL: defaultCDNURL,
		CDN:    &http.Client{Timeout: 20 * time.Second},
		Search: search,
	}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "Ubisoft",
		DescriptionKey:   "providers.ubisoftCovers.description",
		EnabledByDefault: true,
		DefaultOrder:     50,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string {
	return []string{"ubiservices.cdn.ubi.com", "store.ubisoft.com", "staticctf.ubisoft.com"}
}

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// Applies reports whether the game is linked to Ubisoft Connect or has a copy for it (a Humble
// Uplay key, for instance).
func (c *Covers) Applies(q media.CoverQuery) bool {
	if q.Links[LinkedStore.Key] != "" {
		return true
	}

	for _, p := range q.Platforms {
		if strings.EqualFold(p, Platform) {
			return true
		}
	}

	return false
}

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	var out []media.CoverCandidate

	if space := q.Links[LinkedStore.Key]; space != "" {
		u := c.CDNURL + "/" + url.PathEscape(space) + "/spaceCardAsset/boxArt_mobile.jpg"
		if c.exists(ctx, u) {
			out = append(out, media.CoverCandidate{
				URL:      u,
				ThumbURL: u,
				Label:    "Ubisoft Connect: box art",
				Title:    q.Title,
				Provider: CoverProviderID,
			})
		}
	}

	store, err := c.storeCovers(ctx, q.Title)
	if err != nil && len(out) == 0 {
		return nil, err
	}

	return append(out, store...), nil
}

// exists checks an image URL without downloading it (Ubisoft's CDN only has art for some games).
func (c *Covers) exists(ctx context.Context, u string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false
	}

	res, err := c.CDN.Do(req)
	if err != nil {
		return false
	}

	res.Body.Close()

	return res.StatusCode == http.StatusOK && strings.HasPrefix(res.Header.Get("Content-Type"), "image/")
}

type storeHit struct {
	Title       string  `json:"title"`
	ShortTitle  string  `json:"short_title"`
	Edition     string  `json:"Edition"`
	ProductType string  `json:"product_type"`
	DLCType     *string `json:"dlcType"`
	ImageGroups []struct {
		Images []struct {
			URL string `json:"dis_base_link"`
		} `json:"images"`
	} `json:"image_groups"`
}

// storeCovers searches the Ubisoft Store for the game (full games only, same title) and returns
// the packshot of the standard edition first.
func (c *Covers) storeCovers(ctx context.Context, title string) ([]media.CoverCandidate, error) {
	var out struct {
		Hits []storeHit `json:"hits"`
	}
	if err := c.Search.Post(ctx, "", map[string]any{"query": title, "hitsPerPage": 20}, &out); err != nil {
		return nil, fmt.Errorf("ubisoft store search: %w", err)
	}

	return pickStoreCovers(title, out.Hits), nil
}

func pickStoreCovers(title string, hits []storeHit) []media.CoverCandidate {
	key := game.MatchKey(title)

	var standard, others []media.CoverCandidate

	seen := map[string]bool{}

	for _, h := range hits {
		if h.DLCType != nil || !strings.EqualFold(h.ProductType, "Games") {
			continue
		}

		if game.MatchKey(h.ShortTitle) != key && game.MatchKey(h.Title) != key {
			continue
		}

		if len(h.ImageGroups) == 0 || len(h.ImageGroups[0].Images) == 0 {
			continue
		}

		u := h.ImageGroups[0].Images[0].URL
		if !strings.HasPrefix(u, "https://") || seen[u] {
			continue
		}

		seen[u] = true

		cand := media.CoverCandidate{
			URL:      u,
			ThumbURL: u,
			Label:    "Ubisoft Store: " + strings.TrimSpace(h.Edition),
			Title:    h.ShortTitle,
			Provider: CoverProviderID,
		}
		if strings.Contains(strings.ToLower(h.Edition), "standard") {
			standard = append(standard, cand)
		} else {
			others = append(others, cand)
		}
	}

	return append(standard, others...)
}

// Test implements media.Provider: it runs one search on the Ubisoft Store's public product index and
// fails if the store answers with an error or something that is not a search result.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := c.storeCovers(ctx, "Assassin's Creed"); err != nil {
		return fmt.Errorf("the Ubisoft Store search is not answering (%w); try again later", err)
	}

	return nil
}
