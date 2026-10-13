// Package amazon implements the Amazon Games / Prime Gaming source provider. Amazon has no public
// library API, so it signs in as the Amazon Games launcher does, the way nile (Heroic's Amazon
// backend) works:
//
//  1. the user signs in with their Amazon account through the login link, which registers a
//     launcher "device" (PKCE); the browser lands on amazon.com with a one-time code in the
//     address, which the user pastes;
//  2. the code registers the device and gives a refresh token, which does not rotate;
//  3. each scan turns it into an access token and reads the games the account owns (bought or
//     claimed with Prime Gaming) from the launcher's entitlements service.
//
// It only reads: nothing is bought, claimed, installed or changed in the account.
package amazon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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

// Type is the source type id of Amazon Games accounts.
const Type source.Type = "amazon"

// Platform is the store name used on copies.
const Platform = game.PlatformAmazon

const (
	settingCode    = "login_code"
	settingSession = "session"

	// The Amazon Games launcher for Windows, as registered by nile.
	deviceType = "A2UMVHOX7UP4V7"
	appName    = "AGSLauncher for Windows"
	appVersion = "1.0.0"

	// accountUserAgent is what the launcher sends to the account API; userAgent to the
	// entitlements service.
	accountUserAgent = "AGSLauncher/1.0.0"
	userAgent        = "com.amazon.agslauncher.win/3.0.9202.1"

	// keyID is the launcher's fixed entitlements key (from nile).
	keyID = "d5dc8b8b-86c8-4fc4-ae93-18c0def5314d"

	defaultSignInURL = "https://amazon.com/ap/signin"
	defaultAPIURL    = "https://api.amazon.com"
	defaultGamingURL = "https://gaming.amazon.com"

	pageSize = 50
	maxPages = 100
)

var (
	// ErrNoCredentials means there is neither a stored session nor a code to start one.
	ErrNoCredentials = errkind.New(errkind.Invalid, "amazon: open the sign-in link, sign in and paste the address of the amazon.com page you land on (see the help below the field)")

	// ErrBadCode means Amazon rejected the code.
	ErrBadCode = errkind.New(errkind.Invalid, "amazon rejected the code: it works once, expires within minutes and only with the sign-in link of this Game Vault since it last started. Reload the page, open the sign-in link again and paste the new address")

	// ErrSignedOut means the stored session can no longer be used.
	ErrSignedOut = source.SignedOut("the Amazon session is no longer valid (the Game Vault device may have been removed from your Amazon account): open the sign-in link and paste a new address")

	reCode  = regexp.MustCompile(`openid\.oa2\.authorization_code=([^&#\s"]+)`)
	reSteam = regexp.MustCompile(`store\.steampowered\.com/app/(\d+)`)
)

// session is what is stored: the refresh token and the device serial it was registered with
// (entitlement requests carry a hash of it).
type session struct {
	RefreshToken string `json:"refresh_token"`
	Serial       string `json:"serial"`
}

// login is the device a sign-in link registers; it lives in memory until a code uses it.
type login struct {
	serial   string
	clientID string
	verifier string
	url      string
}

// Provider implements sync.Provider and sync.Preparer for Amazon Games accounts.
type Provider struct {
	// SignInURL is the Amazon sign-in page the login link opens.
	SignInURL string

	// API calls the Amazon account API (device registration, tokens); tests point it at a fake
	// server.
	API *apiclient.Client

	// Gaming calls the launcher's entitlements service.
	Gaming *apiclient.Client

	// Now is the clock.
	Now func() time.Time

	// sessions exchanges the pasted codes and keeps the sessions' access tokens.
	sessions *oauthsession.Sessions[session]

	mu    gosync.Mutex
	login login
}

// NewProvider returns the Amazon Games source with its production endpoints.
func NewProvider() *Provider {
	p := &Provider{
		SignInURL: defaultSignInURL,
		API:       newClient(defaultAPIURL, accountUserAgent),
		Gaming:    newClient(defaultGamingURL, userAgent),
		Now:       time.Now,
	}
	p.sessions = &oauthsession.Sessions[session]{
		CodeField:  settingCode,
		StateField: settingSession,
		Clean:      func(v string) (string, error) { return cleanCode(v), nil },
		Exchange:   p.register,
		Refresh:    p.refresh,
		// The refresh token does not rotate: it identifies the session for as long as it lives.
		RefreshToken:     func(s session) string { return s.RefreshToken },
		ErrNoCredentials: ErrNoCredentials,
		ErrSignedOut:     ErrSignedOut,
		Now:              func() time.Time { return p.Now() },
	}

	return p
}

