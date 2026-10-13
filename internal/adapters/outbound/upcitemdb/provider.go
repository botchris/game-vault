// Package upcitemdb implements a barcode provider backed by UPCitemdb (https://www.upcitemdb.com).
//
// Without a key it uses the free trial endpoint (about 100 lookups a day per IP). Coverage of
// European (PAL) game boxes is partial: in tests it knew about half of them.
package upcitemdb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies this provider in settings and provider chains.
const ID provider.ID = "upcitemdb"

const (
	settingUserKey = "user_key"
	defaultBaseURL = "https://api.upcitemdb.com"
)

// Provider implements media.BarcodeProvider.
type Provider struct {
	// API calls UPCitemdb's API; tests point it at a fake server.
	API *apiclient.Client
}

var _ media.BarcodeProvider = (*Provider)(nil)

// New returns the UPCitemdb barcode provider with its production endpoints.
func New() *Provider {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 15 * time.Second

	return &Provider{API: api}
}

// Descriptor implements media.BarcodeProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               ID,
		Kind:             provider.KindBarcode,
		Name:             "UPCitemdb",
		DescriptionKey:   "providers.upcitemdb.description",
		EnabledByDefault: true,
		Fields: schema.Fields{{
			Key:      settingUserKey,
			LabelKey: "providers.upcitemdb.userKey",
			HelpKey:  "providers.upcitemdb.userKeyHelp",
			HelpURL:  "https://www.upcitemdb.com/wp/docs/main/development/",
			Kind:     schema.FieldSecret,
		}},
		DefaultOrder: 30,
	}
}

// Test implements providers.Provider: it looks up a well-known EAN with the configured endpoint (trial
// or user key). An unknown code still counts as working; a rate limit, a rejected key or any other
// HTTP error does not.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
	if _, err := p.Lookup(ctx, "5030934110075", s); err != nil {
		return fmt.Errorf("the UPCitemdb lookup failed, check the user key or try again later: %w", err)
	}

	return nil
}

// Lookup queries the trial endpoint, or the paid one when a user key is configured.
func (p *Provider) Lookup(ctx context.Context, code game.Barcode, s schema.Settings) ([]media.BarcodeMatch, error) {
	req := apiclient.Request{
		Path:  "/prod/trial/lookup",
		Query: url.Values{"upc": {string(code)}},
	}

	if k := s[settingUserKey]; k != "" {
		req.Path = "/prod/v1/lookup"
		req.Header = http.Header{
			"user_key": {k},
			"key_type": {"3scale"},
		}
	}

	res, err := p.API.Send(ctx, req)
	if err != nil {
		return nil, apiclient.ClassifyError(ctx, err)
	}

	var out struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Items   []struct {
			Title  string   `json:"title"`
			Images []string `json:"images"`
		} `json:"items"`
	}

	// An error answer is read for its code and message only, which it may lack; an answer to the
	// lookup must be JSON (a web page instead is an outage, not "no match").
	if err := res.Decode(&out); err != nil && res.OK() {
		return nil, apiclient.ClassifyError(ctx, err)
	}

	// UPCitemdb's error codes tell its two limits apart: TOO_FAST is a burst (requests per
	// minute), EXCEED_LIMIT the daily allowance. A rejected user key is a settings problem. All
	// three are the provider's status, not an answer about the code.
	switch {
	case out.Code == "EXCEED_LIMIT":
		return nil, fmt.Errorf("daily lookup limit reached; try again tomorrow or add a user key: %w", provider.ErrQuotaExhausted)
	case res.Status == http.StatusTooManyRequests:
		return nil, fmt.Errorf("too many lookups at once; try again in a minute: %w", provider.ErrRateLimited)
	case out.Code == "INVALID_UPC" || out.Code == "NOT_FOUND":
		return nil, nil
	case res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden:
		msg := out.Message
		if msg == "" {
			msg = fmt.Sprintf("the user key was rejected (HTTP %d)", res.Status)
		}

		return nil, provider.BadSettings(msg + ": check the UPCitemdb user key in the settings")
	case !res.OK():
		err := apiclient.ClassifyError(ctx, res.Err())
		if out.Message != "" {
			return nil, fmt.Errorf("%s: %w", out.Message, err)
		}

		return nil, err
	}

	var matches []media.BarcodeMatch

	for _, it := range out.Items {
		if it.Title == "" {
			continue
		}

		title, platform, edition := game.ParseProductName(it.Title)

		m := media.BarcodeMatch{
			Raw:      it.Title,
			Title:    title,
			Platform: platform,
			Edition:  edition,
			Provider: ID,
		}
		if len(it.Images) > 0 {
			m.ImageURL = it.Images[0]
		}

		matches = append(matches, m)
	}

	return matches, nil
}
