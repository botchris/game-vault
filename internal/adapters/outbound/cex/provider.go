// Package cex implements a barcode provider backed by CeX (webuy.com), the second-hand game
// retailer, whose product ids are the EANs of the boxes. It knows many European (PAL) editions
// that general barcode databases miss.
//
// It reads the box detail endpoint the CeX website itself calls (undocumented; community notes in
// github.com/Dionakra/webuy-api). Each country has its own catalog of the editions sold there,
// and a barcode does not tell which country it belongs to, so every country is asked in turn. The
// countries that answered before are asked first: a collection mostly comes from one market, so
// after the first scans a lookup usually costs one request. Only the product name and platform
// are used: CeX's images and prices are not.
package cex

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies this provider in settings and provider chains.
const ID provider.ID = "cex"

const (
	settingCountries = "countries"

	// settingHits is learned, never shown: how many lookups each country answered ("es:12,uk:1").
	settingHits = "country_hits"

	// defaultBaseURL has the country code in place of %s.
	defaultBaseURL = "https://wss2.cex.%s.webuy.io/v3"
	userAgent      = "GameVault/1.0 (+https://github.com/botchris/game-vault)"

	// gamingSuperCat is CeX's top category for games in every country (its name is translated).
	gamingSuperCat = 1
)

// Countries are the CeX stores with a box detail API (2026-10), in the order tried when nothing
// has been learned yet.
var Countries = []string{"uk", "es", "ie", "pt", "au", "in", "mx", "it", "pl"}

// ErrBlocked means CeX answered with something other than its JSON API, typically a bot
// protection page.
var ErrBlocked = errors.New("CeX is not answering lookups right now (its site may be checking for bots); the next barcode database is used instead")

// Provider implements media.BarcodeProvider.
type Provider struct {
	// API calls CeX. Its BaseURL has the country code in place of %s (each country has its own
	// host), so tests can point every country at one server.
	API *apiclient.Client
}

var _ media.BarcodeProvider = (*Provider)(nil)

// New returns the CeX barcode provider with its production endpoints.
func New() *Provider {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 10 * time.Second

	return &Provider{API: api}
}

// Descriptor implements media.BarcodeProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: ID, Kind: provider.KindBarcode, Name: "CeX",
		DescriptionKey: "providers.cex.description", EnabledByDefault: true,
		Fields: schema.Fields{
			{
				Key: settingCountries, LabelKey: "providers.cex.countries", HelpKey: "providers.cex.countriesHelp",
				Kind: schema.FieldText,
			},
			{Key: settingHits, Kind: schema.FieldState},
		},
		DefaultOrder: 10,
	}
}

// Lookup asks each country's catalog in turn, the ones that answered before first, and stops at
// the first that knows the code. The country that answers is remembered in s.
func (p *Provider) Lookup(ctx context.Context, code game.Barcode, s schema.Settings) ([]media.BarcodeMatch, error) {
	hits := parseHits(s[settingHits])

	for _, country := range order(allowed(s[settingCountries]), hits) {
		box, err := p.detail(ctx, country, code)
		if err != nil {
			return nil, err
		}

		if box == nil {
			continue
		}

		hits[country]++
		s[settingHits] = formatHits(hits)

		if box.SuperCatID != gamingSuperCat {
			return nil, nil // a film, a controller…: the code is not a game
		}

		return []media.BarcodeMatch{match(*box)}, nil
	}

	return nil, nil
}

// Test implements media.Provider with one light request (the list of top categories) to the first
// country that would be asked, which shows whether CeX answers its API at all.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
	country := order(allowed(s[settingCountries]), parseHits(s[settingHits]))[0]

	var out struct{}

	return p.get(ctx, country, "/supercats", &out)
}

// box is the part of CeX's box detail that is used.
type box struct {
	BoxName      string `json:"boxName"`
	CategoryName string `json:"categoryName"`
	SuperCatID   int    `json:"superCatId"`
}

// detail returns the box with that code in a country's catalog, or nil when it is unknown there.
func (p *Provider) detail(ctx context.Context, country string, code game.Barcode) (*box, error) {
	var out struct {
		Response struct {
			Data *struct {
				BoxDetails []box `json:"boxDetails"`
			} `json:"data"`
		} `json:"response"`
	}
	if err := p.get(ctx, country, "/boxes/"+string(code)+"/detail", &out); err != nil {
		return nil, err
	}

	if d := out.Response.Data; d != nil && len(d.BoxDetails) > 0 {
		return &d.BoxDetails[0], nil
	}

	return nil, nil
}

// get decodes the JSON answer of a country's API at path into out.
func (p *Provider) get(ctx context.Context, country, path string, out any) error {
	api := *p.API
	api.BaseURL = fmt.Sprintf(p.API.BaseURL, country)

	err := api.Get(ctx, path, nil, out)

	// A bot check is an HTML page (often 403 or 503): stop rather than try every country.
	var se *apiclient.StatusError
	if errors.Is(err, apiclient.ErrNotJSON) || (errors.As(err, &se) && strings.HasPrefix(se.Body, "<")) {
		return ErrBlocked
	}

	if se != nil {
		return fmt.Errorf("CeX %s: HTTP %d", country, se.Status)
	}

	if err != nil {
		return fmt.Errorf("unexpected CeX answer: %w", err)
	}

	return nil
}

// reParens drops CeX's notes on the box ("(2 Discs)", "(No DLC)", "(Platinum)").
var reParens = regexp.MustCompile(`\s*\([^)]*\)`)

func match(b box) media.BarcodeMatch {
	title, platform, edition := media.CleanProductTitle(reParens.ReplaceAllString(b.BoxName, ""))
	// The platform comes from the category ("Xbox 360 Juegos", "Playstation4 Games").
	if _, p, _ := media.CleanProductTitle(b.CategoryName); p != "" {
		platform = p
	}

	return media.BarcodeMatch{Raw: b.BoxName, Title: title, Platform: platform, Edition: edition, Provider: ID}
}

// allowed returns the countries the user limited lookups to, or every country.
func allowed(setting string) []string {
	var out []string

	for c := range strings.SplitSeq(strings.ToLower(setting), ",") {
		if c = strings.TrimSpace(c); slices.Contains(Countries, c) && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return Countries
	}

	return out
}

// order puts the countries that answered most often first, keeping the given order otherwise.
func order(countries []string, hits map[string]int) []string {
	out := slices.Clone(countries)
	slices.SortStableFunc(out, func(a, b string) int { return hits[b] - hits[a] })

	return out
}

func parseHits(s string) map[string]int {
	hits := map[string]int{}

	for part := range strings.SplitSeq(s, ",") {
		country, n, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}

		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			hits[country] = v
		}
	}

	return hits
}

func formatHits(hits map[string]int) string {
	countries := make([]string, 0, len(hits))
	for c := range hits {
		countries = append(countries, c)
	}

	slices.Sort(countries)

	parts := make([]string, 0, len(countries))
	for _, c := range countries {
		parts = append(parts, c+":"+strconv.Itoa(hits[c]))
	}

	return strings.Join(parts, ",")
}
