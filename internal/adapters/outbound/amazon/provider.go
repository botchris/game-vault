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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Amazon Games accounts.
const Type source.Type = "amazon"

// Platform is the store name used on copies.
const Platform = "Amazon Games"

const (
	settingCode    = "login_code"
	settingSession = "session"

	// The Amazon Games launcher for Windows, as registered by nile.
	deviceType = "A2UMVHOX7UP4V7"
	appName    = "AGSLauncher for Windows"
	appVersion = "1.0.0"
	userAgent  = "com.amazon.agslauncher.win/3.0.9202.1"
	// keyID is the launcher's fixed entitlements key (from nile).
	keyID = "d5dc8b8b-86c8-4fc4-ae93-18c0def5314d"

	defaultSignInURL       = "https://amazon.com/ap/signin"
	defaultAPIURL          = "https://api.amazon.com"
	defaultEntitlementsURL = "https://gaming.amazon.com/api/distribution/entitlements"

	pendingTTL = 15 * time.Minute
	pageSize   = 50
	maxPages   = 100
)

var (
	// ErrNoSession means there is neither a stored session nor a code to start one.
	ErrNoSession = errors.New("amazon: open the sign-in link, sign in and paste the address of the amazon.com page you land on (see the help below the field)")
	// ErrBadCode means Amazon rejected the code.
	ErrBadCode = errors.New("amazon rejected the code: it works once, expires within minutes and only with the sign-in link of this Game Vault since it last started. Reload the page, open the sign-in link again and paste the new address")
	// ErrSignedOut means the stored session can no longer be used.
	ErrSignedOut = errors.New("the Amazon session is no longer valid (the Game Vault device may have been removed from your Amazon account): open the sign-in link and paste a new address")

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
	serial, clientID, verifier, url string
}

type token struct {
	access  string
	expires time.Time
}

// Provider implements sync.Provider, sync.Preparer and sync.Tester for Amazon Games accounts.
type Provider struct {
	SignInURL, APIURL, EntitlementsURL string
	Client                             *http.Client
	Now                                func() time.Time

	mu      gosync.Mutex
	login   login
	pending map[string]session // sha256(code) → session from a recent registration
	tokens  map[string]token   // refresh token → current access token
}

// NewProvider returns the Amazon Games source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{
		SignInURL: defaultSignInURL, APIURL: defaultAPIURL, EntitlementsURL: defaultEntitlementsURL,
		Client: &http.Client{Timeout: 30 * time.Second}, Now: time.Now,
	}
}

// Descriptor implements sync.Provider. The sign-in link carries this process's device and PKCE
// challenge, so a code only works with the link of the running server.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Amazon Games",
		DescriptionKey: "sources.amazon.description",
		Fields: []source.Field{
			{Key: settingCode, LabelKey: "sources.amazon.code", HelpKey: "sources.amazon.codeHelp", HelpURL: p.currentLogin().url, Kind: source.FieldSecret},
			{Key: settingSession, Kind: source.FieldState},
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

	return login{serial: serial, clientID: clientID, verifier: verifier, url: signInURL + "?" + q.Encode()}
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

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---- Amazon account ----

// post sends a JSON request to the account API and decodes the answer into out. Amazon answers
// rejected values with HTTP 400 "InvalidValue", reported as rejected.
func (p *Provider) post(ctx context.Context, path string, body, out any) (rejected bool, err error) {
	raw, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.APIURL+path, bytes.NewReader(raw))
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AGSLauncher/1.0.0")

	res, err := p.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 && res.StatusCode < 500 {
		return true, nil
	}

	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("amazon sign-in: HTTP %d", res.StatusCode)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return false, errors.New("amazon sign-in: unexpected answer")
	}

	return false, nil
}

// register exchanges a code for a session, registering the device of the current sign-in link.
func (p *Provider) register(ctx context.Context, code string) (session, string, time.Duration, error) {
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
		return session{}, "", 0, err
	}

	b := out.Response.Success.Tokens.Bearer
	if rejected || b.RefreshToken == "" {
		return session{}, "", 0, ErrBadCode
	}

	// A registered device is used up: the next sign-in link registers a new one.
	p.mu.Lock()
	p.login = login{}
	p.mu.Unlock()

	return session{RefreshToken: b.RefreshToken, Serial: l.serial}, b.AccessToken, time.Duration(b.ExpiresIn), nil
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
func (p *Provider) Prepare(ctx context.Context, settings source.Settings) (source.Settings, error) {
	out := source.Settings{}
	maps.Copy(out, settings)

	code := cleanCode(settings[settingCode])
	delete(out, settingCode)

	if code == "" || code == source.SecretPlaceholder {
		if out[settingSession] == "" {
			return nil, ErrNoSession
		}

		return out, nil
	}

	key := hash(code)

	p.mu.Lock()
	s, ok := p.pending[key]
	p.mu.Unlock()

	if !ok {
		var (
			access string
			ttl    time.Duration
			err    error
		)

		s, access, ttl, err = p.register(ctx, code)
		if err != nil {
			return nil, err
		}

		p.mu.Lock()
		if p.pending == nil {
			p.pending = map[string]session{}
		}

		p.pending[key] = s
		p.remember(s.RefreshToken, access, ttl)
		p.mu.Unlock()
	}

	raw, _ := json.Marshal(s)
	out[settingSession] = string(raw)

	return out, nil
}

