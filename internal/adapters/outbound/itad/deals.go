// Package itad is the IsThereAnyDeal plugin: current game deals (provider.KindDeals) from ITAD's
// API v2 (https://docs.isthereanydeal.com), with the user's own free API key.
//
// ITAD's terms allow private personal use with one's own key, ask for caching and a credit, and
// forbid altering its data (prices, and links with their affiliate tags) and competing with it.
// Game Vault therefore shows its lists as they come, links through ITAD's own links, keeps the
// lists as a snapshot refreshed every six hours, and has no price alerts of its own. Its limit is
// 1000 requests per 5 minutes (HTTP 429 with Retry-After beyond); a refresh sends four.
package itad

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/ratelimit"
	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies IsThereAnyDeal in the deals chain.
const ID provider.ID = "itad"

const (
	settingAPIKey  = "api_key"
	settingCountry = "country"

	defaultBaseURL = "https://api.isthereanydeal.com"

	// keyURL is where a signed-in user registers an app and gets its API key.
	keyURL = "https://isthereanydeal.com/apps/my/"

	// steamShop is Steam's shop id in ITAD's lookups.
	steamShop = 61

	// imageHost serves the games' art.
	imageHost = "assets.isthereanydeal.com"

	// maxList is the largest page ITAD's deals list gives (limit 1–200). The Steam lookup takes the
	// ids of one page at once.
	maxList = 200

	requestTimeout = 20 * time.Second

	// testLimit is the size of the featured list the home page's refresh asks for, so Test sends
	// the request a refresh sends and never a shape of its own.
	testLimit = 40
)

var (
	// ErrNotConfigured means the API key is missing.
	ErrNotConfigured = provider.BadSettings("enter your API key in the provider settings")

	// ErrBadKey means ITAD rejected the API key (HTTP 403 "Invalid or expired api key").
	ErrBadKey = provider.BadSettings("the API key was rejected: check it in the provider settings")

	// ErrBadCountry means the country setting is not a two-letter code.
	ErrBadCountry = provider.BadSettings("the country must be a two-letter code such as US, DE or ES: fix it in the provider settings")
)

// Deals implements home.DealsProvider with ITAD's deals list.
type Deals struct {
	// API calls ITAD (https://api.isthereanydeal.com); tests point it at a fake server.
	API *apiclient.Client

	// throttle spaces the requests; nil sends them at once.
	throttle *ratelimit.Throttle
}

var (
	_ home.DealsProvider = (*Deals)(nil)
	_ media.ImageHoster  = (*Deals)(nil)
)

// NewDeals returns the provider with ITAD's production endpoint. One request a second is far
// below ITAD's limit; a 429 or an outage rests it a minute, or as long as Retry-After asks.
func NewDeals() *Deals {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = requestTimeout
	// Go forwards custom headers such as ITAD-API-Key on a redirect, even to another host. ITAD's
	// API answers without redirects, so one would come from something in between: it is refused
	// (an unexpected HTTP 3xx) rather than followed, and the key never leaves ITAD's host.
	api.NoRedirects = true

	return &Deals{
		API: api,
		throttle: ratelimit.New(ratelimit.Config{
			Gap:     time.Second,
			MaxWait: 30 * time.Second,
			Backoff: time.Minute,
		}),
	}
}

// Descriptor implements providers.Impl.
func (d *Deals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ID,
		Kind:           provider.KindDeals,
		Name:           "IsThereAnyDeal",
		DescriptionKey: "providers.itad.description",
		Fields: schema.Fields{
			{
				Key:      settingAPIKey,
				LabelKey: "providers.itad.apiKey",
				HelpKey:  "providers.itad.apiKeyHelp",
				HelpURL:  keyURL,
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:      settingCountry,
				LabelKey: "providers.itad.country",
				HelpKey:  "providers.itad.countryHelp",
				Kind:     schema.FieldText,
			},
		},
		// Disabled until the user enters a key, like every provider that needs one.
		DefaultOrder: 10,
	}
}

// ImageHosts implements media.ImageHoster: the games' art goes through the image proxy.
func (d *Deals) ImageHosts() []string { return []string{imageHost} }

// Test implements providers.Impl with the featured list's request, as a refresh sends it, without
// the Steam lookup.
func (d *Deals) Test(ctx context.Context, s schema.Settings) error {
	key, country, err := settingsOf(s, "")
	if err != nil {
		return err
	}

	var page dealsPage

	return d.send(ctx, key, apiclient.Request{
		Path:  "/deals/v2",
		Query: featuredQuery(country, testLimit),
	}, &page)
}

