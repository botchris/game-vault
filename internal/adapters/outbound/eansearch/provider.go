// Package eansearch implements a barcode provider backed by EAN-Search.org, a database with good
// coverage of European products. It needs a (paid) API token.
package eansearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
// returns the body and the remaining credits (X-Credits-Remaining, -1 when absent). The token
// travels in the query (the only way the API takes it), so no error returned here may show the
// URL's query: transport errors go through apiclient.ClassifyError, which drops it.
func (p *Provider) call(ctx context.Context, q url.Values) ([]byte, int, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/api", nil)
		if err != nil {
			return nil, -1, err
		}

		req.URL.RawQuery = q.Encode()
		req.Header.Set("User-Agent", apiclient.UserAgent)

		res, err := p.Client.Do(req)
		if err != nil {
			return nil, -1, apiclient.ClassifyError(ctx, err)
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

		remaining := -1
		if n, err := strconv.Atoi(res.Header.Get("X-Credits-Remaining")); err == nil {
			remaining = n
		}

		if res.StatusCode != http.StatusOK {
			return nil, remaining, apiError(res.StatusCode, body)
		}

		return body, remaining, nil
	}
}

// apiError builds the error of a non-200 answer. EAN-Search answers a rejected token with a 401
// and a JSON array holding one {"error": "Invalid token"} (probed with a bogus token on
// 2026-10-13): that is a settings problem, reported in the provider's status with the API's words.
// A message about credits is a used-up plan; a 429 or a 5xx is a passing trouble.
func apiError(status int, body []byte) error {
	var out []result
	// Best effort: without a readable message the status alone describes the error.
	_ = json.Unmarshal(body, &out)

	msg := ""
	if len(out) > 0 {
		msg = strings.TrimSpace(out[0].Error)
	}

	switch {
	case isNoCredits(msg):
		return ErrNoCredits
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if msg == "" {
			msg = fmt.Sprintf("the token was rejected (HTTP %d)", status)
		}

		return provider.BadSettings(msg + ": check the EAN-Search token in the settings")
	case msg != "":
		return fmt.Errorf("%s: %w", msg, apiclient.ClassifyStatus(status))
	}

	return apiclient.ClassifyStatus(status)
}

// isNoCredits reports whether an API message says the plan has no lookups left.
func isNoCredits(msg string) bool { return strings.Contains(strings.ToLower(msg), "credit") }

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
		case isNoCredits(it.Error):
			return nil, ErrNoCredits
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
		if isNoCredits(out[0].Error) {
			return ErrNoCredits
		}

		return fmt.Errorf("%s", out[0].Error)
	}

	if remaining == 0 {
		return ErrNoCredits
	}

	return nil
}

// ErrNoCredits means the token works but has no lookups left. It matches provider.ErrQuotaExhausted
// (and so provider.ErrRateLimited): the provider's status shows it, and nothing is remembered about
// the barcode from it.
var ErrNoCredits error = noCredits{}

// noCredits is ErrNoCredits' type: its own message, unwrapping to provider.ErrQuotaExhausted.
type noCredits struct{}

func (noCredits) Error() string {
	return "the EAN-Search token has no credits left: buy more or wait for the plan to renew"
}

// Unwrap makes errors.Is(ErrNoCredits, provider.ErrQuotaExhausted) true.
func (noCredits) Unwrap() error { return provider.ErrQuotaExhausted }
