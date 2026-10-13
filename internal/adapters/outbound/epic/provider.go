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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthsession"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Epic Games Store accounts.
const Type source.Type = "epic"

// Platform is the store name used on copies, matching Humble's Epic keys so they are flagged as
// redundant when the game is already in the Epic library.
const Platform = game.PlatformEpic

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

	workers = 4
)

// LoginURL is where the user signs in to get an authorization code.
const LoginURL = "https://www.epicgames.com/id/login?redirectUrl=" +
	"https%3A%2F%2Fwww.epicgames.com%2Fid%2Fapi%2Fredirect%3FclientId%3D" + clientID + "%26responseType%3Dcode"

var (
	// ErrNoCredentials means there is neither a stored session nor a code to start one.
	ErrNoCredentials = errkind.New(errkind.Invalid, "epic: paste an authorization code (open the login link, sign in and copy \"authorizationCode\")")

	// ErrBadCode means Epic rejected the authorization code.
	ErrBadCode = errkind.New(errkind.Invalid, "epic rejected the authorization code: codes work once and expire within minutes, get a new one from the login link")

	// ErrSignedOut means the stored session can no longer be refreshed.
	ErrSignedOut = source.SignedOut("the Epic session expired: open the login link and paste a new authorization code")

	// ErrEmailCode means the user pasted the sign-in code Epic emails, not the authorization code.
	ErrEmailCode = errkind.New(errkind.Invalid, "that is the sign-in code Epic emails you, not the authorization code: after signing in, open the login link again and copy \"authorizationCode\" from the page it shows")

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

// Provider implements sync.Provider and sync.Preparer for Epic accounts.
type Provider struct {
	// OAuth calls Epic's account service (sign-in, tokens); tests point it at a fake server.
	OAuth *apiclient.Client

	// Library calls the library service.
	Library *apiclient.Client

	// Catalog calls the public catalog.
	Catalog *apiclient.Client

	// Now is the clock.
	Now func() time.Time

	// sessions exchanges the pasted codes and keeps the sessions' access tokens.
	sessions *oauthsession.Sessions[session]

	mu      gosync.Mutex
	catalog map[string]catalogItem // namespace/id → item; titles barely change
}

// NewProvider returns the Epic source with its production endpoints.
func NewProvider() *Provider {
	p := &Provider{
		OAuth:   newClient(defaultOAuthURL),
		Library: newClient(defaultLibraryURL),
		Catalog: newClient(defaultCatalogURL),
		Now:     time.Now,
	}
	p.sessions = &oauthsession.Sessions[session]{
		CodeField:        settingAuthCode,
		StateField:       settingSession,
		Clean:            cleanCode,
		Exchange:         p.exchange,
		Refresh:          p.refresh,
		RefreshToken:     func(s session) string { return s.RefreshToken },
		ErrNoCredentials: ErrNoCredentials,
		ErrSignedOut:     ErrSignedOut,
		Now:              func() time.Time { return p.Now() },
	}

	return p
}

// newClient returns a client of one of Epic's services, sent as the launcher.
func newClient(baseURL string) *apiclient.Client {
	c := apiclient.New(baseURL)
	c.HTTP.Timeout = 30 * time.Second
	c.UserAgent = userAgent

	return c
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{
	Key:  game.LinkEpic,
	Name: "Epic Games",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Epic Games Store",
		DescriptionKey: "sources.epic.description",
		Fields: []schema.Field{
			{
				Key:      settingAuthCode,
				LabelKey: "sources.epic.authCode",
				HelpKey:  "sources.epic.authCodeHelp",
				HelpURL:  LoginURL,
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    LoginURL,
					Capture: schema.Capture{
						Fetch: &schema.FetchCapture{
							URL:   "https://www.epicgames.com/id/api/redirect?clientId=" + clientID + "&responseType=code",
							Field: "authorizationCode",
						},
					},
				},
				Kind: schema.FieldSecret,
			},
			{
				Key:  settingSession,
				Kind: schema.FieldState,
			},
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

// Prepare implements sync.Preparer: a pasted authorization code is exchanged for a session,
// which is stored instead of the code (codes work only once).
func (p *Provider) Prepare(ctx context.Context, settings schema.Settings) (schema.Settings, error) {
	return p.sessions.Prepare(ctx, settings)
}

// Test implements sync.Provider: it reads the first library page, which proves the session (renewed
// on the way, so the rotated one is handed back), without the rest of the library or the catalog.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	return p.sessions.WithRetry(ctx, settings, func(token string) error {
		_, err := p.library(ctx, token, 1)

		return err
	})
}

// errTokenRejected means Epic refused an access token (HTTP 401): it may have expired or been
// revoked before its time. The sessions renew it once; only a refused refresh is signed out.
var errTokenRejected = fmt.Errorf("epic rejected the access token: %w", apiclient.ErrTokenRejected)

