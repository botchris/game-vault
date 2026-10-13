// Package ebay implements a barcode provider backed by the eBay Browse API: it finds listings
// whose EAN matches the scanned barcode. Second-hand sellers on eBay Europe list old PAL games
// with their EAN, which generic barcode databases lack.
//
// Listing titles are noisy ("Dead Space 3 Xbox 360 PAL Completo Muy buen estado"), so the
// provider takes the title most listings agree on; the cover providers then turn it into the
// canonical game. Needs free eBay developer keys (client credentials).
package ebay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthapp"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies this provider in settings and provider chains.
const ID provider.ID = "ebay"

const (
	settingClientID     = "client_id"
	settingClientSecret = "client_secret"
	settingMarketplaces = "marketplaces"
	defaultMarketplaces = "EBAY_ES,EBAY_GB,EBAY_DE,EBAY_FR,EBAY_IT"
	defaultBaseURL      = "https://api.ebay.com"

	// settingsGroup puts both eBay providers in one group: they use the same developer keys and
	// marketplaces, so the user enters them once.
	settingsGroup     = "ebay"
	listingsPerLookup = 20
)

// ErrKeysRefused means eBay refused, twice, an access token it had just issued for the keys: they
// are not allowed to use the Browse API.
var ErrKeysRefused = provider.BadSettings("eBay refused the access token of these keys: check them in the settings")

// errTokenRefused is the Browse API's HTTP 401 to an access token.
var errTokenRefused = fmt.Errorf("eBay refused the access token: %w", apiclient.ErrTokenRejected)

// Provider implements media.BarcodeProvider. The barcode and price providers share one, so they
// share its access tokens.
type Provider struct {
	// API calls eBay's REST APIs (the token endpoint and the Browse API); tests point it at a fake
	// server.
	API *apiclient.Client

	// tokens holds the application token of each set of keys.
	tokens oauthapp.Tokens
}

var (
	_ media.BarcodeProvider = (*Provider)(nil)
)

// New returns the eBay barcode provider with its production endpoints.
func New() *Provider {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 15 * time.Second

	return &Provider{API: api}
}

// Descriptor implements media.BarcodeProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ID,
		Kind:           provider.KindBarcode,
		Name:           "eBay",
		DescriptionKey: "providers.ebay.description",
		SettingsGroup:  settingsGroup,
		Fields: schema.Fields{
			{
				Key:      settingClientID,
				LabelKey: "providers.ebay.clientId",
				HelpKey:  "providers.ebay.keysHelp",
				HelpURL:  "https://developer.ebay.com/my/keys",
				Kind:     schema.FieldText,
				Required: true,
			},
			{
				Key:      settingClientSecret,
				LabelKey: "providers.ebay.clientSecret",
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:      settingMarketplaces,
				LabelKey: "providers.ebay.marketplaces",
				HelpKey:  "providers.ebay.marketplacesHelp",
				Kind:     schema.FieldText,
			},
		},
		DefaultOrder: 20,
	}
}

// credentials returns the client id, the secret and the key of their cached token.
func credentials(s schema.Settings) (id, secret, key string) {
	id, secret = strings.TrimSpace(s[settingClientID]), strings.TrimSpace(s[settingClientSecret])

	return id, secret, oauthapp.Key(id, secret)
}

// accessToken returns an application token (client credentials grant) for the keys, cached until
// shortly before it expires.
func (p *Provider) accessToken(ctx context.Context, s schema.Settings) (string, error) {
	id, secret, key := credentials(s)

	return p.tokens.Get(ctx, key, p.fetchToken(id, secret))
}

// fetchToken returns the request of an application token for the keys.
func (p *Provider) fetchToken(id, secret string) oauthapp.Fetch {
	return func(ctx context.Context) (oauthapp.Token, error) {
		res, err := p.API.Send(ctx, apiclient.Request{
			Method: http.MethodPost,
			Path:   "/identity/v1/oauth2/token",
			Header: http.Header{"Authorization": {apiclient.BasicAuth(id, secret)}},
			Form: url.Values{
				"grant_type": {"client_credentials"},
				"scope":      {"https://api.ebay.com/oauth/api_scope"},
			},
		})
		if err != nil {
			return oauthapp.Token{}, fmt.Errorf("eBay sign-in: %w", apiclient.ClassifyError(ctx, err))
		}

		var out struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
			Description string `json:"error_description"`
		}

		decodeErr := res.Decode(&out)

		// Only a 400, 401 or 403 (invalid_client, invalid_scope…) means the keys are wrong; a 429,
		// an outage or a web page instead of JSON is a passing trouble, after which the same keys
		// work again.
		switch {
		case res.OK() && errors.Is(decodeErr, apiclient.ErrNotJSON):
			return oauthapp.Token{}, fmt.Errorf("eBay sign-in: %w", apiclient.ClassifyError(ctx, decodeErr))
		case res.OK() && out.AccessToken != "":
			// A zero lifetime counts as oauthapp.DefaultLifetime, not as a token already expired.
			return oauthapp.Token{
				Value:    out.AccessToken,
				Lifetime: time.Duration(out.ExpiresIn) * time.Second,
			}, nil
		case res.OK() || apiclient.Rejected(res.Status):
			if out.Description != "" {
				return oauthapp.Token{}, provider.BadSettings("eBay rejected the keys: " + out.Description)
			}

			return oauthapp.Token{}, provider.BadSettings(fmt.Sprintf("eBay rejected the keys (HTTP %d): check them in the settings", res.Status))
		}

		return oauthapp.Token{}, fmt.Errorf("eBay sign-in: %w", apiclient.ClassifyError(ctx, res.Err()))
	}
}

