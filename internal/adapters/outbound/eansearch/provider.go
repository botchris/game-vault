// Package eansearch implements a barcode provider backed by EAN-Search.org, a database with good
// coverage of European products. It needs a (paid) API token.
package eansearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies this provider in settings and provider chains.
const ID provider.ID = "eansearch"

const (
	settingToken   = "token"
	defaultBaseURL = "https://api.ean-search.org"
)

// Provider implements media.BarcodeProvider.
type Provider struct {
	BaseURL string
	Client  *http.Client
}

var (
	_ media.BarcodeProvider = (*Provider)(nil)
)

// New returns the EAN-Search barcode provider with its production endpoints.
func New() *Provider {
	return &Provider{
		BaseURL: defaultBaseURL,
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Descriptor implements media.BarcodeProvider.
func (p *Provider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ID,
		Kind:           provider.KindBarcode,
		Name:           "EAN-Search",
		DescriptionKey: "providers.eansearch.description",
		Fields: schema.Fields{{
			Key:      settingToken,
			LabelKey: "providers.eansearch.token",
			HelpKey:  "providers.eansearch.tokenHelp",
			HelpURL:  "https://www.ean-search.org/ean-database-api.html",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
		DefaultOrder: 40,
	}
}

// call runs an API operation, retrying once when the API asks to slow down (HTTP 429), and
// returns the body and the remaining credits (X-Credits-Remaining, -1 when absent).
func (p *Provider) call(ctx context.Context, q url.Values) ([]byte, int, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/api?"+q.Encode(), nil)
		if err != nil {
			return nil, -1, err
		}

		req.Header.Set("User-Agent", "GameVault/1.0")

		res, err := p.Client.Do(req)
		if err != nil {
			return nil, -1, err
		}

		body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()

		if res.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			select {
			case <-ctx.Done():
				return nil, -1, ctx.Err()
			case <-time.After(time.Second):
			}

			continue
		}

		if err != nil {
			return nil, -1, err
		}

		if res.StatusCode != http.StatusOK {
			return nil, -1, fmt.Errorf("HTTP %d", res.StatusCode)
		}

		remaining := -1
		if n, err := strconv.Atoi(res.Header.Get("X-Credits-Remaining")); err == nil {
			remaining = n
		}

		return body, remaining, nil
	}
}

type result struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

func isNotFound(msg string) bool { return strings.Contains(strings.ToLower(msg), "not found") }

// Lookup calls op=barcode-lookup. Answers are a JSON array of products, or of one {"error": ...}
// ("Barcode not found" when the code is unknown).
func (p *Provider) Lookup(ctx context.Context, code game.Barcode, s schema.Settings) ([]media.BarcodeMatch, error) {
	q := url.Values{"token": {s[settingToken]}, "op": {"barcode-lookup"}, "ean": {string(code)}, "format": {"json"}}

	body, _, err := p.call(ctx, q)
	if err != nil {
		return nil, err
	}

	var out []result
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unexpected answer: %w", err)
	}

	var matches []media.BarcodeMatch

	for _, it := range out {
		switch {
		case isNotFound(it.Error):
			return nil, nil
		case it.Error != "":
			return nil, fmt.Errorf("%s", it.Error)
		case it.Name != "":
			title, platform, edition := media.CleanProductTitle(it.Name)
			matches = append(matches, media.BarcodeMatch{
				Raw:      it.Name,
				Title:    title,
				Platform: platform,
				Edition:  edition,
				Provider: ID,
			})
		}
	}

	return matches, nil
}

// Test checks the token with the lightweight verify-checksum operation and reports the remaining
// credits from the response header. It may count as one query on some plans.
func (p *Provider) Test(ctx context.Context, s schema.Settings) error {
	q := url.Values{"token": {s[settingToken]}, "op": {"verify-checksum"}, "ean": {"5030934110075"}, "format": {"json"}}

	body, remaining, err := p.call(ctx, q)
	if err != nil {
		return err
	}

	var out []result
	if json.Unmarshal(body, &out) == nil && len(out) > 0 && out[0].Error != "" {
		return fmt.Errorf("%s", out[0].Error)
	}

	if remaining == 0 {
		return ErrNoCredits
	}

	return nil
}

// ErrNoCredits means the token works but has no lookups left.
var ErrNoCredits = errors.New("the EAN-Search token has no credits left: buy more or wait for the plan to renew")
