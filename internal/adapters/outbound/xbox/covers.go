package xbox

import (
	"context"
	"net/http"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Xbox cover provider.
const CoverProviderID provider.ID = "xbox-covers"

// Covers is a cover provider for games imported from a Microsoft account: the Store's official
// poster (portrait), from the public Store catalog. No sign-in needed.
type Covers struct {
	CatalogURL, Market, Language string
	Client                       *http.Client
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

func NewCovers() *Covers {
	return &Covers{CatalogURL: defaultCatalogURL, Market: "US", Language: "en-US", Client: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: CoverProviderID, Kind: provider.KindCover, Name: "Xbox / Microsoft Store",
		DescriptionKey: "providers.xboxCovers.description", EnabledByDefault: true,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string { return []string{"store-images.s-microsoft.com"} }

// Applies: only games with a copy imported from a Microsoft account.
func (c *Covers) Applies(q media.CoverQuery) bool { return len(q.ExternalIDsWithPrefix("xbox:")) > 0 }

// imagePurposes ranks the Store's images: portrait poster first, square box art next.
var imagePurposes = []string{"Poster", "BoxArt"}

func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	ids := q.ExternalIDsWithPrefix("xbox:")
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
				out = append(out, media.CoverCandidate{URL: u, ThumbURL: u + "?w=300", Label: "Microsoft Store: " + strings.ToLower(purpose),
					Title: lp.ProductTitle, Provider: CoverProviderID})
				break
			}
		}
	}
	return out, nil
}
