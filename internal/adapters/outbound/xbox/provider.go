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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthsession"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
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
	defaultUserAuthURL    = "https://user.auth.xboxlive.com"
	defaultXSTSURL        = "https://xsts.auth.xboxlive.com"
	defaultCollectionsURL = "https://collections.mp.microsoft.com"
	defaultCatalogURL     = "https://displaycatalog.mp.microsoft.com"

	catalogPage = 20
	maxPages    = 50
)

// LoginURL is where the user signs in to get a code.
var LoginURL = defaultLiveURL + "/oauth20_authorize.srf?" + url.Values{
	"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {scope},
}.Encode()

var (
	// ErrNoCredentials means there is neither a stored session nor a code to start one.
	ErrNoCredentials = errkind.New(errkind.Invalid, "xbox: paste the address of the page Microsoft opens after signing in (see the help below the field)")

	// ErrBadCode means Microsoft rejected the code.
	ErrBadCode = errkind.New(errkind.Invalid, "microsoft rejected the code: codes work once and expire within minutes, open the login link again and paste the new address")

	// ErrSignedOut means the stored session can no longer be refreshed.
	ErrSignedOut = source.SignedOut("the Microsoft session expired: open the login link and paste a new address")

	reCode = regexp.MustCompile(`[?&#]code=([^&#\s"]+)`)
)

type session struct {
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
}

// Provider implements sync.Provider and sync.Preparer for Xbox accounts.
type Provider struct {
	// Live calls the Microsoft account service (codes and tokens); tests point it at a fake server.
	Live *apiclient.Client

	// UserAuth calls Xbox Live's user authentication.
	UserAuth *apiclient.Client

	// XSTS calls Xbox Live's security token service.
	XSTS *apiclient.Client

	// Collections calls the Microsoft Store collections (what the account owns).
	Collections *apiclient.Client

	// Catalog calls the public Microsoft Store catalog.
	Catalog *apiclient.Client

	// Market is the Store market asked for (a country code).
	Market string

	// Language is the language of the titles asked for.
	Language string

	// Now is the clock.
	Now func() time.Time

	// sessions exchanges the pasted codes and keeps the sessions' access tokens.
	sessions *oauthsession.Sessions[session]
}

// NewProvider returns the Xbox / Microsoft Store source with its production endpoints.
func NewProvider() *Provider {
	p := &Provider{
		Live:        newClient(defaultLiveURL, 30*time.Second),
		UserAuth:    newClient(defaultUserAuthURL, 30*time.Second),
		XSTS:        newClient(defaultXSTSURL, 30*time.Second),
		Collections: newClient(defaultCollectionsURL, 30*time.Second),
		Catalog:     newClient(defaultCatalogURL, 30*time.Second),
		Market:      "US",
		Language:    "en-US",
		Now:         time.Now,
	}
	p.sessions = &oauthsession.Sessions[session]{
		CodeField:        settingCode,
		StateField:       settingSession,
		Clean:            func(v string) (string, error) { return cleanCode(v), nil },
		Exchange:         p.exchange,
		Refresh:          p.refresh,
		RefreshToken:     func(s session) string { return s.RefreshToken },
		ErrNoCredentials: ErrNoCredentials,
		ErrSignedOut:     ErrSignedOut,
		Now:              func() time.Time { return p.Now() },
	}

	return p
}

