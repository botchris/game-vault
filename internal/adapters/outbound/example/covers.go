package example

import (
	"context"
	"fmt"
	"net/http"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the provider in the cover chain. It is stored with the user's chain,
// so it never changes.
const CoverProviderID provider.ID = "example-covers"

// imageHost is where the store serves its images.
const imageHost = "images.example-store.invalid"

// Covers is a cover provider for games linked to Example Store: the store's own box art, from its
// public catalog (no sign-in needed).
type Covers struct {
	// API calls the store's public catalog; tests point it at a fake server.
	API *apiclient.Client
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.StoreLinker   = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the Example Store cover provider with its production endpoints.
func NewCovers() *Covers { return &Covers{API: apiclient.New(defaultBaseURL)} }

// Descriptor implements media.CoverProvider. DefaultOrder is its place in the cover chain for new
// installs: after the stores already there (Steam is 20, Xbox 80). The user can reorder it.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "Example Store",
		DescriptionKey:   "providers.exampleCovers.description",
		EnabledByDefault: true,
		DefaultOrder:     90,
	}
}

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// ImageHosts implements media.ImageHoster: the browser loads candidates through Game Vault's image
// proxy, which only fetches from declared hosts.
func (c *Covers) ImageHosts() []string { return []string{imageHost} }

// Applies reports whether the game is linked to the store, for its PC edition. It decides from the
// query alone, without the network.
func (c *Covers) Applies(q media.CoverQuery) bool { return q.ForPC() && q.Links[LinkedStore.Key] != "" }

type product struct {
	Title string `json:"title"`
	Art   struct {
		Portrait string `json:"portrait"`
		Wide     string `json:"wide"`
	} `json:"art"`
}

// Covers implements media.CoverProvider: the portrait box art first, then the wide banner. A game
// the catalog does not know is no candidates, not an error.
func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	id := q.Links[LinkedStore.Key]
	if id == "" {
		return nil, nil
	}

	var p product

	err := c.API.Get(ctx, "/v1/products/"+id, nil, &p)
	if apiclient.IsStatus(err, http.StatusNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("example store catalog: %w", err)
	}

	var out []media.CoverCandidate

	for _, a := range []struct {
		url   string
		label string
	}{{p.Art.Portrait, "box art"}, {p.Art.Wide, "banner"}} {
		if a.url != "" {
			out = append(out, media.CoverCandidate{
				URL:      a.url,
				ThumbURL: a.url,
				Label:    "Example Store: " + a.label,
				Title:    p.Title,
				Provider: CoverProviderID,
			})
		}
	}

	return out, nil
}

// testProductID is a product that exists in the catalog, used to check the API answers.
const testProductID = "1"

// Test implements media.Provider: one request to the public catalog. No key is needed, so only
// transport and HTTP errors fail it.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	var p product
	if err := c.API.Get(ctx, "/v1/products/"+testProductID, nil, &p); err != nil {
		return fmt.Errorf("the Example Store catalog is not answering (%w); try again later", err)
	}

	return nil
}
