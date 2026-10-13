// Package playstation implements the PlayStation Network source provider. Sony has no public
// library API, so it signs in like the PlayStation app does, the way psn-api and PSNAWP work:
//
//  1. the user signs in on playstation.com, then opens Sony's "ssocookie" page and pastes the
//     NPSSO token it shows;
//  2. the NPSSO gives an authorization code, exchanged for a session (a refresh token);
//  3. scans read the games bought with the account (PS4 and PS5, no PS Plus games) from the
//     GraphQL API of PlayStation's game library website, and leave out apps, demos, soundtracks
//     and videos (see nongames.go).
//
// It only reads: nothing is bought, downloaded or changed in the account.
package playstation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of PlayStation accounts.
const Type source.Type = "playstation"

const (
	settingNPSSO   = "npsso"
	settingSession = "session"

	// The PlayStation app's public client credentials, the same ones psn-api and PSNAWP use.
	clientID     = "09515159-7237-4370-9b40-3806e67c0891"
	clientSecret = "ucPjka5tntB2KqsP"
	redirectURI  = "com.scee.psxandroid.scecompcall://redirect"
	scope        = "psn:mobile.v2.core psn:clientapp"

	defaultAuthURL    = "https://ca.account.sony.com/api/authz/v3/oauth"
	defaultLibraryURL = "https://web.np.playstation.com/api/graphql/v1/op"

	pageSize = 24
	maxPages = 100
)

// NPSSOURL shows the NPSSO token of the signed-in browser.
const NPSSOURL = "https://ca.account.sony.com/api/v1/ssocookie"

// purchasedQueryHashes are the registered ("persisted") versions of the library website's
// purchased-games query, newest first: Sony only runs queries it knows by hash.
var purchasedQueryHashes = []string{
	"827a423f6a8ddca4107ac01395af2ec0eafd8396fc7fa204aaf9b7ed2eefa168",
	"2c045408b0a4d0264bb5a3edfed4efd49fb4749cf8d216be9043768adff905e2",
}

var (
	// ErrNoNPSSO means nothing usable was pasted.
	ErrNoNPSSO = errors.New("playstation: paste the NPSSO token (sign in on playstation.com, then open the page in the help)")

	// ErrSignedOut means Sony no longer accepts the NPSSO token.
	ErrSignedOut = source.SignedOut("playstation did not accept the NPSSO token: sign in again on playstation.com and paste a new one")

	reNPSSO = regexp.MustCompile(`"npsso"\s*:\s*"([^"]+)"`)
	reCode  = regexp.MustCompile(`[?&]code=([^&#\s]+)`)
)

func cleanNPSSO(v string) string {
	v = strings.TrimSpace(v)
	if m := reNPSSO.FindStringSubmatch(v); m != nil {
		return m[1]
	}

	if strings.Contains(v, "npsso=") {
		if s, ok := browsersession.Parse(v)["npsso"]; ok {
			return strings.Trim(s, `"' `)
		}
	}

	return strings.Trim(v, `"' `)
}

type session struct {
	RefreshToken string `json:"refresh_token"`
}

type accessToken struct {
	token   string
	expires time.Time
}

// Provider implements sync.Provider for PlayStation accounts.
type Provider struct {
	AuthURL    string
	LibraryURL string

	// GameListURL is the PlayStation app's played-titles list, read to tell games from apps.
	GameListURL string

	Client *http.Client
	Now    func() time.Time

	// Log receives the titles left out as non-games. Optional.
	Log *slog.Logger

	mu     gosync.Mutex
	tokens map[string]accessToken // refresh token → access token
}