// remember keeps an access token until shortly before it expires. The caller holds p.mu.
func (p *Provider) remember(refresh, access string, ttl time.Duration) {
	if p.tokens == nil {
		p.tokens = map[string]token{}
	}

	p.tokens[refresh] = token{access: access, expires: p.Now().Add(ttl - time.Minute)}
}

// accessToken returns a current access token for the session, refreshing it when needed. fresh
// skips the cached one (the entitlements service rejected it).
func (p *Provider) accessToken(ctx context.Context, s session, fresh bool) (string, error) {
	p.mu.Lock()
	t, ok := p.tokens[s.RefreshToken]
	p.mu.Unlock()

	if ok && !fresh && p.Now().Before(t.expires) {
		return t.access, nil
	}

	var out struct {
		AccessToken string    `json:"access_token"`
		ExpiresIn   expiresIn `json:"expires_in"`
	}

	rejected, err := p.post(ctx, "/auth/token", map[string]string{
		"source_token": s.RefreshToken, "source_token_type": "refresh_token", "requested_token_type": "access_token",
		"app_name": appName, "app_version": appVersion,
	}, &out)
	if err != nil {
		return "", err
	}

	if rejected || out.AccessToken == "" {
		return "", ErrSignedOut
	}

	p.mu.Lock()
	p.remember(s.RefreshToken, out.AccessToken, time.Duration(out.ExpiresIn))
	p.mu.Unlock()

	return out.AccessToken, nil
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

// errExpired means the entitlements service rejected the access token.
var errExpired = errors.New("access token rejected")

// page reads one page of entitlements.
func (p *Provider) page(ctx context.Context, access, serial, next string) ([]entitlement, string, error) {
	hw := sha256.Sum256([]byte(serial))
	body, _ := json.Marshal(map[string]any{
		"Operation": "GetEntitlements", "clientId": "Sonic", "syncPoint": nil, "nextToken": nilIfEmpty(next),
		"maxResults": pageSize, "productIdFilter": nil, "keyId": keyID,
		"hardwareHash": strings.ToUpper(hex.EncodeToString(hw[:])),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.EntitlementsURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("X-Amz-Target", "com.amazon.animusdistributionservice.entitlement.AnimusEntitlementsService.GetEntitlements")
	req.Header.Set("x-amzn-token", access)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "amz-1.0")

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, "", errExpired
	}

	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("amazon library: HTTP %d", res.StatusCode)
	}

	var out struct {
		Entitlements []entitlement `json:"entitlements"`
		NextToken    string        `json:"nextToken"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, "", errors.New("amazon library: unexpected answer")
	}

	return out.Entitlements, out.NextToken, nil
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
		batch, n, err := p.page(ctx, access, serial, next)
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

// owned reads the account's games, renewing the access token once if it was rejected.
func (p *Provider) owned(ctx context.Context, settings source.Settings) ([]entitlement, error) {
	var s session
	if json.Unmarshal([]byte(settings[settingSession]), &s) != nil || s.RefreshToken == "" {
		return nil, ErrNoSession
	}

	for _, fresh := range []bool{false, true} {
		access, err := p.accessToken(ctx, s, fresh)
		if err != nil {
			return nil, err
		}

		items, err := p.all(ctx, access, s.Serial)
		if !errors.Is(err, errExpired) {
			return items, err
		}
	}

	return nil, ErrSignedOut
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

		var appID int64
		if m := reSteam.FindStringSubmatch(e.Product.ProductDetail.Details.Websites.Steam); m != nil {
			appID, _ = strconv.ParseInt(m[1], 10, 64)
		}

		copies = append(copies, game.ImportedCopy{
			ExternalID: "amazon:" + id,
			Title:      title,
			SteamAppID: appID,
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: Platform, Status: game.StatusOwned},
		})
	}

	return copies, warnings
}

// Test implements sync.Tester: it reads the library once.
func (p *Provider) Test(ctx context.Context, settings source.Settings) (sync.TestResult, error) {
	items, err := p.owned(ctx, settings)
	copies, _ := mapEntitlements(items)

	return sync.TestResult{Count: len(copies), Unit: "copies"}, err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	items, err := p.owned(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	copies, warnings := mapEntitlements(items)
	if len(copies) == 0 {
		warnings = append(warnings, "Amazon returned no games for this account (Prime Gaming codes for other stores are not listed here)")
	}

	return copies, warnings, nil
}
