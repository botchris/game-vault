// Package humble implements the Humble Bundle source provider. It uses the undocumented JSON API
// that humblebundle.com's own "Keys & Entitlements" page calls, authenticated with the browser's
// session cookie. It only reads: hidden keys are reported as unrevealed and never revealed.
package humble

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Humble Bundle accounts.
const Type source.Type = "humble"

const (
	settingSession = "session_cookie"
	defaultBaseURL = "https://www.humblebundle.com"
	batchSize      = 40
)

// ErrSignedOut means Humble rejected the session cookie.
var ErrSignedOut = source.SignedOut("humble bundle rejected the session cookie; copy _simpleauth_sess again from your browser")

// Provider implements sync.Provider for Humble Bundle.
type Provider struct {
	// API calls humblebundle.com; tests point it at a fake server.
	API *apiclient.Client

	// Pause between batched requests, to be gentle with Humble.
	Pause time.Duration

	// log receives progress messages during long scans (WithLogger).
	log *slog.Logger
}

// NewProvider returns the Humble Bundle source with its production endpoints.
func NewProvider(opts ...Option) *Provider {
	// The orders API serves the session of the user's browser, so requests look like that browser.
	api := apiclient.New(defaultBaseURL)
	api.UserAgent = apiclient.BrowserUserAgent

	p := &Provider{
		API:   api,
		Pause: 500 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Option configures a Provider.
type Option func(*Provider)

// WithLogger sends what a scan reports while it runs (the orders it downloads, the keys that are not games) to log. Without it nothing is logged.
func WithLogger(log *slog.Logger) Option {
	return func(p *Provider) { p.log = log }
}

// cleanCookie accepts the value as copied from the browser: the bare value, "_simpleauth_sess=…",
// a whole Cookie header, surrounding quotes and a trailing ";".
func cleanCookie(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "=") {
		if s, ok := browsersession.Parse(v)["_simpleauth_sess"]; ok {
			v = s
		}
	}

	v = strings.TrimSuffix(v, ";")

	return strings.Trim(strings.TrimSpace(v), `"`)
}

// Test implements sync.Provider: one request that checks the session by listing the orders, instead of
// downloading every order like Fetch does.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.listGamekeys(ctx, cleanCookie(settings[settingSession]))
	return err
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Humble Bundle",
		DescriptionKey: "sources.humble.description",
		Fields: []schema.Field{{
			Key:      settingSession,
			LabelKey: "sources.humble.sessionCookie",
			HelpKey:  "sources.humble.sessionCookieHelp",
			HelpURL:  "https://www.humblebundle.com/home/keys",
			SignIn: &schema.SignInRecipe{
				Version: schema.RecipeVersion,
				Open:    "https://www.humblebundle.com/home/keys",
				When: &schema.When{
					URLPrefix: "https://www.humblebundle.com/home/keys",
				},
				Capture: schema.Capture{
					Cookie: &schema.CookieCapture{
						URL:  "https://www.humblebundle.com",
						Name: "_simpleauth_sess",
					},
				},
			},
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
	orders, err := p.fetchOrders(ctx, cleanCookie(settings[settingSession]))
	if err != nil {
		return nil, nil, err
	}

	copies, skipped, warnings := MapOrders(orders, time.Now())
	if len(skipped) > 0 {
		skippedKeys(len(skipped)).Log(p.log)
		p.logf("humble: skipped keys that are not games", "titles", strings.Join(skipped, "; "))
	}

	return copies, warnings, nil
}

// skippedKeys counts the n keys that are not games.
func skippedKeys(n int) *source.Skipped {
	skipped := &source.Skipped{Store: "Humble Bundle"}
	for range n {
		skipped.Add("software, courses, in-game items and store coupons")
	}

	return skipped
}

// get reads one of Humble's endpoints with the session cookie. A refusal or a redirect to the
// sign-in page is ErrSignedOut; a 429, an outage or no answer is a passing trouble.
func (p *Provider) get(ctx context.Context, cookie, path string, q url.Values) (*apiclient.Response, error) {
	res, err := p.API.Send(ctx, apiclient.Request{
		Path:   path,
		Query:  q,
		Header: http.Header{"Cookie": {"_simpleauth_sess=" + cookie}},
	})
	if err != nil {
		return nil, fmt.Errorf("humble bundle %s: %w", path, apiclient.ClassifyError(ctx, err))
	}

	if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden || strings.Contains(res.URL.Path, "login") {
		return nil, ErrSignedOut
	}

	if err := res.Err(); err != nil {
		return nil, fmt.Errorf("humble bundle %s: %w", path, apiclient.ClassifyError(ctx, err))
	}

	return res, nil
}

// listGamekeys returns the id of every order in the account.
func (p *Provider) listGamekeys(ctx context.Context, cookie string) ([]string, error) {
	if cookie == "" {
		return nil, ErrSignedOut
	}

	res, err := p.get(ctx, cookie, "/api/v1/user/order", nil)
	if err != nil {
		return nil, err
	}

	var list []struct {
		Gamekey string `json:"gamekey"`
	}

	err = res.Decode(&list)

	switch {
	case errors.Is(err, apiclient.ErrNotJSON):
		return nil, ErrSignedOut // an HTML sign-in page served with a 200 instead of the list
	case err != nil:
		return nil, fmt.Errorf("humble bundle order list: %w", err)
	}

	keys := make([]string, len(list))
	for i, o := range list {
		keys[i] = o.Gamekey
	}

	return keys, nil
}

func (p *Provider) fetchOrders(ctx context.Context, cookie string) ([]json.RawMessage, error) {
	list, err := p.listGamekeys(ctx, cookie)
	if err != nil {
		return nil, err
	}

	p.logf("humble: downloading orders", "orders", len(list))

	var orders []json.RawMessage

	for i := 0; i < len(list); i += batchSize {
		q := url.Values{"all_tpkds": {"true"}}
		for _, k := range list[i:min(i+batchSize, len(list))] {
			q.Add("gamekeys", k)
		}

		res, err := p.get(ctx, cookie, "/api/v1/orders", q)
		if err != nil {
			return nil, err
		}

		var byKey map[string]json.RawMessage
		if err := res.Decode(&byKey); err != nil {
			return nil, fmt.Errorf("humble bundle orders: %w", err)
		}

		for _, o := range byKey {
			orders = append(orders, o)
		}

		p.logf("humble: progress", "downloaded", min(i+batchSize, len(list)), "total", len(list))

		if i+batchSize < len(list) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(p.Pause):
			}
		}
	}

	return orders, nil
}

func (p *Provider) logf(msg string, args ...any) {
	if p.log != nil {
		p.log.Info(msg, args...)
	}
}
