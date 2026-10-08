package epic

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Epic cover provider.
const CoverProviderID provider.ID = "epic-covers"

// Covers is a cover provider for games imported from an Epic library: the store's own box art,
// read from the public catalog with an application token (no user session needed).
type Covers struct {
	p *Provider // shares the HTTP client, endpoints and launcher credentials

	mu      gosync.Mutex
	token   string
	expires time.Time
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the Epic cover provider with its production endpoints.
func NewCovers() *Covers {
	return &Covers{p: NewProvider()}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: CoverProviderID, Kind: provider.KindCover, Name: "Epic Games Store",
		DescriptionKey: "providers.epicCovers.description", EnabledByDefault: true,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string {
	return []string{"cdn1.epicgames.com", "cdn2.epicgames.com", "cdn1.unrealengine.com", "cdn2.unrealengine.com"}
}

// Applies reports whether the game has a copy imported from an Epic library.
func (c *Covers) Applies(q media.CoverQuery) bool { return len(q.ExternalIDsWithPrefix("epic:")) > 0 }

// imageOrder ranks Epic's key image types: portrait box art first, landscape last.
var imageOrder = []string{"DieselGameBoxTall", "OfferImageTall", "CodeRedemption_340x440", "DieselGameBox", "OfferImageWide", "Thumbnail"}

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	ids := q.ExternalIDsWithPrefix("epic:")

	token, err := c.appToken(ctx)
	if err != nil {
		return nil, err
	}

	v := url.Values{"id": ids, "country": {"US"}, "locale": {"en-US"}}

	var items map[string]struct {
		Title     string `json:"title"`
		KeyImages []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"keyImages"`
	}
	if err := c.p.get(ctx, token, c.p.CatalogURL+"/catalog/api/shared/bulk/items?"+v.Encode(), &items); err != nil {
		c.mu.Lock()
		c.token = "" // maybe expired early: get a new one next time
		c.mu.Unlock()

		return nil, err
	}

	var out []media.CoverCandidate

	for _, typ := range imageOrder {
		for _, id := range ids {
			it, ok := items[id]
			if !ok {
				continue
			}

			for _, img := range it.KeyImages {
				if img.Type != typ || !strings.HasPrefix(img.URL, "https://") {
					continue
				}

				label := "Epic: box art"
				if !strings.Contains(typ, "Tall") && typ != "CodeRedemption_340x440" {
					label = "Epic: wide image"
				}

				out = append(out, media.CoverCandidate{URL: img.URL, ThumbURL: img.URL, Label: label, Title: it.Title, Provider: CoverProviderID})
			}
		}
	}

	return out, nil
}

// appToken returns an application token (client credentials grant): enough for the catalog.
func (c *Covers) appToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Add(time.Minute).Before(c.expires) {
		return c.token, nil
	}

	tok, err := c.p.oauthRaw(ctx, url.Values{"grant_type": {"client_credentials"}, "token_type": {"eg1"}})
	if err != nil {
		return "", fmt.Errorf("epic catalog token: %w", err)
	}

	c.token, c.expires = tok.AccessToken, tok.ExpiresAt

	return c.token, nil
}