// newClient returns a client of one of Microsoft's services.
func newClient(baseURL string, timeout time.Duration) *apiclient.Client {
	c := apiclient.New(baseURL)
	c.HTTP.Timeout = timeout

	return c
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{
	Key:  "xbox",
	Name: "Xbox",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Xbox / Microsoft Store",
		DescriptionKey: "sources.xbox.description",
		Fields: []schema.Field{
			{
				Key:      settingCode,
				LabelKey: "sources.xbox.code",
				HelpKey:  "sources.xbox.codeHelp",
				HelpURL:  LoginURL,
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    LoginURL,
					Capture: schema.Capture{
						Redirect: &schema.RedirectCapture{
							Prefix: redirectURI,
							Param:  "code",
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

// Prepare implements sync.Preparer: a pasted code is exchanged for a session, which is stored
// instead of the code (codes work once).
func (p *Provider) Prepare(ctx context.Context, settings schema.Settings) (schema.Settings, error) {
	return p.sessions.Prepare(ctx, settings)
}

// ---- Microsoft account ----

type liveToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	UserID       string `json:"user_id"`
}

// exchange trades a code for a session (oauthsession.Sessions.Exchange).
func (p *Provider) exchange(ctx context.Context, code string) (oauthsession.Grant[session], error) {
	tok, err := p.live(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}})
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	return grant(tok, tok.UserID), nil
}

// refresh renews a session (oauthsession.Sessions.Refresh). Microsoft rotates the refresh token.
func (p *Provider) refresh(ctx context.Context, s session) (oauthsession.Grant[session], error) {
	tok, err := p.live(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}})
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	return grant(tok, s.UserID), nil
}

// grant is the session and access token of a token answer, for the account userID.
func grant(tok liveToken, userID string) oauthsession.Grant[session] {
	return oauthsession.Grant[session]{
		Session: session{
			RefreshToken: tok.RefreshToken,
			UserID:       userID,
		},
		AccessToken: tok.AccessToken,
		Lifetime:    time.Duration(tok.ExpiresIn) * time.Second,
	}
}

// live calls the Microsoft account token endpoint.
func (p *Provider) live(ctx context.Context, form url.Values) (liveToken, error) {
	form.Set("client_id", clientID)
	form.Set("scope", scope)

	res, err := p.Live.Send(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/oauth20_token.srf",
		Form:   form,
	})
	if err != nil {
		return liveToken{}, apiclient.ClassifyError(ctx, err)
	}

	// login.live.com refuses a used code or a stale refresh token with a 400 invalid_grant; a 429
	// or an outage is a passing trouble, after which the same code or session still works.
	if !res.OK() {
		if apiclient.Rejected(res.Status) && form.Get("grant_type") == "authorization_code" {
			return liveToken{}, ErrBadCode
		}

		if apiclient.Rejected(res.Status) {
			return liveToken{}, ErrSignedOut
		}

		return liveToken{}, fmt.Errorf("microsoft sign-in: %w", apiclient.ClassifyStatus(res.Status))
	}

	var tok liveToken
	if err := res.Decode(&tok); err != nil || tok.AccessToken == "" || tok.RefreshToken == "" {
		return liveToken{}, errors.New("microsoft sign-in: unexpected answer")
	}

	return tok, nil
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

// errTokenRejected means Xbox Live or the Store refused the Microsoft access token or the Xbox Live
// token made from it (HTTP 401): it may have expired or been revoked before its time. The sessions
// renew it once; only a refused refresh is signed out.
var errTokenRejected = fmt.Errorf("xbox live rejected the access token: %w", apiclient.ErrTokenRejected)

// xblContract is the header of Xbox Live's authentication API version.
var xblContract = http.Header{"x-xbl-contract-version": {"1"}}

// storeAuth turns a Microsoft access token into the Xbox Live authorization of the Store
// ("XBL3.0 x=<user hash>;<token>").
func (p *Provider) storeAuth(ctx context.Context, access string) (string, error) {
	var user xblToken

	err := p.UserAuth.Do(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/user/authenticate",
		Header: xblContract,
		Body: map[string]any{
			"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
			"Properties": map[string]any{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "t=" + access},
		},
	}, &user)
	if apiclient.IsStatus(err, http.StatusUnauthorized) {
		return "", errTokenRejected
	}

	if err != nil {
		return "", fmt.Errorf("xbox live sign-in: %w", apiclient.ClassifyError(ctx, err))
	}

	var xsts xblToken

	err = p.XSTS.Do(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/xsts/authorize",
		Header: xblContract,
		Body: map[string]any{
			"RelyingParty": "http://mp.microsoft.com/", "TokenType": "JWT",
			"Properties": map[string]any{"UserTokens": []string{user.Token}, "SandboxId": "RETAIL"},
		},
	}, &xsts)
	if apiclient.IsStatus(err, http.StatusUnauthorized) {
		return "", errors.New("xbox live refused this account (it may need an Xbox profile: sign in once on xbox.com)")
	}

	if err != nil {
		return "", fmt.Errorf("xbox live authorization: %w", apiclient.ClassifyError(ctx, err))
	}

	if xsts.Token == "" || len(xsts.DisplayClaims.Xui) == 0 {
		return "", errors.New("xbox live authorization: unexpected answer")
	}

	return "XBL3.0 x=" + xsts.DisplayClaims.Xui[0].UHS + ";" + xsts.Token, nil
}

// rejectedXToken reports whether the Store refused the Xbox Live token: a 401, or any error whose
// answer names an InvalidXToken.
func rejectedXToken(err error) bool {
	var se *apiclient.StatusError

	return errors.As(err, &se) && (se.Status == http.StatusUnauthorized || strings.Contains(se.Body, "InvalidXToken"))
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

		err := p.Collections.Do(ctx, apiclient.Request{
			Method: http.MethodPost,
			Path:   "/v7.0/collections/query",
			Body:   body,
			Header: http.Header{"Authorization": {auth}},
		}, &out)
		if rejectedXToken(err) {
			return nil, errTokenRejected
		}

		if err != nil {
			return nil, fmt.Errorf("microsoft store collections: %w", apiclient.ClassifyError(ctx, err))
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

// catalog reads products from the public Store catalog, a page at a time. A 429 or an outage says
// nothing about the products: callers ask again later.
func catalog(ctx context.Context, api *apiclient.Client, market, language string, ids []string) (map[string]catalogProduct, error) {
	out := map[string]catalogProduct{}

	for i := 0; i < len(ids); i += catalogPage {
		q := url.Values{"bigIds": {strings.Join(ids[i:min(i+catalogPage, len(ids))], ",")}, "market": {market}, "languages": {language}}

		var page struct {
			Products []catalogProduct `json:"Products"`
		}
		if err := api.Get(ctx, "/v7.0/products", q, &page); err != nil {
			return nil, fmt.Errorf("microsoft store catalog: %w", apiclient.ClassifyError(ctx, err))
		}

		for _, pr := range page.Products {
			out[pr.ProductID] = pr
		}
	}

	return out, nil
}

// owned reads the games the account owns. A rejected access token is renewed once, and the Xbox
// Live tokens made again from the new one.
func (p *Provider) owned(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, error) {
	var ents []entitlement

	err := p.sessions.WithRetry(ctx, settings, func(access string) error {
		auth, err := p.storeAuth(ctx, access)
		if err != nil {
			return err
		}

		ents, err = p.entitlements(ctx, auth)

		return err
	})
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

	products, err := catalog(ctx, p.Catalog, p.Market, p.Language, ids)
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
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   "Microsoft Store",
			},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out, nil
}

// Test implements sync.Provider.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.owned(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
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
