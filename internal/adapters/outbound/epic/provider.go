// Package epic implements the Epic Games Store source provider. Epic has no public library API,
// so it talks to the same endpoints the Epic launcher uses, the way Legendary and Heroic do:
//
//  1. the user signs in on epicgames.com and pastes the one-time authorization code it shows;
//  2. the code is exchanged for a session (a refresh token that rotates on every use);
//  3. scans list the library and read each item's title from the public catalog.
//
// It only reads the library: nothing is bought, installed or changed in the account.
package epic

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
	"slices"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Epic Games Store accounts.
const Type source.Type = "epic"

// Platform is the store name used on copies, matching Humble's Epic keys so they are flagged as
// redundant when the game is already in the Epic library.
const Platform = "Epic Games"

const (
	settingAuthCode = "authorization_code"
	settingSession  = "session"

	// The Epic launcher's public client credentials, the same ones Legendary uses.
	clientID     = "34a02cf8f4414e29b15921876da36f9a"
	clientSecret = "daafbccc737745039dffe53d94fc76cf"
	userAgent    = "UELauncher/11.0.1-14907503+++Portal+Release-Live Windows/10.0.19041.1.256.64bit"

	defaultOAuthURL   = "https://account-public-service-prod03.ol.epicgames.com"
	defaultLibraryURL = "https://library-service.live.use1a.on.epicgames.com"
	defaultCatalogURL = "https://catalog-public-service-prod06.ol.epicgames.com"

	// pendingTTL keeps the session of a just-exchanged code, so "Test" and then "Save" can both
	// use the same one-time code.
	pendingTTL = 15 * time.Minute
	workers    = 4
)

// LoginURL is where the user signs in to get an authorization code.
const LoginURL = "https://www.epicgames.com/id/login?redirectUrl=" +
	"https%3A%2F%2Fwww.epicgames.com%2Fid%2Fapi%2Fredirect%3FclientId%3D" + clientID + "%26responseType%3Dcode"

var (
	// ErrNoSession means there is neither a stored session nor a code to start one.
	ErrNoSession = errors.New("epic: paste an authorization code (open the login link, sign in and copy \"authorizationCode\")")

	// ErrBadCode means Epic rejected the authorization code.
	ErrBadCode = errors.New("epic rejected the authorization code: codes work once and expire within minutes, get a new one from the login link")

	// ErrSessionExpired means the stored session can no longer be refreshed.
	ErrSessionExpired = errors.New("the Epic session expired: open the login link and paste a new authorization code")

	// ErrEmailCode means the user pasted the sign-in code Epic emails, not the authorization code.
	ErrEmailCode = errors.New("that is the sign-in code Epic emails you, not the authorization code: after signing in, open the login link again and copy \"authorizationCode\" from the page it shows")

	reCodeJSON  = regexp.MustCompile(`"authorizationCode"\s*:\s*(null|"([^"]*)")`)
	reEmailCode = regexp.MustCompile(`^\d{4,8}$`)
)

// session is what the source stores between scans.
type session struct {
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	AccountID        string    `json:"account_id"`
	DisplayName      string    `json:"display_name"`
}

type accessToken struct {
	token   string
	expires time.Time
}

// Provider implements sync.Provider and sync.Preparer for Epic accounts.
type Provider struct {
	OAuthURL, LibraryURL, CatalogURL string
	Client                           *http.Client
	Now                              func() time.Time

	mu      gosync.Mutex
	pending map[string]pendingSession // sha256(code) → session from a recent exchange
	tokens  map[string]accessToken    // account id → access token
	catalog map[string]catalogItem    // namespace/id → item; titles barely change
}

type pendingSession struct {
	s       session
	expires time.Time
}

// NewProvider returns the Epic source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{
		OAuthURL: defaultOAuthURL, LibraryURL: defaultLibraryURL, CatalogURL: defaultCatalogURL,
		Client: &http.Client{Timeout: 30 * time.Second}, Now: time.Now,
	}
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{Key: "epic", Name: "Epic Games"}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Epic Games Store",
		DescriptionKey: "sources.epic.description",
		Fields: []source.Field{
			{Key: settingAuthCode, LabelKey: "sources.epic.authCode", HelpKey: "sources.epic.authCodeHelp",
				HelpURL: LoginURL, Kind: source.FieldSecret},
			{Key: settingSession, Kind: source.FieldState},
		},
	}
}

// cleanCode accepts the bare code or the whole JSON page Epic shows after signing in.
func cleanCode(v string) (string, error) {
	v = strings.TrimSpace(v)
	if m := reCodeJSON.FindStringSubmatch(v); m != nil {
		if m[1] == "null" {
			return "", errors.New("epic showed no code: sign in on epicgames.com first, then open the login link again")
		}

		return m[2], nil
	}

	v = strings.Trim(v, `"' `)
	if reEmailCode.MatchString(v) {
		return "", ErrEmailCode
	}

	return v, nil
}

