// Package gog implements the GOG source provider. GOG has no public library API, so it signs in
// like the GOG Galaxy client does, the way Heroic, Playnite and lgogdownloader work:
//
//  1. the user signs in on gog.com through the login link; GOG redirects to a page whose address
//     carries a one-time code (…/on_login_success?origin=client&code=…), which the user pastes;
//  2. the code is exchanged for a session (a refresh token that rotates on every use);
//  3. scans list the owned products, which already carry their titles.
//
// It only reads the library: nothing is bought, downloaded or changed in the account.
package gog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of GOG accounts.
const Type source.Type = "gog"

// Platform is the store name used on copies, matching Humble's GOG keys.
const Platform = "GOG"

const (
	settingAuthCode = "authorization_code"
	settingSession  = "session"

	// GOG Galaxy's public client credentials, the same ones Heroic and lgogdownloader use.
	clientID     = "46899977096215655"
	clientSecret = "9d85c43b1482497dbbce61f6e4aa173a433796eeae2ca8c5f6129f2dc4de46d9"
	redirectURI  = "https://embed.gog.com/on_login_success?origin=client"

	defaultAuthURL  = "https://auth.gog.com"
	defaultEmbedURL = "https://embed.gog.com"

	// pendingTTL keeps the session of a just-exchanged code, so "Test" and then "Save" can both
	// use the same one-time code.
	pendingTTL = 15 * time.Minute
	maxPages   = 200
)

// LoginURL is where the user signs in to get a code.
var LoginURL = defaultAuthURL + "/auth?" + url.Values{
	"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "layout": {"client2"},
}.Encode()

var (
	// ErrNoSession means there is neither a stored session nor a code to start one.
	ErrNoSession = errors.New("gog: paste the code (open the login link, sign in and copy the address of the page GOG opens)")

	// ErrBadCode means GOG rejected the code.
	ErrBadCode = errors.New("gog rejected the code: codes work once and expire quickly, open the login link again and paste the new address")

	// ErrSessionExpired means the stored session can no longer be refreshed.
	ErrSessionExpired = errors.New("the GOG session expired: open the login link and paste a new code")

	reCodeParam = regexp.MustCompile(`[?&]code=([^&#\s"]+)`)
)

// session is what the source stores between scans.
type session struct {
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
}

type accessToken struct {
	token   string
	expires time.Time
}

type pendingSession struct {
	s       session
	expires time.Time
}

// Provider implements sync.Provider and sync.Preparer for GOG accounts.
type Provider struct {
	AuthURL, EmbedURL string
	Client            *http.Client
	Now               func() time.Time

	mu      gosync.Mutex
	pending map[string]pendingSession // sha256(code) → session from a recent exchange
	tokens  map[string]accessToken    // user id → access token
}

// NewProvider returns the GOG source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{
		AuthURL: defaultAuthURL, EmbedURL: defaultEmbedURL, Now: time.Now,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			// Without a valid session GOG redirects to its login page: report that, do not follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "GOG",
		DescriptionKey: "sources.gog.description",
		Fields: []source.Field{
			{Key: settingAuthCode, LabelKey: "sources.gog.authCode", HelpKey: "sources.gog.authCodeHelp",
				HelpURL: LoginURL, Kind: source.FieldSecret},
			{Key: settingSession, Kind: source.FieldState},
		},
	}
}

// cleanCode accepts the bare code or the whole address of the page GOG redirects to.
func cleanCode(v string) string {
	v = strings.TrimSpace(v)
	if m := reCodeParam.FindStringSubmatch(v); m != nil {
		if code, err := url.QueryUnescape(m[1]); err == nil {
			return code
		}

		return m[1]
	}

	return strings.Trim(v, `"' `)
}

func hashCode(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

// Prepare implements sync.Preparer: a pasted code is exchanged for a session, which is stored
// instead of the code (codes work only once).
func (p *Provider) Prepare(ctx context.Context, settings source.Settings) (source.Settings, error) {
	out := source.Settings{}
	for k, v := range settings {
		out[k] = v
	}

	code := cleanCode(settings[settingAuthCode])
	delete(out, settingAuthCode)

	if code == "" || code == source.SecretPlaceholder {
		if out[settingSession] == "" {
			return nil, ErrNoSession
		}

		return out, nil
	}

	key := hashCode(code)

	p.mu.Lock()
	pend, ok := p.pending[key]
	p.mu.Unlock()

	s := pend.s
	if !ok || p.Now().After(pend.expires) {
		tok, err := p.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}})
		if err != nil {
			return nil, err
		}

		s = p.remember(tok)
		p.mu.Lock()
		if p.pending == nil {
			p.pending = map[string]pendingSession{}
		}

		p.pending[key] = pendingSession{s: s, expires: p.Now().Add(pendingTTL)}
		p.mu.Unlock()
	}

	raw, _ := json.Marshal(s)
	out[settingSession] = string(raw)

	return out, nil
}