var reCountry = regexp.MustCompile(`^[A-Za-z]{2}$`)

// settingsOf returns the key and the country to ask for: the settings' own, else fallback (the
// home page's, from the preferred currency), else the United States.
func settingsOf(s schema.Settings, fallback string) (key, country string, err error) {
	key = strings.TrimSpace(s[settingAPIKey])
	if key == "" {
		return "", "", ErrNotConfigured
	}

	country = strings.TrimSpace(s[settingCountry])
	if country == "" {
		country = strings.TrimSpace(fallback)
	}

	if country == "" {
		country = "US"
	}

	if !reCountry.MatchString(country) {
		return "", "", ErrBadCountry
	}

	return key, strings.ToUpper(country), nil
}

// Deals implements home.DealsProvider. ITAD documents no "historical lows" list: those are the
// deepest cuts (a full page) whose price is at the lowest ITAD has seen.
func (d *Deals) Deals(ctx context.Context, s schema.Settings, q home.DealQuery) ([]game.Deal, error) {
	key, country, err := settingsOf(s, q.Country)
	if err != nil {
		return nil, err
	}

	limit := q.Limit
	if limit <= 0 || limit > maxList {
		limit = maxList
	}

	var query url.Values

	keep := func(itemJSON) bool { return true }

	switch q.List {
	case home.DealsFeatured:
		query = featuredQuery(country, limit)
	case home.DealsHistoricalLows:
		query = url.Values{
			"country": {country},
			"mature":  {"false"},
			"sort":    {"-cut"},
			"limit":   {strconv.Itoa(maxList)},
		}

		keep = itemJSON.atLow
	default:
		return nil, fmt.Errorf("unknown deal list %q", q.List)
	}

	var page dealsPage
	if err := d.send(ctx, key, apiclient.Request{
		Path:  "/deals/v2",
		Query: query,
	}, &page); err != nil {
		return nil, err
	}

	// Only games: a soundtrack's or a DLC's deal says nothing about a game the user may own.
	items := make([]itemJSON, 0, limit)

	for _, it := range page.List {
		if it.Type == "game" && it.Deal != nil && keep(it) {
			items = append(items, it)
		}

		if len(items) == limit {
			break
		}
	}

	steam, err := d.steamApps(ctx, key, items)
	if err != nil {
		return nil, err
	}

	out := make([]game.Deal, 0, len(items))
	for _, it := range items {
		out = append(out, it.deal(steam[it.ID]))
	}

	return out, nil
}

// featuredQuery is the featured list's query: no sort, so ITAD's own order, the one its deals
// page opens with, and no mature games.
func featuredQuery(country string, limit int) url.Values {
	return url.Values{
		"country": {country},
		"mature":  {"false"},
		"limit":   {strconv.Itoa(limit)},
	}
}

var reAppID = regexp.MustCompile(`^[0-9]{1,10}$`)

// steamApps returns the Steam app of each game ITAD knows one for, so the home page matches
// ownership by Steam id. A package ("sub/…") is not an app and is skipped.
func (d *Deals) steamApps(ctx context.Context, key string, items []itemJSON) (map[string]string, error) {
	out := map[string]string{}
	if len(items) == 0 {
		return out, nil
	}

	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}

	// A game without a Steam entry is null (or an empty list): a nil slice either way.
	var found map[string][]string

	err := d.send(ctx, key, apiclient.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/lookup/shop/%d/id/v1", steamShop),
		Body:   ids,
	}, &found)
	if err != nil {
		return nil, err
	}

	for id, shopIDs := range found {
		for _, s := range shopIDs {
			if app, ok := strings.CutPrefix(s, "app/"); ok && reAppID.MatchString(app) {
				out[id] = app
				break
			}
		}
	}

	return out, nil
}

// send makes one request with the key, through the throttle. The key travels in a header: request
// errors name the URL, and they reach the logs and the page.
func (d *Deals) send(ctx context.Context, key string, r apiclient.Request, out any) error {
	r.Header = http.Header{"ITAD-API-Key": {key}}

	return d.throttle.Do(ctx, func() error {
		return classify(ctx, d.API.Do(ctx, r, out))
	})
}

// classify turns a request's error into what it means for the user. Only ITAD's own JSON about the
// key ("Invalid or expired api key", "Missing api key") is a rejected key: a 403 web page comes from
// a CDN or a firewall in front of ITAD, and is a passing trouble like an outage, not a key to fix.
func classify(ctx context.Context, err error) error {
	var se *apiclient.StatusError
	if errors.As(err, &se) && (se.Status == http.StatusUnauthorized || se.Status == http.StatusForbidden) {
		switch {
		case keyRefused(se.Body):
			return ErrBadKey
		case strings.HasPrefix(strings.TrimSpace(se.Body), "<"):
			return fmt.Errorf("blocked by a web page, try again later (%w): %w", err, provider.ErrUnavailable)
		}
	}

	return apiclient.ClassifyError(ctx, err)
}

