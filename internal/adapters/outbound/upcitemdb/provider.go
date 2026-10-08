// Package upcitemdb implements a barcode provider backed by UPCitemdb (https://www.upcitemdb.com).
//
// Without a key it uses the free trial endpoint (about 100 lookups a day per IP). Coverage of
// European (PAL) game boxes is partial: in tests it knew about half of them.
package upcitemdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

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
	BaseURL string
	Client  *http.Client
}

var _ media.BarcodeProvider = (*Provider)(nil)

// New returns the UPCitemdb barcode provider with its production endpoints.
func New() *Provider {
	return &Provider{BaseURL: defaultBaseURL, Client: &http.Client{Timeout: 15 * time.Second}}
}

// Descriptor implements media.BarcodeProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: ID, Kind: provider.KindBarcode, Name: "UPCitemdb",
		DescriptionKey: "providers.upcitemdb.description", EnabledByDefault: true,
		Fields: schema.Fields{{
			Key: settingUserKey, LabelKey: "providers.upcitemdb.userKey", HelpKey: "providers.upcitemdb.userKeyHelp",
			HelpURL: "https://www.upcitemdb.com/wp/docs/main/development/", Kind: schema.FieldSecret,
		}},
	}
}

// Lookup queries the trial endpoint, or the paid one when a user key is configured.
func (p *Provider) Lookup(ctx context.Context, code game.Barcode, s schema.Settings) ([]media.BarcodeMatch, error) {
	path := "/prod/trial/lookup"
	if s[settingUserKey] != "" {
		path = "/prod/v1/lookup"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path+"?"+url.Values{"upc": {string(code)}}.Encode(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	if k := s[settingUserKey]; k != "" {
		req.Header.Set("user_key", k)
		req.Header.Set("key_type", "3scale")
	}

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var out struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Items   []struct {
			Title  string   `json:"title"`
			Images []string `json:"images"`
		} `json:"items"`
	}

	_ = json.NewDecoder(res.Body).Decode(&out)
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("daily lookup limit reached; try again tomorrow or add a user key")
	case out.Code == "INVALID_UPC" || out.Code == "NOT_FOUND":
		return nil, nil
	case res.StatusCode != http.StatusOK:
		if out.Message != "" {
			return nil, fmt.Errorf("%s (HTTP %d)", out.Message, res.StatusCode)
		}

		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}

	var matches []media.BarcodeMatch

	for _, it := range out.Items {
		if it.Title == "" {
			continue
		}

		title, platform, edition := media.CleanProductTitle(it.Title)

		m := media.BarcodeMatch{Raw: it.Title, Title: title, Platform: platform, Edition: edition, Provider: ID}
		if len(it.Images) > 0 {
			m.ImageURL = it.Images[0]
		}

		matches = append(matches, m)
	}

	return matches, nil
}
