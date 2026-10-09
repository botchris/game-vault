// Package humble implements the Humble Bundle source provider. It uses the undocumented JSON API
// that humblebundle.com's own "Keys & Entitlements" page calls, authenticated with the browser's
// session cookie. It only reads: hidden keys are reported as unrevealed and never revealed.
package humble

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// ErrUnauthorized means Humble rejected the session cookie.
var ErrUnauthorized = errors.New("humble bundle rejected the session cookie; copy _simpleauth_sess again from your browser")

// Provider implements sync.Provider for Humble Bundle.
type Provider struct {
	BaseURL string
	Client  *http.Client

	// Pause between batched requests, to be gentle with Humble.
	Pause time.Duration

	// Log receives progress messages during long scans. Optional.
	Log *slog.Logger
}

// NewProvider returns the Humble Bundle source with its production endpoints.
func NewProvider(log *slog.Logger) *Provider {
	return &Provider{
		BaseURL: defaultBaseURL,
		Client:  &http.Client{Timeout: 60 * time.Second},
		Pause:   500 * time.Millisecond,
		Log:     log,
	}
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
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, err := p.listGamekeys(ctx, cleanCookie(settings[settingSession]))
	return err
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Humble Bundle",
		DescriptionKey: "sources.humble.description",
		Fields: []source.Field{{
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
			Kind:     source.FieldSecret,
			Required: true,
		}},
	}
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	orders, err := p.fetchOrders(ctx, cleanCookie(settings[settingSession]))
	if err != nil {
		return nil, nil, err
	}

	copies, skipped, warnings := MapOrders(orders, time.Now())
	if len(skipped) > 0 {
		p.logf("humble: skipped keys that are not games", "count", len(skipped), "titles", strings.Join(skipped, "; "))
	}

	return copies, warnings, nil
}

func (p *Provider) get(ctx context.Context, cookie, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Cookie", "_simpleauth_sess="+cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Safari/537.36")

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || strings.Contains(res.Request.URL.Path, "login") {
		return nil, ErrUnauthorized
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("humble bundle %s: HTTP %d", path, res.StatusCode)
	}

	return io.ReadAll(res.Body)
}

// listGamekeys returns the id of every order in the account.
func (p *Provider) listGamekeys(ctx context.Context, cookie string) ([]string, error) {
	if cookie == "" {
		return nil, ErrUnauthorized
	}

	body, err := p.get(ctx, cookie, "/api/v1/user/order")
	if err != nil {
		return nil, err
	}

	var list []struct {
		Gamekey string `json:"gamekey"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, ErrUnauthorized // an HTML login page instead of JSON
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

		body, err := p.get(ctx, cookie, "/api/v1/orders?"+q.Encode())
		if err != nil {
			return nil, err
		}

		var byKey map[string]json.RawMessage
		if err := json.Unmarshal(body, &byKey); err != nil {
			return nil, fmt.Errorf("unexpected humble bundle response: %w", err)
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
	if p.Log != nil {
		p.Log.Info(msg, args...)
	}
}
