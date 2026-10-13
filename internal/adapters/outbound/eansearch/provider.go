// Package eansearch implements a barcode provider backed by EAN-Search.org, a database with good
// coverage of European products. It needs a (paid) API token.
package eansearch

import (
	"context"
	"fmt"
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

	// maxBody bounds an answer: a lookup returns a handful of products.
	maxBody = 1 << 20

	// retryWait is how long a lookup waits before its one retry after HTTP 429.
	retryWait = time.Second
)

// Provider implements media.BarcodeProvider.
type Provider struct {
	// API calls EAN-Search's API; tests point it at a fake server.
	API *apiclient.Client
}

var (
	_ media.BarcodeProvider = (*Provider)(nil)
)

// New returns the EAN-Search barcode provider with its production endpoints.
func New() *Provider {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 15 * time.Second
	api.MaxBody = maxBody

	return &Provider{API: api}
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
// returns the answer and the remaining credits (X-Credits-Remaining, -1 when absent). The token
// travels in the query (the only way the API takes it); apiclient never shows the query in an
// error, so none returned here names the token.
func (p *Provider) call(ctx context.Context, q url.Values) (*apiclient.Response, int, error) {
	for attempt := 0; ; attempt++ {
		res, err := p.API.Send(ctx, apiclient.Request{
			Path:  "/api",
			Query: q,
		})
		if err != nil {
			return nil, -1, apiclient.ClassifyError(ctx, err)
		}

		// One quick retry: EAN-Search's 429 is a short burst limit, and the lookup is on demand.
		if res.Status == http.StatusTooManyRequests && attempt == 0 {
			timer := time.NewTimer(retryWait)

			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, -1, ctx.Err()
			case <-timer.C:
			}

			continue
		}

		remaining := -1
		if n, err := strconv.Atoi(res.Header.Get("X-Credits-Remaining")); err == nil {
			remaining = n
		}

		if !res.OK() {
			return nil, remaining, apiError(ctx, res)
		}

		return res, remaining, nil
	}
}

// apiError builds the error of a non-200 answer. EAN-Search answers a rejected token with a 401
// and a JSON array holding one {"error": "Invalid token"} (probed with a bogus token on
// 2026-10-13): that is a settings problem, reported in the provider's status with the API's words.
// A message about credits is a used-up plan; a 429 or a 5xx is a passing trouble.
func apiError(ctx context.Context, res *apiclient.Response) error {
	var out []result
	// Best effort: without a readable message the status alone describes the error.
	_ = res.Decode(&out)

	msg := ""
	if len(out) > 0 {
		msg = strings.TrimSpace(out[0].Error)
	}

	switch {
	case isNoCredits(msg):
		return ErrNoCredits
	case res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden:
		if msg == "" {
			msg = fmt.Sprintf("the token was rejected (HTTP %d)", res.Status)
		}

		return provider.BadSettings(msg + ": check the EAN-Search token in the settings")
	case msg != "":
		return fmt.Errorf("%s: %w", msg, apiclient.ClassifyError(ctx, res.Err()))
	}

	return apiclient.ClassifyError(ctx, res.Err())
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

	res, _, err := p.call(ctx, q)
	if err != nil {
		return nil, err
	}

	// A web page instead of JSON (a bot check, a maintenance notice) is an outage.
	var out []result
	if err := res.Decode(&out); err != nil {
		return nil, apiclient.ClassifyError(ctx, err)
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

	res, remaining, err := p.call(ctx, q)
	if err != nil {
		return err
	}

	var out []result
	if res.Decode(&out) == nil && len(out) > 0 && out[0].Error != "" {
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
