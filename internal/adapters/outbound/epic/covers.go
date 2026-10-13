package epic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthapp"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Epic cover provider.
const CoverProviderID provider.ID = "epic-covers"

// Covers is a cover provider for games imported from an Epic library: the store's own box art,
// read from the public catalog with an application token (no user session needed). It shares only
// the launcher's credentials and the token request with the source, so the source's session rules
// (a refused token signs the user out) never reach a cover lookup.
type Covers struct {
	// OAuth calls Epic's account service for the application token; tests point it at a fake
	// server.
	OAuth *apiclient.Client

	// Catalog calls the public catalog.
	Catalog *apiclient.Client

	// tokens keeps the application token.
	tokens oauthapp.Tokens
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the Epic cover provider with its production endpoints.
func NewCovers() *Covers {
	return &Covers{
		OAuth:   newClient(defaultOAuthURL),
		Catalog: newClient(defaultCatalogURL),
	}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "Epic Games Store",
		DescriptionKey:   "providers.epicCovers.description",
		EnabledByDefault: true,
		DefaultOrder:     30,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string {
	return []string{"cdn1.epicgames.com", "cdn2.epicgames.com", "cdn1.unrealengine.com", "cdn2.unrealengine.com"}
}

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// Applies reports whether the game is linked to the Epic Games Store, for its PC edition.
func (c *Covers) Applies(q media.GameQuery) bool { return q.ForPC() && q.Links[LinkedStore.Key] != "" }

// imageOrder ranks Epic's key image types: portrait box art first, landscape last.
var imageOrder = []string{"DieselGameBoxTall", "OfferImageTall", "CodeRedemption_340x440", "DieselGameBox", "OfferImageWide", "Thumbnail"}

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	id := q.Links[LinkedStore.Key]
	v := url.Values{"id": {id}, "country": {"US"}, "locale": {"en-US"}}

	var items map[string]struct {
		Title     string `json:"title"`
		KeyImages []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"keyImages"`
	}

	// A token Epic refuses (it expired early) is renewed once.
	err := c.tokens.WithRetry(ctx, appTokenKey, c.appToken, func(token string) error {
		return c.get(ctx, token, "/catalog/api/shared/bulk/items", v, &items)
	})
	if err != nil {
		return nil, err
	}

	it, ok := items[id]
	if !ok {
		return nil, nil
	}

	var out []media.CoverCandidate

	for _, typ := range imageOrder {
		for _, img := range it.KeyImages {
			if img.Type != typ || !strings.HasPrefix(img.URL, "https://") {
				continue
			}

			label := "Epic: box art"
			if !strings.Contains(typ, "Tall") && typ != "CodeRedemption_340x440" {
				label = "Epic: wide image"
			}

			out = append(out, media.CoverCandidate{
				URL:      img.URL,
				ThumbURL: img.URL,
				Label:    label,
				Title:    it.Title,
				Provider: CoverProviderID,
			})
		}
	}

	return out, nil
}

// get reads the catalog with the application token. A 401 wraps apiclient.ErrTokenRejected, which
// the tokens renew once; a 429 or an outage is a passing trouble, after which the chain asks again.
func (c *Covers) get(ctx context.Context, token, path string, q url.Values, out any) error {
	err := c.Catalog.Do(ctx, apiclient.Request{
		Path:   path,
		Query:  q,
		Header: http.Header{"Authorization": {"bearer " + token}},
	}, out)

	switch {
	case err == nil:
		return nil
	case apiclient.IsStatus(err, http.StatusUnauthorized):
		return fmt.Errorf("epic refused the catalog's application token: %w", apiclient.ErrTokenRejected)
	}

	return fmt.Errorf("epic catalog: %w", apiclient.ClassifyError(ctx, err))
}

// clientCredentials returns the form of the application token request (client credentials grant).
func clientCredentials() url.Values {
	return url.Values{"grant_type": {"client_credentials"}, "token_type": {"eg1"}}
}

// appTokenKey is the one application token's key: the launcher's credentials never change.
const appTokenKey = "launcher"

// appToken asks for an application token (client credentials grant): enough for the catalog.
func (c *Covers) appToken(ctx context.Context) (oauthapp.Token, error) {
	tok, err := requestToken(ctx, c.OAuth, clientCredentials())
	if err != nil {
		return oauthapp.Token{}, fmt.Errorf("epic catalog token: %w", err)
	}

	return oauthapp.Token{
		Value:   tok.AccessToken,
		Expires: tok.ExpiresAt,
	}, nil
}

// Test implements providers.Impl: it requests a fresh application token (client credentials
// grant), which proves the launcher credentials and the sign-in endpoint work. Any HTTP or
// transport failure is reported.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := requestToken(ctx, c.OAuth, clientCredentials()); err != nil {
		return fmt.Errorf("epic did not give an application token (%w): check your connection and try again later", err)
	}

	return nil
}