// newClient returns a client of one of Amazon's services, sent as the launcher.
func newClient(baseURL, ua string) *apiclient.Client {
	c := apiclient.New(baseURL)
	c.HTTP.Timeout = 30 * time.Second
	c.UserAgent = ua

	return c
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports.
var LinkedStore = game.Store{
	Key:  game.LinkAmazon,
	Name: "Amazon Games",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider. The sign-in link carries this process's device and PKCE
// challenge, so a code only works with the link of the running server.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Amazon Games",
		DescriptionKey: "sources.amazon.description",
		Fields: []schema.Field{
			{
				Key:      settingCode,
				LabelKey: "sources.amazon.code",
				HelpKey:  "sources.amazon.codeHelp",
				HelpURL:  p.currentLogin().url,
				// No Open: the sign-in link changes with every run and is the field's HelpURL.
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Hosts:   []string{"www.amazon.com"}, // the link opens on amazon.com and lands on www
					Capture: schema.Capture{
						Redirect: &schema.RedirectCapture{
							Prefix: "https://www.amazon.com/",
							Param:  "openid.oa2.authorization_code",
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

// currentLogin returns the device the sign-in link registers, creating one if needed.
func (p *Provider) currentLogin() login {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.login.url == "" {
		p.login = newLogin(p.SignInURL)
	}

	return p.login
}

func newLogin(signInURL string) login {
	serial := strings.ToUpper(randomHex(16))
	clientID := hex.EncodeToString([]byte(serial + "#" + deviceType))
	verifier := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	challenge := sha256.Sum256([]byte(verifier))

	q := url.Values{
		"openid.ns":                        {"http://specs.openid.net/auth/2.0"},
		"openid.claimed_id":                {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.identity":                  {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.mode":                      {"checkid_setup"},
		"openid.oa2.scope":                 {"device_auth_access"},
		"openid.ns.oa2":                    {"http://www.amazon.com/ap/ext/oauth/2"},
		"openid.oa2.response_type":         {"code"},
		"openid.oa2.code_challenge_method": {"S256"},
		"openid.oa2.client_id":             {"device:" + clientID},
		"openid.oa2.code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"openid.return_to":                 {"https://www.amazon.com"},
		"openid.pape.max_auth_age":         {"0"},
		"openid.assoc_handle":              {"amzn_sonic_games_launcher"},
		"pageId":                           {"amzn_sonic_games_launcher"},
		"marketPlaceId":                    {"ATVPDKIKX0DER"},
		"language":                         {"en_US"},
	}

	return login{
		serial:   serial,
		clientID: clientID,
		verifier: verifier,
		url:      signInURL + "?" + q.Encode(),
	}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails on supported platforms

	return b
}

func randomHex(n int) string { return hex.EncodeToString(randomBytes(n)) }

// cleanCode accepts the whole address of the page Amazon opens, or just the code.
func cleanCode(v string) string {
	v = strings.TrimSpace(v)
	if m := reCode.FindStringSubmatch(v); m != nil {
		if c, err := url.QueryUnescape(m[1]); err == nil {
			return c
		}

		return m[1]
	}

	return strings.Trim(v, `"' `)
}

// ---- Amazon account ----

// post sends a JSON request to the account API and decodes the answer into out. Amazon answers
// rejected values with HTTP 400 "InvalidValue", reported as rejected (so are a 401 and a 403). A
// 429 or an outage is a passing trouble, never "sign in again".
func (p *Provider) post(ctx context.Context, path string, body, out any) (rejected bool, err error) {
	res, err := p.API.Send(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	})
	if err != nil {
		return false, apiclient.ClassifyError(ctx, err)
	}

	switch {
	case apiclient.Rejected(res.Status):
		return true, nil
	case !res.OK():
		return false, fmt.Errorf("amazon sign-in: %w", apiclient.ClassifyStatus(res.Status))
	}

	if err := res.Decode(out); err != nil {
		return false, errors.New("amazon sign-in: unexpected answer")
	}

	return false, nil
}

// register exchanges a code for a session, registering the device of the current sign-in link
// (oauthsession.Sessions.Exchange).
func (p *Provider) register(ctx context.Context, code string) (oauthsession.Grant[session], error) {
	l := p.currentLogin()
	body := map[string]any{
		"auth_data": map[string]any{
			"authorization_code": code, "client_domain": "DeviceLegacy", "client_id": l.clientID,
			"code_algorithm": "SHA-256", "code_verifier": l.verifier, "use_global_authentication": false,
		},
		"registration_data": map[string]any{
			"app_name": appName, "app_version": appVersion, "device_model": "Windows", "device_name": nil,
			"device_serial": l.serial, "device_type": deviceType, "domain": "Device", "os_version": "10.0.19044.0",
		},
		"requested_extensions": []string{"customer_info", "device_info"},
		"requested_token_type": []string{"bearer", "mac_dms"},
		"user_context_map":     map[string]any{},
	}

	var out struct {
		Response struct {
			Success struct {
				Tokens struct {
					Bearer struct {
						AccessToken  string    `json:"access_token"`
						RefreshToken string    `json:"refresh_token"`
						ExpiresIn    expiresIn `json:"expires_in"`
					} `json:"bearer"`
				} `json:"tokens"`
			} `json:"success"`
		} `json:"response"`
	}

	rejected, err := p.post(ctx, "/auth/register", body, &out)
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	b := out.Response.Success.Tokens.Bearer
	if rejected || b.RefreshToken == "" {
		return oauthsession.Grant[session]{}, ErrBadCode
	}

	// A registered device is used up: the next sign-in link registers a new one.
	p.mu.Lock()
	p.login = login{}
	p.mu.Unlock()

	return oauthsession.Grant[session]{
		Session: session{
			RefreshToken: b.RefreshToken,
			Serial:       l.serial,
		},
		AccessToken: b.AccessToken,
		Lifetime:    time.Duration(b.ExpiresIn),
	}, nil
}

// refresh gets a new access token for the session (oauthsession.Sessions.Refresh). The refresh
// token does not rotate, so the session stays the same.
func (p *Provider) refresh(ctx context.Context, s session) (oauthsession.Grant[session], error) {
	var out struct {
		AccessToken string    `json:"access_token"`
		ExpiresIn   expiresIn `json:"expires_in"`
	}

	rejected, err := p.post(ctx, "/auth/token", map[string]string{
		"source_token": s.RefreshToken, "source_token_type": "refresh_token", "requested_token_type": "access_token",
		"app_name": appName, "app_version": appVersion,
	}, &out)
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	if rejected || out.AccessToken == "" {
		return oauthsession.Grant[session]{}, ErrSignedOut
	}

	return oauthsession.Grant[session]{
		Session:     s,
		AccessToken: out.AccessToken,
		Lifetime:    time.Duration(out.ExpiresIn),
	}, nil
}

// expiresIn is Amazon's expires_in: a number of seconds, sent as a string by /auth/register and
// as a number by /auth/token.
type expiresIn time.Duration

// UnmarshalJSON accepts both forms; anything else counts as an hour.
func (e *expiresIn) UnmarshalJSON(b []byte) error {
	n, err := strconv.Atoi(strings.Trim(string(b), `"`))
	if err != nil || n <= 0 {
		n = 3600
	}

	*e = expiresIn(time.Duration(n) * time.Second)

	return nil
}

// Prepare implements sync.Preparer: a pasted code is exchanged for a session, which is stored
// instead of the code (codes work once).
func (p *Provider) Prepare(ctx context.Context, settings schema.Settings) (schema.Settings, error) {
	return p.sessions.Prepare(ctx, settings)
}

// ---- Library ----

type entitlement struct {
	Product struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		ProductDetail struct {
			Details struct {
				Websites struct {
					Steam string `json:"steam"`
				} `json:"websites"`
			} `json:"details"`
		} `json:"productDetail"`
	} `json:"product"`
}

// errTokenRejected means the entitlements service refused the access token (HTTP 401 or 403): it
// may have expired before its time. The sessions renew it once; only a refused refresh is signed
// out.
var errTokenRejected = fmt.Errorf("amazon rejected the access token: %w", apiclient.ErrTokenRejected)

// page reads one page of entitlements.
func (p *Provider) page(ctx context.Context, access, serial, next string, size int) ([]entitlement, string, error) {
	hw := sha256.Sum256([]byte(serial))

	var out struct {
		Entitlements []entitlement `json:"entitlements"`
		NextToken    string        `json:"nextToken"`
	}

	err := p.Gaming.Do(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/api/distribution/entitlements",
		Header: http.Header{
			"X-Amz-Target":     {"com.amazon.animusdistributionservice.entitlement.AnimusEntitlementsService.GetEntitlements"},
			"x-amzn-token":     {access},
			"Content-Encoding": {"amz-1.0"},
		},
		Body: map[string]any{
			"Operation": "GetEntitlements", "clientId": "Sonic", "syncPoint": nil, "nextToken": nilIfEmpty(next),
			"maxResults": size, "productIdFilter": nil, "keyId": keyID,
			"hardwareHash": strings.ToUpper(hex.EncodeToString(hw[:])),
		},
	}, &out)

	switch {
	case err == nil:
		return out.Entitlements, out.NextToken, nil
	case apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden):
		return nil, "", errTokenRejected
	}

	// A 429 or an outage is a passing trouble, never "sign in again".
	return nil, "", fmt.Errorf("amazon library: %w", apiclient.ClassifyError(ctx, err))
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// all reads every page of entitlements with one access token.
func (p *Provider) all(ctx context.Context, access, serial string) ([]entitlement, error) {
	var (
		items []entitlement
		next  string
	)

	for range maxPages {
		batch, n, err := p.page(ctx, access, serial, next, pageSize)
		if err != nil {
			return nil, err
		}

		items = append(items, batch...)

		if n == "" || n == next {
			break
		}

		next = n
	}

	return items, nil
}

// owned reads the account's games with read (every page, or a sample), renewing the access token
// once if it was rejected.
func (p *Provider) owned(ctx context.Context, settings schema.Settings, read func(ctx context.Context, access, serial string) ([]entitlement, error)) ([]entitlement, error) {
	s, err := p.sessions.Current(settings)
	if err != nil {
		return nil, err
	}

	var items []entitlement

	err = p.sessions.WithRetry(ctx, settings, func(access string) error {
		got, err := read(ctx, access, s.Serial)
		items = got

		return err
	})
	if err != nil {
		return nil, err
	}

	return items, nil
}

// mapEntitlements turns entitlements into copies, one per product, and warns about products
// without a title.
func mapEntitlements(items []entitlement) ([]game.ImportedCopy, []string) {
	var (
		copies   []game.ImportedCopy
		warnings []string
	)

	seen := map[string]bool{}

	for _, e := range items {
		id := e.Product.ID
		if id == "" || seen[id] {
			continue
		}

		seen[id] = true

		title := strings.TrimSpace(e.Product.Title)
		if title == "" {
			warnings = append(warnings, "Amazon returned a game without a title (product "+id+"); it was skipped")
			continue
		}

		links := game.Links{LinkedStore.Key: id}
		if m := reSteam.FindStringSubmatch(e.Product.ProductDetail.Details.Websites.Steam); m != nil {
			links[game.LinkSteam] = m[1]
		}

		copies = append(copies, game.ImportedCopy{
			ExternalID: "amazon:" + id,
			Title:      title,
			Links:      links,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   Platform,
			},
		})
	}

	return copies, warnings
}

// Test implements sync.Provider: it asks for one entitlement, which proves the session (renewing
// the access token on the way, so the newest session is handed back) without reading the library.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.owned(ctx, settings, func(ctx context.Context, access, serial string) ([]entitlement, error) {
		items, _, err := p.page(ctx, access, serial, "", 1)

		return items, err
	})

	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
	items, err := p.owned(ctx, settings, p.all)
	if err != nil {
		return nil, nil, err
	}

	copies, warnings := mapEntitlements(items)
	if len(copies) == 0 {
		warnings = append(warnings, "Amazon returned no games for this account (Prime Gaming codes for other stores are not listed here)")
	}

	return copies, warnings, nil
}
