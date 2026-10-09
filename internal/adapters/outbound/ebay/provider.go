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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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

// Provider implements media.BarcodeProvider.
type Provider struct {
	BaseURL string
	Client  *http.Client

	mu     sync.Mutex
	tokens map[string]token // by hash of client id + secret, so changed keys get a new token
}

type token struct {
	value   string
	expires time.Time
}

var (
	_ media.BarcodeProvider = (*Provider)(nil)
)

// New returns the eBay barcode provider with its production endpoints.
func New() *Provider {
	return &Provider{
		BaseURL: defaultBaseURL,
		Client:  &http.Client{Timeout: 15 * time.Second},
		tokens:  map[string]token{},
	}
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

// accessToken returns an application token (client credentials grant), cached until it expires.
func (p *Provider) accessToken(ctx context.Context, s schema.Settings) (string, error) {
	id, secret := strings.TrimSpace(s[settingClientID]), strings.TrimSpace(s[settingClientSecret])
	sum := sha256.Sum256([]byte(id + "\x00" + secret))
	cacheKey := hex.EncodeToString(sum[:])

	p.mu.Lock()
	if t, ok := p.tokens[cacheKey]; ok && time.Now().Before(t.expires) {
		p.mu.Unlock()
		return t.value, nil
	}
	p.mu.Unlock()

	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"https://api.ebay.com/oauth/api_scope"}}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/identity/v1/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(id+":"+secret)))

	res, err := p.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}

	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusOK || out.AccessToken == "" {
		if out.Description != "" {
			return "", fmt.Errorf("eBay rejected the keys: %s", out.Description)
		}

		return "", fmt.Errorf("eBay rejected the keys (HTTP %d)", res.StatusCode)
	}

	p.mu.Lock()
	p.tokens[cacheKey] = token{
		value:   out.AccessToken,
		expires: time.Now().Add(time.Duration(out.ExpiresIn)*time.Second - time.Minute),
	}
	p.mu.Unlock()

	return out.AccessToken, nil
}

// Test checks the keys by getting a token.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
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

func (p *Provider) search(ctx context.Context, tok, marketplace string, q url.Values) ([]listing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/buy/browse/v1/item_summary/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", marketplace)

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eBay %s search: HTTP %d", marketplace, res.StatusCode)
	}

	var out struct {
		ItemSummaries []listing `json:"itemSummaries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out.ItemSummaries, nil
}

// Lookup searches the marketplaces in order and stops at the first one with listings.
func (p *Provider) Lookup(ctx context.Context, code game.Barcode, s schema.Settings) ([]media.BarcodeMatch, error) {
	tok, err := p.accessToken(ctx, s)
	if err != nil {
		return nil, err
	}

	var lastErr error

	for _, m := range marketplaces(s) {
		items, err := p.search(ctx, tok, m, url.Values{"gtin": {string(code)}, "limit": {fmt.Sprint(listingsPerLookup)}})
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
