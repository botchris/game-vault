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
	// CDN serves the box art of a space id; the provider checks those images exist.
	CDN *apiclient.Client

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

	cdn := apiclient.New(defaultCDNURL)
	cdn.HTTP.Timeout = 20 * time.Second

	return &Covers{
		CDN:    cdn,
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
// Uplay key, for instance), for its PC edition.
func (c *Covers) Applies(q media.GameQuery) bool {
	if !q.ForPC() {
		return false
	}

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
func (c *Covers) Covers(ctx context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	var out []media.CoverCandidate

	if space := q.Links[LinkedStore.Key]; space != "" {
		path := "/" + url.PathEscape(space) + "/spaceCardAsset/boxArt_mobile.jpg"
		if u := c.CDN.BaseURL + path; c.exists(ctx, path) {
			out = append(out, media.CoverCandidate{
				URL:      u,
				ThumbURL: u,
				Label:    "Ubisoft Connect: box art",
				Title:    q.Title,
				Provider: CoverProviderID,
			})
		}
	}

	if q.LinksOnly { // a virtual PC box: the space's art or nothing, no title search
		return out, nil
	}

	store, err := c.storeCovers(ctx, q.Title)
	if err != nil && len(out) == 0 {
		return nil, err
	}

	return append(out, store...), nil
}

// exists checks an image on the CDN without downloading it (Ubisoft's CDN only has art for some
// games).
func (c *Covers) exists(ctx context.Context, path string) bool {
	res, err := c.CDN.Send(ctx, apiclient.Request{
		Method: http.MethodHead,
		Path:   path,
		// An image, not JSON: no Accept header, as before the shared client.
		Header: http.Header{"Accept": nil},
	})
	if err != nil {
		return false
	}

	return res.Status == http.StatusOK && strings.HasPrefix(res.Header.Get("Content-Type"), "image/")
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
		// A 429 or an outage says nothing about the game: the chain asks again later.
		return nil, fmt.Errorf("ubisoft store search: %w", apiclient.ClassifyError(ctx, err))
	}

	return pickStoreCovers(title, out.Hits), nil
}

func pickStoreCovers(title string, hits []storeHit) []media.CoverCandidate {
	var standard, others []media.CoverCandidate

	seen := map[string]bool{}

	for _, h := range hits {
		if h.DLCType != nil || !strings.EqualFold(h.ProductType, "Games") {
			continue
		}

		if !game.SameMatchKey(h.ShortTitle, title) && !game.SameMatchKey(h.Title, title) {
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

// Test implements providers.Provider: it runs one search on the Ubisoft Store's public product index and
// fails if the store answers with an error or something that is not a search result.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := c.storeCovers(ctx, "Assassin's Creed"); err != nil {
		return fmt.Errorf("the Ubisoft Store search is not answering (%w); try again later", err)
	}

	return nil
}