func hashCode(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

// Prepare implements sync.Preparer: a pasted authorization code is exchanged for a session,
// which is stored instead of the code (codes work only once).
func (p *Provider) Prepare(ctx context.Context, settings source.Settings) (source.Settings, error) {
	out := source.Settings{}
	for k, v := range settings {
		out[k] = v
	}

	code, err := cleanCode(settings[settingAuthCode])
	if err != nil {
		return nil, err
	}

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
		tok, err := p.oauth(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "token_type": {"eg1"}})
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

// Test implements sync.Provider: it lists the library without reading the catalog.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	token, err := p.accessToken(ctx, settings)
	if err != nil {
		return err
	}

	_, err = p.library(ctx, token)

	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	token, err := p.accessToken(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	records, err := p.library(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	items, failed := p.catalogItems(ctx, token, records)
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	var warnings []string
	if failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d library items could not be read from the Epic catalog; they are retried on the next scan", failed))
	}

	return mapLibrary(records, items), warnings, nil
}

// mapLibrary turns library records into copies: games only (no DLC, add-ons or Unreal assets),
// one copy per catalog item.
func mapLibrary(records []libraryRecord, items map[string]catalogItem) []game.ImportedCopy {
	seen := map[string]bool{}

	var out []game.ImportedCopy

	for _, r := range records {
		it, ok := items[r.key()]
		if !ok || !it.isGame() || seen[r.CatalogItemID] {
			continue
		}

		seen[r.CatalogItemID] = true
		out = append(out, game.ImportedCopy{
			ExternalID: "epic:" + r.CatalogItemID,
			Links:      game.Links{LinkedStore.Key: r.CatalogItemID},
			Title:      strings.TrimSpace(it.Title),
			Details: game.CopyDetails{
				Kind: game.KindLibrary, Platform: Platform, Status: game.StatusOwned, Origin: "Epic Games Store",
			},
		})
	}

	return out
}

// ---- OAuth ----

type tokenResponse struct {
	AccessToken      string    `json:"access_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	AccountID        string    `json:"account_id"`
	DisplayName      string    `json:"displayName"`
}

type epicError struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

func (p *Provider) oauth(ctx context.Context, form url.Values) (tokenResponse, error) {
	tok, err := p.oauthRaw(ctx, form)
	if err == nil && tok.RefreshToken == "" {
		err = errors.New("epic sign-in: response without a refresh token")
	}

	return tok, err
}

// oauthRaw calls the token endpoint. Client-credentials tokens carry no refresh token.
func (p *Provider) oauthRaw(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.OAuthURL+"/account/api/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}

	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var e epicError

		_ = json.Unmarshal(body, &e)

		switch {
		case form.Get("grant_type") == "authorization_code" && res.StatusCode < 500:
			return tokenResponse{}, ErrBadCode
		case form.Get("grant_type") == "refresh_token" && res.StatusCode < 500:
			return tokenResponse{}, ErrSessionExpired
		}

		return tokenResponse{}, fmt.Errorf("epic sign-in: HTTP %d %s", res.StatusCode, e.ErrorCode)
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("epic sign-in: %w", err)
	}

	if tok.AccessToken == "" {
		return tokenResponse{}, errors.New("epic sign-in: response without a token")
	}

	return tok, nil
}

// remember caches the access token and returns the session to store.
func (p *Provider) remember(tok tokenResponse) session {
	p.mu.Lock()
	if p.tokens == nil {
		p.tokens = map[string]accessToken{}
	}

	p.tokens[tok.AccountID] = accessToken{token: tok.AccessToken, expires: tok.ExpiresAt}
	p.mu.Unlock()

	return session{RefreshToken: tok.RefreshToken, RefreshExpiresAt: tok.RefreshExpiresAt, AccountID: tok.AccountID, DisplayName: tok.DisplayName}
}

// accessToken returns a valid access token, refreshing the session when needed. A refresh rotates
// the refresh token, so the new session is written back into settings for the caller to persist.
func (p *Provider) accessToken(ctx context.Context, settings source.Settings) (string, error) {
	var s session
	if raw := settings[settingSession]; raw == "" || json.Unmarshal([]byte(raw), &s) != nil || s.RefreshToken == "" {
		return "", ErrNoSession
	}

	p.mu.Lock()
	cached, ok := p.tokens[s.AccountID]
	p.mu.Unlock()

	if ok && p.Now().Add(time.Minute).Before(cached.expires) {
		return cached.token, nil
	}

	if !s.RefreshExpiresAt.IsZero() && p.Now().After(s.RefreshExpiresAt) {
		return "", ErrSessionExpired
	}

	tok, err := p.oauth(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}, "token_type": {"eg1"}})
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

// ---- Library and catalog ----

type libraryRecord struct {
	Namespace     string `json:"namespace"`
	CatalogItemID string `json:"catalogItemId"`
	AppName       string `json:"appName"`
	SandboxType   string `json:"sandboxType"`
}

func (r libraryRecord) key() string { return r.Namespace + "/" + r.CatalogItemID }

type catalogItem struct {
	Title      string `json:"title"`
	Categories []struct {
		Path string `json:"path"`
	} `json:"categories"`
	MainGameItem *struct {
		ID string `json:"id"`
	} `json:"mainGameItem"`
}

// isGame excludes DLC (which point to their main game), add-ons and plain applications.
func (it catalogItem) isGame() bool {
	if it.MainGameItem != nil || strings.TrimSpace(it.Title) == "" {
		return false
	}

	var paths []string
	for _, c := range it.Categories {
		paths = append(paths, c.Path)
	}

	if slices.Contains(paths, "addons") {
		return false
	}

	return len(paths) == 0 || slices.Contains(paths, "games")
}

func (p *Provider) library(ctx context.Context, token string) ([]libraryRecord, error) {
	var out []libraryRecord

	cursor := ""

	for range 200 { // ~ 200 pages is far beyond any real library; it guards against a loop
		q := url.Values{"includeMetadata": {"true"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}

		var page struct {
			Records          []libraryRecord `json:"records"`
			ResponseMetadata struct {
				NextCursor string `json:"nextCursor"`
			} `json:"responseMetadata"`
		}
		if err := p.get(ctx, token, p.LibraryURL+"/library/api/public/items?"+q.Encode(), &page); err != nil {
			return nil, err
		}

		for _, r := range page.Records {
			// "ue" holds Unreal Engine marketplace assets; private sandboxes are dev/test builds.
			if r.Namespace == "ue" || r.SandboxType == "PRIVATE" || r.CatalogItemID == "" {
				continue
			}

			out = append(out, r)
		}

		if page.ResponseMetadata.NextCursor == "" || page.ResponseMetadata.NextCursor == cursor {
			return out, nil
		}

		cursor = page.ResponseMetadata.NextCursor
	}

	return out, nil
}

// catalogItems reads the catalog entry of every record not cached yet, a few at a time.
func (p *Provider) catalogItems(ctx context.Context, token string, records []libraryRecord) (map[string]catalogItem, int) {
	p.mu.Lock()
	if p.catalog == nil {
		p.catalog = map[string]catalogItem{}
	}

	var todo []libraryRecord
	for _, r := range records {
		if _, ok := p.catalog[r.key()]; !ok && !slices.ContainsFunc(todo, func(t libraryRecord) bool { return t.key() == r.key() }) {
			todo = append(todo, r)
		}
	}
	p.mu.Unlock()

	var (
		wg     gosync.WaitGroup
		mu     gosync.Mutex
		failed int
		jobs   = make(chan libraryRecord)
	)
	for range workers {
		wg.Go(func() {
			for r := range jobs {
				it, err := p.catalogItem(ctx, token, r)

				mu.Lock()
				if err != nil {
					failed++
				} else {
					p.mu.Lock()
					p.catalog[r.key()] = it
					p.mu.Unlock()
				}
				mu.Unlock()
			}
		})
	}

	for _, r := range todo {
		if ctx.Err() != nil {
			break
		}

		jobs <- r
	}

	close(jobs)
	wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	out := make(map[string]catalogItem, len(records))
	for _, r := range records {
		if it, ok := p.catalog[r.key()]; ok {
			out[r.key()] = it
		}
	}

	return out, failed
}

func (p *Provider) catalogItem(ctx context.Context, token string, r libraryRecord) (catalogItem, error) {
	q := url.Values{
		"id": {r.CatalogItemID}, "includeDLCDetails": {"true"}, "includeMainGameDetails": {"true"},
		"country": {"US"}, "locale": {"en-US"},
	}
	u := fmt.Sprintf("%s/catalog/api/shared/namespace/%s/bulk/items?%s", p.CatalogURL, url.PathEscape(r.Namespace), q.Encode())

	var items map[string]catalogItem
	if err := p.get(ctx, token, u, &items); err != nil {
		return catalogItem{}, err
	}

	it, ok := items[r.CatalogItemID]
	if !ok {
		return catalogItem{}, fmt.Errorf("epic catalog: item %s not found", r.CatalogItemID)
	}

	return it, nil
}

func (p *Provider) get(ctx context.Context, token, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("User-Agent", userAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusUnauthorized:
		p.forget(token) // next call refreshes the session
		return ErrSessionExpired
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("epic %s: HTTP %d", req.URL.Path, res.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out)
}
