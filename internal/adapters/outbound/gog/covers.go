package gog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the GOG cover provider.
const CoverProviderID provider.ID = "gog-covers"

const defaultAPIURL = "https://api.gog.com"

// Covers is a cover provider for games imported from a GOG library: the store's own box art, from
// GOG's public product API (no sign-in needed).
type Covers struct {
	APIURL string
	Client *http.Client
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the GOG cover provider with its production endpoints.
func NewCovers() *Covers {
	return &Covers{APIURL: defaultAPIURL, Client: &http.Client{Timeout: 20 * time.Second}}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: CoverProviderID, Kind: provider.KindCover, Name: "GOG",
		DescriptionKey: "providers.gogCovers.description", EnabledByDefault: true,
		DefaultOrder: 40,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string { return []string{"images.gog-statics.com"} }

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// Applies reports whether the game is linked to GOG.
func (c *Covers) Applies(q media.CoverQuery) bool { return q.Links[LinkedStore.Key] != "" }

type link struct {
	Href string `json:"href"`
}

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	var out []media.CoverCandidate

	for _, id := range []string{q.Links[LinkedStore.Key]} {
		var g struct {
			Links struct {
				BoxArt     link `json:"boxArtImage"`
				Background link `json:"backgroundImage"`
			} `json:"_links"`
			Embedded struct {
				Product struct {
					Title string `json:"title"`
				} `json:"product"`
			} `json:"_embedded"`
		}
		if err := c.get(ctx, c.APIURL+"/v2/games/"+id, &g); err != nil {
			return out, err
		}

		title := g.Embedded.Product.Title
		for _, l := range []struct{ href, label string }{
			{g.Links.BoxArt.Href, "GOG: box art"}, {g.Links.Background.Href, "GOG: wide image"},
		} {
			if strings.HasPrefix(l.href, "https://") {
				out = append(out, media.CoverCandidate{URL: l.href, ThumbURL: l.href, Label: l.label, Title: title, Provider: CoverProviderID})
			}
		}
	}
	// Portrait art of every copy first.
	var portrait, wide []media.CoverCandidate

	for _, cand := range out {
		if cand.Label == "GOG: box art" {
			portrait = append(portrait, cand)
		} else {
			wide = append(wide, cand)
		}
	}

	return append(portrait, wide...), nil
}

func (c *Covers) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	res, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil // product unknown to the API: no candidates
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("gog %s: HTTP %d", req.URL.Path, res.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

// testProductID is a long-lived GOG product (The Witcher: Enhanced Edition) used to probe the API.
const testProductID = "1207658924"

// Test implements media.Provider: it looks up one well-known product in GOG's public product API.
// An unknown product still means the API works; transport errors, HTTP errors and rate limits fail.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	var ignored json.RawMessage

	if err := c.get(ctx, c.APIURL+"/v2/games/"+testProductID, &ignored); err != nil {
		return fmt.Errorf("the GOG product API did not answer (%w): check your connection and try again later", err)
	}

	return nil
}