// libraryWithToken lists the library with an access token, and once more with a renewed one when
// Epic rejects the first. It returns the token that worked, for the catalog reads that follow.
func (p *Provider) libraryWithToken(ctx context.Context, settings schema.Settings) ([]libraryRecord, string, error) {
	var (
		records []libraryRecord
		used    string
	)

	err := p.sessions.WithRetry(ctx, settings, func(token string) error {
		var err error

		records, err = p.library(ctx, token, maxLibraryPages)
		used = token

		return err
	})
	if err != nil {
		return nil, "", err
	}

	return records, used, nil
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
	records, token, err := p.libraryWithToken(ctx, settings)
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
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   "Epic Games Store",
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

// exchange trades an authorization code for a session (oauthsession.Sessions.Exchange).
func (p *Provider) exchange(ctx context.Context, code string) (oauthsession.Grant[session], error) {
	return p.grant(ctx, url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
		"token_type": {"eg1"},
	})
}

// refresh renews a session (oauthsession.Sessions.Refresh). Epic rotates the refresh token.
func (p *Provider) refresh(ctx context.Context, s session) (oauthsession.Grant[session], error) {
	if !s.RefreshExpiresAt.IsZero() && p.Now().After(s.RefreshExpiresAt) {
		return oauthsession.Grant[session]{}, ErrSignedOut
	}

	return p.grant(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.RefreshToken},
		"token_type":    {"eg1"},
	})
}

// grant calls the token endpoint for a user's session.
func (p *Provider) grant(ctx context.Context, form url.Values) (oauthsession.Grant[session], error) {
	tok, err := requestToken(ctx, p.OAuth, form)
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	if tok.RefreshToken == "" {
		return oauthsession.Grant[session]{}, errors.New("epic sign-in: response without a refresh token")
	}

	return oauthsession.Grant[session]{
		Session: session{
			RefreshToken:     tok.RefreshToken,
			RefreshExpiresAt: tok.RefreshExpiresAt,
			AccountID:        tok.AccountID,
			DisplayName:      tok.DisplayName,
		},
		AccessToken: tok.AccessToken,
		Expires:     tok.ExpiresAt,
	}, nil
}

// requestToken calls the token endpoint of Epic's account service through api, for the source's
// sessions and the covers' application token alike. Client-credentials tokens carry no refresh
// token.
func requestToken(ctx context.Context, api *apiclient.Client, form url.Values) (tokenResponse, error) {
	res, err := api.Send(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/account/api/oauth/token",
		Form:   form,
		Header: http.Header{"Authorization": {apiclient.BasicAuth(clientID, clientSecret)}},
	})
	if err != nil {
		return tokenResponse{}, apiclient.ClassifyError(ctx, err)
	}

	if !res.OK() {
		// Epic refuses a used or unknown code and a stale refresh token with a 400 (an errorCode
		// such as invalid_refresh_token); a 429 or an outage is a passing trouble, after which the
		// same code or session still works.
		switch {
		case form.Get("grant_type") == "authorization_code" && apiclient.Rejected(res.Status):
			return tokenResponse{}, ErrBadCode
		case form.Get("grant_type") == "refresh_token" && apiclient.Rejected(res.Status):
			return tokenResponse{}, ErrSignedOut
		}

		var e epicError
		if res.Decode(&e) == nil && e.ErrorCode != "" {
			return tokenResponse{}, fmt.Errorf("epic sign-in: %s: %w", e.ErrorCode, apiclient.ClassifyStatus(res.Status))
		}

		return tokenResponse{}, fmt.Errorf("epic sign-in: %w", apiclient.ClassifyStatus(res.Status))
	}

	var tok tokenResponse
	if err := res.Decode(&tok); err != nil {
		return tokenResponse{}, fmt.Errorf("epic sign-in: %w", err)
	}

	if tok.AccessToken == "" {
		return tokenResponse{}, errors.New("epic sign-in: response without a token")
	}

	return tok, nil
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

// maxLibraryPages is far beyond any real library; it guards against a paging loop.
const maxLibraryPages = 200

// library reads up to pages pages of the library.
func (p *Provider) library(ctx context.Context, token string, pages int) ([]libraryRecord, error) {
	var out []libraryRecord

	cursor := ""

	for range pages {
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
		if err := p.get(ctx, p.Library, token, "/library/api/public/items", q, &page); err != nil {
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
	path := "/catalog/api/shared/namespace/" + url.PathEscape(r.Namespace) + "/bulk/items"

	var items map[string]catalogItem
	if err := p.get(ctx, p.Catalog, token, path, q, &items); err != nil {
		return catalogItem{}, err
	}

	it, ok := items[r.CatalogItemID]
	if !ok {
		return catalogItem{}, fmt.Errorf("epic catalog: item %s not found", r.CatalogItemID)
	}

	return it, nil
}

// get reads one of Epic's APIs with an access token. A 401 is errTokenRejected, which the sessions
// renew once; a 429 or an outage is a passing trouble.
func (p *Provider) get(ctx context.Context, api *apiclient.Client, token, path string, q url.Values, out any) error {
	err := api.Do(ctx, apiclient.Request{
		Path:   path,
		Query:  q,
		Header: http.Header{"Authorization": {"bearer " + token}},
	}, out)

	switch {
	case err == nil:
		return nil
	case apiclient.IsStatus(err, http.StatusUnauthorized):
		return errTokenRejected
	}

	return fmt.Errorf("epic %s: %w", path, apiclient.ClassifyError(ctx, err))
}
