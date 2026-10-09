// Package xbox implements the Xbox / Microsoft Store source provider. Microsoft has no public
// library API, so it signs in like the Xbox app does, the way Playnite and xbox-webapi work:
//
//  1. the user signs in with their Microsoft account through the login link; the browser ends on
//     an almost blank page whose address carries a one-time code, which the user pastes;
//  2. the code becomes a session (a refresh token that rotates on every use);
//  3. each scan turns it into Xbox Live tokens and reads the games the account owns from the
//     Microsoft Store collections (purchases and redeemed codes; Game Pass is left out), naming
//     them from the public Store catalog.
//
// It only reads: nothing is bought, installed or changed in the account.
package xbox

import (
	"bytes"
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
	"sort"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Xbox accounts.
const Type source.Type = "xbox"

// Platform is the store name used on copies, matching Humble's Microsoft / Xbox keys.
const Platform = "Microsoft Store / Xbox"

const (
	settingCode    = "login_code"
	settingSession = "session"

	// The Xbox app's public Microsoft account client.
	clientID    = "000000004C12AE6F"
	redirectURI = "https://login.live.com/oauth20_desktop.srf"
	scope       = "service::user.auth.xboxlive.com::MBI_SSL"

	defaultLiveURL        = "https://login.live.com"
	defaultUserAuthURL    = "https://user.auth.xboxlive.com/user/authenticate"
	defaultXSTSURL        = "https://xsts.auth.xboxlive.com/xsts/authorize"
	defaultCollectionsURL = "https://collections.mp.microsoft.com/v7.0/collections/query"
	defaultCatalogURL     = "https://displaycatalog.mp.microsoft.com/v7.0/products"

	pendingTTL  = 15 * time.Minute
	catalogPage = 20
	maxPages    = 50
)

// LoginURL is where the user signs in to get a code.
var LoginURL = defaultLiveURL + "/oauth20_authorize.srf?" + url.Values{
	"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {scope},
}.Encode()

var (
	// ErrNoSession means there is neither a stored session nor a code to start one.
	ErrNoSession = errors.New("xbox: paste the address of the page Microsoft opens after signing in (see the help below the field)")

	// ErrBadCode means Microsoft rejected the code.
	ErrBadCode = errors.New("microsoft rejected the code: codes work once and expire within minutes, open the login link again and paste the new address")

	// ErrSessionExpired means the stored session can no longer be refreshed.
	ErrSessionExpired = errors.New("the Microsoft session expired: open the login link and paste a new address")

	reCode = regexp.MustCompile(`[?&#]code=([^&#\s"]+)`)
)

type session struct {
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
}

type pending struct {
	s       session
	access  string
	expires time.Time
}

// Provider implements sync.Provider and sync.Preparer for Xbox accounts.
type Provider struct {
	LiveURL, UserAuthURL, XSTSURL, CollectionsURL, CatalogURL string
	Client                                                    *http.Client
	Market, Language                                          string
	Now                                                       func() time.Time

	mu      gosync.Mutex
	pending map[string]pending // sha256(code) → session from a recent exchange
}

// NewProvider returns the Xbox / Microsoft Store source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{
		LiveURL: defaultLiveURL, UserAuthURL: defaultUserAuthURL, XSTSURL: defaultXSTSURL,
		CollectionsURL: defaultCollectionsURL, CatalogURL: defaultCatalogURL,
		Client: &http.Client{Timeout: 30 * time.Second}, Market: "US", Language: "en-US", Now: time.Now,
	}
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{Key: "xbox", Name: "Xbox"}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Xbox / Microsoft Store",
		DescriptionKey: "sources.xbox.description",
		Fields: []source.Field{
			{Key: settingCode, LabelKey: "sources.xbox.code", HelpKey: "sources.xbox.codeHelp", HelpURL: LoginURL, Kind: source.FieldSecret},
			{Key: settingSession, Kind: source.FieldState},
		},
	}
}

// cleanCode accepts the whole address of the page Microsoft opens, or just the code.
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

// ---- Microsoft account ----

type liveToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	UserID       string `json:"user_id"`
}

