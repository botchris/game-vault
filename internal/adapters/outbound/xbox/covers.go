package xbox

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Xbox cover provider.
const CoverProviderID provider.ID = "xbox-covers"

// Covers is a cover provider for games imported from a Microsoft account: the Store's official
// poster (portrait), from the public Store catalog. No sign-in needed.
type Covers struct {
	CatalogURL string
	Market     string
	Language   string
	Client     *http.Client
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the Xbox / Microsoft Store cover provider with its production endpoints.
func NewCovers() *Covers {
	return &Covers{
		CatalogURL: defaultCatalogURL,
		Market:     "US",
		Language:   "en-US",
		Client:     &http.Client{Timeout: 20 * time.Second},
	}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "Xbox / Microsoft Store",
		DescriptionKey:   "providers.xboxCovers.description",
		EnabledByDefault: true,
		DefaultOrder:     80,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string { return []string{"store-images.s-microsoft.com"} }

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// xboxSystems are the consoles whose games the Microsoft Store catalog has art for.
var xboxSystems = []string{"Xbox Series", "Xbox One", "Xbox 360", "Xbox"}

// IsXboxSystem reports whether system is an Xbox console.
func IsXboxSystem(system string) bool { return slices.Contains(xboxSystems, system) }

// Applies reports whether the game is linked to the Microsoft Store and the query is for its PC
// edition, an Xbox edition, or names no system.
func (c *Covers) Applies(q media.CoverQuery) bool {
	return (q.ForPC() || IsXboxSystem(q.System)) && q.Links[LinkedStore.Key] != ""
}

// imagePurposes ranks the Store's images: portrait poster first, square box art next.
var imagePurposes = []string{"Poster", "BoxArt"}

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	ids := []string{q.Links[LinkedStore.Key]}

	products, err := catalog(ctx, c.Client, c.CatalogURL, c.Market, c.Language, ids)
	if err != nil {
		return nil, err
	}

	var out []media.CoverCandidate

	for _, purpose := range imagePurposes {
		for _, id := range ids {
			pr, ok := products[id]
			if !ok || len(pr.LocalizedProperties) == 0 {
				continue
			}

			lp := pr.LocalizedProperties[0]
			for _, img := range lp.Images {
				if img.ImagePurpose != purpose || img.URI == "" {
					continue
				}

				u := img.URI
				if strings.HasPrefix(u, "//") {
					u = "https:" + u
				}

				out = append(out, media.CoverCandidate{
					URL:      u,
					ThumbURL: u + "?w=300",
					Label:    "Microsoft Store: " + strings.ToLower(purpose),
					Title:    lp.ProductTitle,
					Provider: CoverProviderID,
				})

				break
			}
		}
	}

	return out, nil
}

// testProductID is a Microsoft Store product id used to probe the catalog; an unknown id would
// work too, because the catalog answers 200 with no products.
const testProductID = "9NBLGGH4R315"

// Test implements media.Provider: it asks the public Store catalog for one product. An empty
// answer still means the catalog works; transport errors, HTTP errors and rate limits fail.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := catalog(ctx, c.Client, c.CatalogURL, c.Market, c.Language, []string{testProductID}); err != nil {
		return fmt.Errorf("the Microsoft Store catalog did not answer (%w): check your connection and try again later", err)
	}

	return nil
}