// NewProvider returns the PlayStation source with its production endpoints.
func NewProvider(log *slog.Logger) *Provider {
	return &Provider{
		AuthURL:     defaultAuthURL,
		LibraryURL:  defaultLibraryURL,
		GameListURL: defaultGameListURL,
		Now:         time.Now,
		Log:         log,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			// The authorization step answers with a redirect to the app's custom scheme: read it, do not follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports.
var LinkedStore = game.Store{
	Key:  "psn",
	Name: "PlayStation",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "PlayStation",
		DescriptionKey: "sources.playstation.description",
		Fields: []source.Field{
			{
				Key:      settingNPSSO,
				LabelKey: "sources.playstation.npsso",
				HelpKey:  "sources.playstation.npssoHelp",
				HelpURL:  NPSSOURL,
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    "https://www.playstation.com/",
					Hosts:   []string{"ca.account.sony.com"},
					Capture: schema.Capture{
						Fetch: &schema.FetchCapture{
							URL:   NPSSOURL,
							Field: "npsso",
						},
					},
				},
				Kind:     source.FieldSecret,
				Required: true,
			},
			{
				Key:  settingSession,
				Kind: source.FieldState,
			},
		},
	}
}

// ---- Sign-in ----

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// authorize turns the NPSSO token into a one-time authorization code.
func (p *Provider) authorize(ctx context.Context, npsso string) (string, error) {
	q := url.Values{"access_type": {"offline"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {scope}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.AuthURL+"/authorize?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Cookie", "npsso="+npsso)
	req.Header.Set("User-Agent", apiclient.BrowserUserAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return "", apiclient.ClassifyError(ctx, err)
	}

	res.Body.Close()

	// A 429 or an outage is a passing trouble, not a refused NPSSO: it says nothing about it.
	if apiclient.Transient(res.StatusCode) {
		return "", fmt.Errorf("playstation sign-in: %w", apiclient.ClassifyStatus(res.StatusCode))
	}

	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, redirectURI) {
		return "", ErrSignedOut // sent to the sign-in page: the NPSSO is no longer valid
	}

	m := reCode.FindStringSubmatch(loc)
	if m == nil {
		return "", ErrSignedOut
	}

	return url.QueryUnescape(m[1])
}

func (p *Provider) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("token_format", "jwt")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.AuthURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}

	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", apiclient.BrowserUserAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return tokenResponse{}, apiclient.ClassifyError(ctx, err)
	}
	defer res.Body.Close()

	// Sony refuses a stale refresh token or code with a 400 invalid_grant; a 429 or an outage is a
	// passing trouble, after which the same session still works.
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		if apiclient.Rejected(res.StatusCode) {
			return tokenResponse{}, ErrSignedOut
		}

		return tokenResponse{}, fmt.Errorf("playstation sign-in: %w", apiclient.ClassifyStatus(res.StatusCode))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return tokenResponse{}, errors.New("playstation sign-in: unexpected answer")
	}

	return tok, nil
}

// accessToken returns a valid access token: cached, from the stored refresh token, or by signing
// in again with the NPSSO when the refresh token no longer works. A new refresh token is written
// back into settings for the caller to persist.
func (p *Provider) accessToken(ctx context.Context, settings source.Settings) (string, error) {
	var s session

	_ = json.Unmarshal([]byte(settings[settingSession]), &s)
	if s.RefreshToken != "" {
		p.mu.Lock()
		cached, ok := p.tokens[s.RefreshToken]
		p.mu.Unlock()

		if ok && p.Now().Add(time.Minute).Before(cached.expires) {
			return cached.token, nil
		}

		tok, err := p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}, "scope": {scope}})
		if err == nil {
			return p.remember(settings, tok, s.RefreshToken), nil
		}

		if !errors.Is(err, ErrSignedOut) {
			return "", err
		}
	}

	npsso := cleanNPSSO(settings[settingNPSSO])
	if npsso == "" {
		return "", ErrNoNPSSO
	}

	code, err := p.authorize(ctx, npsso)
	if err != nil {
		return "", err
	}

	tok, err := p.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}})
	if err != nil {
		return "", err
	}

	return p.remember(settings, tok, ""), nil
}