func (p *Provider) live(ctx context.Context, form url.Values) (liveToken, error) {
	form.Set("client_id", clientID)
	form.Set("scope", scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.LiveURL+"/oauth20_token.srf", strings.NewReader(form.Encode()))
	if err != nil {
		return liveToken{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := p.Client.Do(req)
	if err != nil {
		return liveToken{}, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		if res.StatusCode < 500 && form.Get("grant_type") == "authorization_code" {
			return liveToken{}, ErrBadCode
		}

		if res.StatusCode < 500 {
			return liveToken{}, ErrSessionExpired
		}

		return liveToken{}, fmt.Errorf("microsoft sign-in: HTTP %d", res.StatusCode)
	}

	var tok liveToken
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" || tok.RefreshToken == "" {
		return liveToken{}, errors.New("microsoft sign-in: unexpected answer")
	}

	return tok, nil
}

// Prepare implements sync.Preparer: a pasted code is exchanged for a session, which is stored
// instead of the code (codes work once).
func (p *Provider) Prepare(ctx context.Context, settings source.Settings) (source.Settings, error) {
	out := source.Settings{}
	for k, v := range settings {
		out[k] = v
	}

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
	pend, ok := p.pending[key]
	p.mu.Unlock()

	if !ok || p.Now().After(pend.expires) {
		tok, err := p.live(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}})
		if err != nil {
			return nil, err
		}

		pend = pending{s: session{RefreshToken: tok.RefreshToken, UserID: tok.UserID}, access: tok.AccessToken, expires: p.Now().Add(pendingTTL)}
		p.mu.Lock()
		if p.pending == nil {
			p.pending = map[string]pending{}
		}

		p.pending[key] = pend
		p.mu.Unlock()
	}

	raw, _ := json.Marshal(pend.s)
	out[settingSession] = string(raw)

	return out, nil
}

// accessToken refreshes the session (the refresh token rotates: the new one is written back into
// settings for the caller to persist), reusing a fresh exchange when there is one.
func (p *Provider) accessToken(ctx context.Context, settings source.Settings) (string, error) {
	var s session
	if json.Unmarshal([]byte(settings[settingSession]), &s) != nil || s.RefreshToken == "" {
		return "", ErrNoSession
	}

	p.mu.Lock()
	for _, pend := range p.pending {
		if pend.s.RefreshToken == s.RefreshToken && p.Now().Before(pend.expires) {
			p.mu.Unlock()
			return pend.access, nil
		}
	}
	p.mu.Unlock()

	tok, err := p.live(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}})
	if err != nil {
		return "", err
	}

	raw, _ := json.Marshal(session{RefreshToken: tok.RefreshToken, UserID: s.UserID})
	settings[settingSession] = string(raw)

	return tok.AccessToken, nil
}

// ---- Xbox Live ----