// Test implements sync.Provider: one request for the list of owned product ids.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	token, err := p.accessToken(ctx, settings)
	if err != nil {
		return err
	}

	var owned struct {
		Owned []int64 `json:"owned"`
	}

	return p.get(ctx, token, p.EmbedURL+"/user/data/games", &owned)
}

type product struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	IsGame  bool   `json:"isGame"`
	IsMovie bool   `json:"isMovie"`
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	token, err := p.accessToken(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	var products []product

	for page := 1; page <= maxPages; page++ {
		var res struct {
			TotalPages int       `json:"totalPages"`
			Products   []product `json:"products"`
		}

		q := url.Values{"mediaType": {"1"}, "page": {strconv.Itoa(page)}}
		if err := p.get(ctx, token, p.EmbedURL+"/account/getFilteredProducts?"+q.Encode(), &res); err != nil {
			return nil, nil, err
		}

		products = append(products, res.Products...)
		if page >= res.TotalPages || len(res.Products) == 0 {
			break
		}
	}

	return mapProducts(products), nil, nil
}

// mapProducts turns owned products into copies: games only (mediaType 1 already excludes movies).
func mapProducts(products []product) []game.ImportedCopy {
	seen := map[int64]bool{}

	var out []game.ImportedCopy

	for _, pr := range products {
		title := strings.TrimSpace(pr.Title)
		if pr.IsMovie || title == "" || seen[pr.ID] {
			continue
		}

		seen[pr.ID] = true
		out = append(out, game.ImportedCopy{
			ExternalID: fmt.Sprintf("gog:%d", pr.ID),
			Title:      title,
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: Platform, Status: game.StatusOwned, Origin: "GOG"},
		})
	}

	return out
}

// ---- OAuth ----

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
}

func (p *Provider) token(ctx context.Context, q url.Values) (tokenResponse, error) {
	q.Set("client_id", clientID)
	q.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.AuthURL+"/token?"+q.Encode(), nil)
	if err != nil {
		return tokenResponse{}, err
	}

	res, err := p.Client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}

		_ = json.Unmarshal(body, &e)

		switch {
		case q.Get("grant_type") == "authorization_code" && res.StatusCode < 500:
			return tokenResponse{}, ErrBadCode
		case q.Get("grant_type") == "refresh_token" && res.StatusCode < 500:
			return tokenResponse{}, ErrSessionExpired
		}

		return tokenResponse{}, fmt.Errorf("gog sign-in: HTTP %d %s", res.StatusCode, e.Error)
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("gog sign-in: %w", err)
	}

	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return tokenResponse{}, errors.New("gog sign-in: response without tokens")
	}

	return tok, nil
}

// remember caches the access token and returns the session to store.
func (p *Provider) remember(tok tokenResponse) session {
	p.mu.Lock()
	if p.tokens == nil {
		p.tokens = map[string]accessToken{}
	}

	p.tokens[tok.UserID] = accessToken{token: tok.AccessToken, expires: p.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
	p.mu.Unlock()

	return session{RefreshToken: tok.RefreshToken, UserID: tok.UserID}
}

// accessToken returns a valid access token, refreshing the session when needed. A refresh rotates
// the refresh token, so the new session is written back into settings for the caller to persist.
func (p *Provider) accessToken(ctx context.Context, settings source.Settings) (string, error) {
	var s session
	if raw := settings[settingSession]; raw == "" || json.Unmarshal([]byte(raw), &s) != nil || s.RefreshToken == "" {
		return "", ErrNoSession
	}

	p.mu.Lock()
	cached, ok := p.tokens[s.UserID]
	p.mu.Unlock()

	if ok && p.Now().Add(time.Minute).Before(cached.expires) {
		return cached.token, nil
	}

	tok, err := p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}})
	if err != nil {
		return "", err
	}

	raw, _ := json.Marshal(p.remember(tok))
	settings[settingSession] = string(raw)

	return tok.AccessToken, nil
}

func (p *Provider) forget(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for k, v := range p.tokens {
		if v.token == token {
			delete(p.tokens, k)
		}
	}
}

func (p *Provider) get(ctx context.Context, token, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	res, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusUnauthorized || (res.StatusCode >= 300 && res.StatusCode < 400):
		p.forget(token) // next call refreshes the session
		return ErrSessionExpired
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("gog %s: HTTP %d", req.URL.Path, res.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out)
}