// keyRefused reports whether body is ITAD's error about the API key
// ({"status_code":403,"reason_phrase":"Invalid or expired api key"}).
func keyRefused(body string) bool {
	var answer struct {
		ReasonPhrase string `json:"reason_phrase"`
	}

	if json.Unmarshal([]byte(body), &answer) != nil {
		return false
	}

	return strings.Contains(strings.ToLower(answer.ReasonPhrase), "api key")
}

// dealsPage is one page of ITAD's deals list (the fields used).
type dealsPage struct {
	List []itemJSON `json:"list"`
}

// itemJSON is a game of the deals list with its deal.
type itemJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	// Type is "game", "dlc"... and null for some items.
	Type string `json:"type"`

	// Assets is an object of image URLs, but ITAD sends an empty array for a game without art:
	// it is decoded on its own so that never fails the list.
	Assets json.RawMessage `json:"assets"`

	Deal *dealJSON `json:"deal"`
}

// dealJSON is an item's current deal.
type dealJSON struct {
	Shop struct {
		Name string `json:"name"`
	} `json:"shop"`
	Price      priceJSON  `json:"price"`
	Regular    priceJSON  `json:"regular"`
	HistoryLow *priceJSON `json:"historyLow"`
	Cut        int        `json:"cut"`

	// Flag is "H" on a historical low in ITAD's sample; null otherwise.
	Flag string `json:"flag"`

	// Expiry is RFC 3339 with an offset, or null.
	Expiry string `json:"expiry"`
	URL    string `json:"url"`
}

// priceJSON is an amount; AmountInt is in the currency's minor unit, as game.Money.
type priceJSON struct {
	AmountInt int64  `json:"amountInt"`
	Currency  string `json:"currency"`
}

// atLow reports whether the deal is at the lowest price ITAD has seen: its flag says so, or its
// price is at or under the recorded low.
func (it itemJSON) atLow() bool {
	if it.Deal == nil {
		return false
	}

	low := it.Deal.HistoryLow

	return it.Deal.Flag == "H" || (low != nil && low.AmountInt > 0 && it.Deal.Price.AmountInt <= low.AmountInt)
}

// deal returns the item as a game.Deal, its data unchanged.
func (it itemJSON) deal(steamAppID string) game.Deal {
	out := game.Deal{
		ProviderGameID: it.ID,
		Title:          strings.TrimSpace(it.Title),
		SteamAppID:     steamAppID,
		Shop:           it.Deal.Shop.Name,
		Price: game.Money{
			Amount:   it.Deal.Price.AmountInt,
			Currency: it.Deal.Price.Currency,
		},
		Regular: game.Money{
			Amount:   it.Deal.Regular.AmountInt,
			Currency: it.Deal.Regular.Currency,
		},
		Cut:           it.Deal.Cut,
		HistoricalLow: it.atLow(),
		URL:           httpsOnly(it.Deal.URL),
		ImageURL:      image(it.Assets),
	}

	if t, err := time.Parse(time.RFC3339, it.Deal.Expiry); err == nil {
		out.Expires = t
	}

	return out
}

// httpsOnly returns raw when it is an https address: the page turns it into a link.
func httpsOnly(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}

	return raw
}

// image returns the game's art on ITAD's image host, "" when there is none. The page frames deal
// art as a wide banner, so ITAD's banners come first (the 400-pixel one fits the card best, then
// the nearest sizes); the box art, a tall cover, is the last resort and the page shows it whole.
// ITAD's documentation names these fields without their sizes, and leaves some of them out for
// some games.
func image(raw json.RawMessage) string {
	var assets struct {
		Boxart    string `json:"boxart"`
		Banner145 string `json:"banner145"`
		Banner300 string `json:"banner300"`
		Banner400 string `json:"banner400"`
		Banner600 string `json:"banner600"`
	}

	if json.Unmarshal(raw, &assets) != nil {
		return "" // an empty array: no art
	}

	for _, candidate := range []string{assets.Banner400, assets.Banner300, assets.Banner600, assets.Banner145, assets.Boxart} {
		if u, err := url.Parse(candidate); err == nil && u.Scheme == "https" && u.Hostname() == imageHost {
			return candidate
		}
	}

	return ""
}