type xblToken struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		Xui []struct {
			UHS string `json:"uhs"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func (p *Provider) postJSON(ctx context.Context, u string, body any, headers map[string]string, out any) (int, error) {
	raw, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := p.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, fmt.Errorf("HTTP %d %s", res.StatusCode, strings.TrimSpace(string(data[:min(len(data), 200)])))
	}

	return res.StatusCode, json.Unmarshal(data, out)
}

// storeAuth turns a Microsoft access token into the Xbox Live authorization of the Store
// ("XBL3.0 x=<user hash>;<token>").
func (p *Provider) storeAuth(ctx context.Context, access string) (string, error) {
	var user xblToken
	if code, err := p.postJSON(ctx, p.UserAuthURL, map[string]any{
		"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
		"Properties": map[string]any{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "t=" + access},
	}, map[string]string{"x-xbl-contract-version": "1"}, &user); err != nil {
		if code == http.StatusUnauthorized {
			return "", ErrSessionExpired
		}

		return "", fmt.Errorf("xbox live sign-in: %w", err)
	}

	var xsts xblToken
	if code, err := p.postJSON(ctx, p.XSTSURL, map[string]any{
		"RelyingParty": "http://mp.microsoft.com/", "TokenType": "JWT",
		"Properties": map[string]any{"UserTokens": []string{user.Token}, "SandboxId": "RETAIL"},
	}, map[string]string{"x-xbl-contract-version": "1"}, &xsts); err != nil {
		if code == http.StatusUnauthorized {
			return "", errors.New("xbox live refused this account (it may need an Xbox profile: sign in once on xbox.com)")
		}

		return "", fmt.Errorf("xbox live authorization: %w", err)
	}

	if xsts.Token == "" || len(xsts.DisplayClaims.Xui) == 0 {
		return "", errors.New("xbox live authorization: unexpected answer")
	}

	return "XBL3.0 x=" + xsts.DisplayClaims.Xui[0].UHS + ";" + xsts.Token, nil
}

// ---- Store ----

// entitlement is one item of the Store collections; fields Microsoft omits stay empty and do not
// filter anything out.
type entitlement struct {
	ProductID       string `json:"productId"`
	ProductKind     string `json:"productKind"`
	Status          string `json:"status"`
	AcquisitionType string `json:"acquisitionType"`
	SkuType         string `json:"skuType"`
	IsTrial         bool   `json:"isTrial"`
}

// owned reports whether an entitlement is a game the account keeps: not a Game Pass
// ("recurring") entitlement, not a trial, not expired or revoked.
func (e entitlement) owned() bool {
	if e.ProductID == "" || e.IsTrial || strings.EqualFold(e.SkuType, "Trial") {
		return false
	}

	if e.ProductKind != "" && !strings.EqualFold(e.ProductKind, "Game") {
		return false
	}

	if e.Status != "" && !strings.EqualFold(e.Status, "Active") {
		return false
	}

	return !strings.EqualFold(e.AcquisitionType, "Recurring")
}

func (p *Provider) entitlements(ctx context.Context, auth string) ([]entitlement, error) {
	var all []entitlement

	token := ""

	for range maxPages {
		// beneficiaries is required; empty means the signed-in user (the XBL3.0 token).
		body := map[string]any{
			"beneficiaries": []any{}, "maxPageSize": 1000, "market": p.Market, "languages": []string{p.Language},
			"validityType": "Valid", "excludeDuplicates": true, "productSkuIds": []string{}, "entitlementFilters": []string{},
		}
		if token != "" {
			body["continuationToken"] = token
		}

		var out struct {
			Items             []entitlement `json:"items"`
			ContinuationToken string        `json:"continuationToken"`
		}
		if code, err := p.postJSON(ctx, p.CollectionsURL, body, map[string]string{"Authorization": auth}, &out); err != nil {
			if code == http.StatusUnauthorized || strings.Contains(err.Error(), "InvalidXToken") {
				return nil, ErrSessionExpired
			}

			return nil, fmt.Errorf("microsoft store collections: %w", err)
		}

		all = append(all, out.Items...)
		if out.ContinuationToken == "" || out.ContinuationToken == token {
			break
		}

		token = out.ContinuationToken
	}

	return all, nil
}

// catalogProduct is what the public Store catalog says about a product.
type catalogProduct struct {
	ProductID           string `json:"ProductId"`
	ProductKind         string `json:"ProductKind"`
	LocalizedProperties []struct {
		ProductTitle string `json:"ProductTitle"`
		Images       []struct {
			ImagePurpose string `json:"ImagePurpose"`
			URI          string `json:"Uri"`
			Width        int    `json:"Width"`
			Height       int    `json:"Height"`
		} `json:"Images"`
	} `json:"LocalizedProperties"`
}

// catalog reads products from the public Store catalog, a page at a time.
func catalog(ctx context.Context, client *http.Client, base, market, language string, ids []string) (map[string]catalogProduct, error) {
	out := map[string]catalogProduct{}

	for i := 0; i < len(ids); i += catalogPage {
		q := url.Values{"bigIds": {strings.Join(ids[i:min(i+catalogPage, len(ids))], ",")}, "market": {market}, "languages": {language}}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}

		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		var page struct {
			Products []catalogProduct `json:"Products"`
		}

		err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&page)
		res.Body.Close()

		if res.StatusCode != http.StatusOK || err != nil {
			return nil, fmt.Errorf("microsoft store catalog: HTTP %d", res.StatusCode)
		}

		for _, pr := range page.Products {
			out[pr.ProductID] = pr
		}
	}

	return out, nil
}

func (p *Provider) owned(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, error) {
	access, err := p.accessToken(ctx, settings)
	if err != nil {
		return nil, err
	}

	auth, err := p.storeAuth(ctx, access)
	if err != nil {
		return nil, err
	}

	ents, err := p.entitlements(ctx, auth)
	if err != nil {
		return nil, err
	}

	var ids []string

	seen := map[string]bool{}
	for _, e := range ents {
		if e.owned() && !seen[e.ProductID] {
			seen[e.ProductID] = true
			ids = append(ids, e.ProductID)
		}
	}

	products, err := catalog(ctx, p.Client, p.CatalogURL, p.Market, p.Language, ids)
	if err != nil {
		return nil, err
	}

	var out []game.ImportedCopy

	for _, id := range ids {
		pr, ok := products[id]
		if !ok || len(pr.LocalizedProperties) == 0 || (pr.ProductKind != "" && !strings.EqualFold(pr.ProductKind, "Game")) {
			continue
		}

		title := strings.TrimSpace(pr.LocalizedProperties[0].ProductTitle)
		if title == "" {
			continue
		}

		out = append(out, game.ImportedCopy{
			ExternalID: "xbox:" + id,
			Links:      game.Links{LinkedStore.Key: id},
			Title:      title,
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: Platform, Status: game.StatusOwned, Origin: "Microsoft Store"},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out, nil
}

// Test implements sync.Provider.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, err := p.owned(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	copies, err := p.owned(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	var warnings []string
	if len(copies) == 0 {
		warnings = append(warnings, "the Microsoft Store returned no games bought with this account (Game Pass games are not imported)")
	}

	return copies, warnings, nil
}
