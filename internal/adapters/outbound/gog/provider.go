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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthsession"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
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

	maxPages = 200
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
	ErrSessionExpired = source.SignedOut("the GOG session expired: open the login link and paste a new code")

	reCodeParam = regexp.MustCompile(`[?&]code=([^&#\s"]+)`)
)

// session is what the source stores between scans.
type session struct {
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
}

// Provider implements sync.Provider and sync.Preparer for GOG accounts.
type Provider struct {
	// Auth calls GOG's sign-in service (codes and tokens); tests point it at a fake server.
	Auth *apiclient.Client

	// Embed calls the account API the GOG website uses (the owned products).
	Embed *apiclient.Client

	// Now is the clock.
	Now func() time.Time

	// sessions exchanges the pasted codes and keeps the sessions' access tokens.
	sessions *oauthsession.Sessions[session]
}

// NewProvider returns the GOG source with its production endpoints.
func NewProvider() *Provider {
	p := &Provider{
		Auth:  newClient(defaultAuthURL),
		Embed: newClient(defaultEmbedURL),
		Now:   time.Now,
	}
	p.sessions = &oauthsession.Sessions[session]{
		CodeField:    settingAuthCode,
		StateField:   settingSession,
		Clean:        func(v string) (string, error) { return cleanCode(v), nil },
		Exchange:     p.exchange,
		Refresh:      p.refresh,
		RefreshToken: func(s session) string { return s.RefreshToken },
		ErrNoSession: ErrNoSession,
		ErrSignedOut: ErrSessionExpired,
		Now:          func() time.Time { return p.Now() },
	}

	return p
}

// newClient returns a client of one of GOG's services. Without a valid session GOG redirects to
// its login page: the redirect is the answer, reported as a rejected token, never followed.
func newClient(baseURL string) *apiclient.Client {
	c := apiclient.New(baseURL)
	c.HTTP.Timeout = 30 * time.Second
	c.NoRedirects = true

	return c
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{
	Key:  "gog",
	Name: "GOG",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "GOG",
		DescriptionKey: "sources.gog.description",
		Fields: []source.Field{
			{
				Key:      settingAuthCode,
				LabelKey: "sources.gog.authCode",
				HelpKey:  "sources.gog.authCodeHelp",
				HelpURL:  LoginURL,
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    LoginURL,
					Hosts:   []string{"embed.gog.com"},
					Capture: schema.Capture{
						Redirect: &schema.RedirectCapture{
							Prefix: "https://embed.gog.com/on_login_success",
							Param:  "code",
						},
					},
				},
				Kind: source.FieldSecret,
			},
			{
				Key:  settingSession,
				Kind: source.FieldState,
			},
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

// Prepare implements sync.Preparer: a pasted code is exchanged for a session, which is stored
// instead of the code (codes work only once).
func (p *Provider) Prepare(ctx context.Context, settings source.Settings) (source.Settings, error) {
	return p.sessions.Prepare(ctx, settings)
}

// Test implements sync.Provider: one request for the list of owned product ids.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	return p.sessions.WithRetry(ctx, settings, func(token string) error {
		var owned struct {
			Owned []int64 `json:"owned"`
		}

		return p.get(ctx, token, "/user/data/games", nil, &owned)
	})
}

type product struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	IsGame  bool   `json:"isGame"`
	IsMovie bool   `json:"isMovie"`
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	var products []product

	err := p.sessions.WithRetry(ctx, settings, func(token string) error {
		products = nil // a renewed token reads every page again

		for page := 1; page <= maxPages; page++ {
			var res struct {
				TotalPages int       `json:"totalPages"`
				Products   []product `json:"products"`
			}

			q := url.Values{"mediaType": {"1"}, "page": {strconv.Itoa(page)}}
			if err := p.get(ctx, token, "/account/getFilteredProducts", q, &res); err != nil {
				return err
			}

			products = append(products, res.Products...)
			if page >= res.TotalPages || len(res.Products) == 0 {
				break
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
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
			Links:      game.Links{LinkedStore.Key: strconv.FormatInt(pr.ID, 10)},
			Title:      title,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   "GOG",
			},
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

// exchange trades a code for a session (oauthsession.Sessions.Exchange).
func (p *Provider) exchange(ctx context.Context, code string) (oauthsession.Grant[session], error) {
	return p.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}})
}

// refresh renews a session (oauthsession.Sessions.Refresh). GOG rotates the refresh token.
func (p *Provider) refresh(ctx context.Context, s session) (oauthsession.Grant[session], error) {
	return p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}})
}

// token calls the token endpoint, which takes everything in the query, as GOG Galaxy sends it.
// apiclient never shows a query in its errors, so neither the client secret nor the code or the
// refresh token reaches a warning or the logs.
func (p *Provider) token(ctx context.Context, q url.Values) (oauthsession.Grant[session], error) {
	q.Set("client_id", clientID)
	q.Set("client_secret", clientSecret)

	res, err := p.Auth.Send(ctx, apiclient.Request{
		Path:  "/token",
		Query: q,
	})
	if err != nil {
		return oauthsession.Grant[session]{}, apiclient.ClassifyError(ctx, err)
	}

	if !res.OK() {
		// GOG refuses a used code or a stale refresh token with a 400 invalid_grant; a 429 or an
		// outage is a passing trouble, after which the same code or session still works.
		switch {
		case q.Get("grant_type") == "authorization_code" && apiclient.Rejected(res.Status):
			return oauthsession.Grant[session]{}, ErrBadCode
		case q.Get("grant_type") == "refresh_token" && apiclient.Rejected(res.Status):
			return oauthsession.Grant[session]{}, ErrSessionExpired
		}

		var e struct {
			Error string `json:"error"`
		}
		if res.Decode(&e) == nil && e.Error != "" {
			return oauthsession.Grant[session]{}, fmt.Errorf("gog sign-in: %s: %w", e.Error, apiclient.ClassifyStatus(res.Status))
		}

		return oauthsession.Grant[session]{}, fmt.Errorf("gog sign-in: %w", apiclient.ClassifyStatus(res.Status))
	}

	var tok tokenResponse
	if err := res.Decode(&tok); err != nil {
		return oauthsession.Grant[session]{}, fmt.Errorf("gog sign-in: %w", err)
	}

	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return oauthsession.Grant[session]{}, errors.New("gog sign-in: response without tokens")
	}

	return oauthsession.Grant[session]{
		Session: session{
			RefreshToken: tok.RefreshToken,
			UserID:       tok.UserID,
		},
		AccessToken: tok.AccessToken,
		Lifetime:    time.Duration(tok.ExpiresIn) * time.Second,
	}, nil
}

// errTokenRejected means GOG refused an access token (HTTP 401, or a redirect to its login page):
// it may have expired or been revoked before its time. The sessions renew it once; only a refused
// refresh is signed out.
var errTokenRejected = fmt.Errorf("gog rejected the access token: %w", apiclient.ErrTokenRejected)

// get reads the account API with an access token. A 429 or an outage is a passing trouble.
func (p *Provider) get(ctx context.Context, token, path string, q url.Values, out any) error {
	err := p.Embed.Do(ctx, apiclient.Request{
		Path:   path,
		Query:  q,
		Header: http.Header{"Authorization": {"Bearer " + token}},
	}, out)

	var se *apiclient.StatusError

	switch {
	case err == nil:
		return nil
	case errors.As(err, &se) && (se.Status == http.StatusUnauthorized || (se.Status >= 300 && se.Status < 400)):
		return errTokenRejected
	}

	return fmt.Errorf("gog %s: %w", path, apiclient.ClassifyError(ctx, err))
}