func (p *Provider) remember(settings source.Settings, tok tokenResponse, previous string) string {
	refresh := tok.RefreshToken
	if refresh == "" {
		refresh = previous
	}

	if refresh != "" {
		raw, _ := json.Marshal(session{RefreshToken: refresh})
		settings[settingSession] = string(raw)

		p.mu.Lock()
		if p.tokens == nil {
			p.tokens = map[string]accessToken{}
		}

		p.tokens[refresh] = accessToken{tok.AccessToken, p.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
		p.mu.Unlock()
	}

	return tok.AccessToken
}

// ---- Library ----

type title struct {
	Name          string `json:"name"`
	Platform      string `json:"platform"`
	ProductID     string `json:"productId"`
	EntitlementID string `json:"entitlementId"`
	TitleID       string `json:"titleId"`
	ConceptID     string `json:"conceptId"`
	Membership    string `json:"membership"`
	IsActive      *bool  `json:"isActive"`
	Image         *struct {
		URL string `json:"url"`
	} `json:"image"`
}

func (p *Provider) page(ctx context.Context, token, hash string, start int) ([]title, bool, error) {
	vars, _ := json.Marshal(map[string]any{
		"isActive": true, "platform": []string{"ps4", "ps5"}, "size": pageSize, "start": start,
		"sortBy": "ACTIVE_DATE", "sortDirection": "desc", "subscriptionService": "NONE",
	})
	ext, _ := json.Marshal(map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": hash}})
	q := url.Values{"operationName": {"getPurchasedGameList"}, "variables": {string(vars)}, "extensions": {string(ext)}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.LibraryURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, false, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json") // Sony's GraphQL refuses requests that could be CSRF
	req.Header.Set("User-Agent", apiclient.BrowserUserAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("playstation library: %w", apiclient.ClassifyError(ctx, err))
	}
	defer res.Body.Close()

	// A 401 refuses the token whatever the body says (a web page as well as a GraphQL error), and
	// a rate limit or an outage is a passing trouble: neither is worth decoding.
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		p.forget(token) // the next accessToken renews it
		return nil, false, errTokenRejected
	case apiclient.Transient(res.StatusCode):
		return nil, false, fmt.Errorf("playstation library: %w", apiclient.ClassifyStatus(res.StatusCode))
	}

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))

	var out struct {
		Message string `json:"message"`
		Data    struct {
			Purchased *struct {
				Games    []title `json:"games"`
				PageInfo struct {
					IsLast     bool `json:"isLast"`
					TotalCount int  `json:"totalCount"`
				} `json:"pageInfo"`
			} `json:"purchasedTitlesRetrieve"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("playstation library: HTTP %d, unexpected answer", res.StatusCode)
	}

	if strings.Contains(out.Message, "not whitelisted") {
		return nil, false, errQueryRetired
	}

	if len(out.Errors) > 0 {
		if strings.Contains(out.Errors[0].Message, "authorized") {
			p.forget(token) // the next accessToken renews it
			return nil, false, errTokenRejected
		}

		return nil, false, fmt.Errorf("playstation library: %s", out.Errors[0].Message)
	}

	if out.Data.Purchased == nil {
		return nil, false, fmt.Errorf("playstation library: HTTP %d, no library in the answer", res.StatusCode)
	}

	pg := out.Data.Purchased
	last := pg.PageInfo.IsLast || len(pg.Games) < pageSize || start+len(pg.Games) >= pg.PageInfo.TotalCount && pg.PageInfo.TotalCount > 0

	return pg.Games, last, nil
}

var errQueryRetired = errors.New("playstation library: the library query is no longer registered by Sony")

// errTokenRejected means the library refused an access token: it may have expired or been revoked
// before its time. The caller renews it once (withToken); only a refused renewal is signed out.
var errTokenRejected = errors.New("playstation rejected the access token")

// forget drops a cached access token the library refused, so the next accessToken renews it.
func (p *Provider) forget(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for k, v := range p.tokens {
		if v.token == token {
			delete(p.tokens, k)
		}
	}
}

// withToken runs call with an access token, and once more with a renewed one when the library
// refuses the first (errTokenRejected): only a refused renewal means the user must sign in again.
func (p *Provider) withToken(ctx context.Context, settings source.Settings, call func(token string) error) error {
	for range 2 {
		token, err := p.accessToken(ctx, settings)
		if err != nil {
			return err
		}

		if err := call(token); !errors.Is(err, errTokenRejected) {
			return err
		}
	}

	return ErrSignedOut
}

func (p *Provider) purchased(ctx context.Context, token string) ([]title, error) {
	var lastErr error

	for _, hash := range purchasedQueryHashes {
		var (
			all []title
			err error
		)

		for page := 0; page < maxPages; page++ {
			var (
				batch []title
				last  bool
			)

			batch, last, err = p.page(ctx, token, hash, len(all))
			if err != nil {
				break
			}

			all = append(all, batch...)

			if last {
				break
			}
		}

		if err == nil {
			return all, nil
		}

		if !errors.Is(err, errQueryRetired) {
			return nil, err
		}

		lastErr = err
	}

	return nil, lastErr
}

// platformName maps Sony's platform code to Game Vault's.
func platformName(p string) string {
	switch strings.ToUpper(p) {
	case "PS5":
		return "PS5"
	case "PS4":
		return "PS4"
	case "PS3":
		return "PS3"
	case "PSVITA", "PS VITA":
		return "PS Vita"
	}

	return "PlayStation Store"
}

// mapTitles turns purchased titles into copies, one per game and platform; PS Plus games and
// inactive entitlements are left out. Titles that are not games (named so, or found in nonGames)
// are reported as withdrawn, so a copy imported before they were told apart is removed, and their
// names are listed in skipped.
func mapTitles(titles []title, nonGames *nonGameIndex) (out []game.ImportedCopy, skipped []string) {
	seen := map[string]bool{}

	for _, t := range titles {
		name := strings.TrimSpace(t.Name)
		if name == "" || (t.IsActive != nil && !*t.IsActive) {
			continue
		}

		if m := strings.ToUpper(t.Membership); m != "" && m != "NONE" {
			continue // PS Plus (or another subscription)
		}

		id := t.EntitlementID
		if id == "" {
			id = t.ProductID
		}

		if id == "" {
			id = t.TitleID
		}

		if id == "" || seen[id] {
			continue
		}

		seen[id] = true

		if nonGameName(name) || (nonGames != nil && nonGames.has(t)) {
			out = append(out, game.ImportedCopy{
				ExternalID: "psn:" + id,
				Title:      name,
				Withdrawn:  true,
			})
			skipped = append(skipped, name)

			continue
		}

		platform := platformName(t.Platform)

		// The system is set on purpose although it equals the platform's system: Sony says which
		// console the title was bought for, and the copy keeps it as the source's system.
		system := platform
		if platform == "PlayStation Store" {
			system = "" // Sony did not say which console: the platform's system applies
		}

		out = append(out, game.ImportedCopy{
			ExternalID: "psn:" + id,
			Links:      game.Links{LinkedStore.Key: id},
			Title:      name,
			System:     system,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: platform,
				Status:   game.StatusOwned,
				Origin:   "PlayStation Store",
			},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out, skipped
}

// Test implements sync.Provider: it signs in and reads the purchases, without the played titles.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	return p.withToken(ctx, settings, func(token string) error {
		_, err := p.purchased(ctx, token)

		return err
	})
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	var (
		titles []title
		token  string
	)

	err := p.withToken(ctx, settings, func(tok string) error {
		var err error

		token = tok
		titles, err = p.purchased(ctx, tok)

		return err
	})
	if err != nil {
		return nil, nil, err
	}

	copies, skipped := mapTitles(titles, p.playedNonGames(ctx, token))
	if len(skipped) > 0 && p.Log != nil {
		p.Log.Info("playstation: skipped titles that are not games", "count", len(skipped), "titles", strings.Join(skipped, "; "))
	}

	var warnings []string
	if len(copies) == len(skipped) {
		warnings = append(warnings, "PlayStation returned no games bought with this account (PS Plus games are not imported)")
	}

	return copies, warnings, nil
}