// Test checks the keys by getting a new token: a cached one would not reach eBay.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
	_, _, key := credentials(s)
	p.tokens.Forget(key)

	_, err := p.accessToken(ctx, s)

	return err
}

func marketplaces(s schema.Settings) []string {
	v := s[settingMarketplaces]
	if strings.TrimSpace(v) == "" {
		v = defaultMarketplaces
	}

	var out []string

	for m := range strings.SplitSeq(v, ",") {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			out = append(out, m)
		}
	}

	return out
}

type listing struct {
	Title string `json:"title"`
	Price struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"price"`
	Image struct {
		ImageURL string `json:"imageUrl"`
	} `json:"image"`
}

// search asks the Browse API for the listings matching q on a marketplace. A token eBay refuses
// (it expired early, or was revoked) is renewed once and the search sent again.
func (p *Provider) search(ctx context.Context, s schema.Settings, marketplace string, q url.Values) ([]listing, error) {
	id, secret, key := credentials(s)

	var out struct {
		ItemSummaries []listing `json:"itemSummaries"`
	}

	err := p.tokens.WithRetry(ctx, key, p.fetchToken(id, secret), func(tok string) error {
		err := p.API.Do(ctx, apiclient.Request{
			Path:  "/buy/browse/v1/item_summary/search",
			Query: q,
			Header: http.Header{
				"Authorization":           {"Bearer " + tok},
				"X-EBAY-C-MARKETPLACE-ID": {marketplace},
			},
		}, &out)
		if apiclient.IsStatus(err, http.StatusUnauthorized) {
			return errTokenRefused
		}

		return err
	})

	switch {
	case err == nil:
		return out.ItemSummaries, nil
	case errors.Is(err, apiclient.ErrTokenRejected):
		// A token eBay just issued that it still refuses: the keys may not use the Browse API.
		return nil, ErrKeysRefused
	case errors.Is(err, provider.ErrBadSettings):
		return nil, err
	}

	return nil, fmt.Errorf("eBay %s search: %w", marketplace, apiclient.ClassifyError(ctx, err))
}

// Lookup searches the marketplaces in order and stops at the first one with listings.
func (p *Provider) Lookup(ctx context.Context, code game.Barcode, s schema.Settings) ([]media.BarcodeMatch, error) {
	// The token first: keys eBay rejects fail the lookup once, not once per marketplace.
	if _, err := p.accessToken(ctx, s); err != nil {
		return nil, err
	}

	var lastErr error

	for _, m := range marketplaces(s) {
		items, err := p.search(ctx, s, m, url.Values{"gtin": {string(code)}, "limit": {fmt.Sprint(listingsPerLookup)}})
		if errors.Is(err, provider.ErrBadSettings) {
			return nil, err
		}

		if err != nil {
			lastErr = err
			continue
		}

		if match, ok := consensus(items); ok {
			return []media.BarcodeMatch{match}, nil
		}
	}

	return nil, lastErr
}

// Words sellers add to titles that are not part of the game's name (several languages).
var reListingNoise = regexp.MustCompile(`(?i)\b(pal|ntsc|esp|espa[ñn]a|espa[ñn]ol|castellano|spanish|english|uk|eu|fr|ita|deutsch|` +
	`completo|complete|complet|komplett|cib|nuevo|new|neuf|neu|nuovo|precintado|sealed|sigillato|usado|used|occasion|gebraucht|` +
	`muy|buen|bueno|estado|very|good|excellent|mint|condition|zustand|manual|manuale|anleitung|instrucciones|libro|with|con|mit|avec|` +
	`disco|disc|game|juego|jeu|spiel|gioco|videojuego|version|versi[oó]n|edition)\b|[|/•★*!]+`)

// consensus picks the title most listings agree on and the platform they mention most.
func consensus(items []listing) (media.BarcodeMatch, bool) {
	type group struct {
		count int
		title string
		raw   string
		image string
	}

	groups := map[string]*group{}
	platforms := map[string]int{}

	for _, it := range items {
		title, platform, _ := media.CleanProductTitle(it.Title)
		title = strings.Join(strings.Fields(reListingNoise.ReplaceAllString(title, " ")), " ")

		key := game.MatchKey(title)
		if key == "" {
			continue
		}

		if platform != "" {
			platforms[platform]++
		}

		g := groups[key]
		if g == nil {
			g = &group{
				title: title,
				raw:   it.Title,
				image: it.Image.ImageURL,
			}
			groups[key] = g
		}

		g.count++
		if len(title) < len(g.title) { // the shortest spelling is usually the cleanest
			g.title = title
		}
	}

	if len(groups) == 0 {
		return media.BarcodeMatch{}, false
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		if groups[keys[i]].count != groups[keys[j]].count {
			return groups[keys[i]].count > groups[keys[j]].count
		}

		return len(keys[i]) < len(keys[j])
	})
	best := groups[keys[0]]

	platform, n := "", 0
	for pl, c := range platforms {
		if c > n || (c == n && pl < platform) {
			platform, n = pl, c
		}
	}

	return media.BarcodeMatch{
		Raw:      best.raw,
		Title:    best.title,
		Platform: platform,
		ImageURL: best.image,
		Provider: ID,
	}, true
}
