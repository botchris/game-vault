// Package playstation implements the PlayStation Network source provider. Sony has no public
// library API, so it signs in like the PlayStation app does, the way psn-api and PSNAWP work:
//
//  1. the user signs in on playstation.com, then opens Sony's "ssocookie" page and pastes the
//     NPSSO token it shows;
//  2. the NPSSO gives an authorization code, exchanged for a session (a refresh token);
//  3. scans read the games bought with the account (PS4 and PS5, no PS Plus games) from the
//     GraphQL API of PlayStation's game library website.
//
// It only reads: nothing is bought, downloaded or changed in the account.
package playstation

import (
	"context"
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
	userAgent         = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Safari/537.36"

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
	ErrSignedOut = errors.New("playstation did not accept the NPSSO token: sign in again on playstation.com and paste a new one")

	reNPSSO = regexp.MustCompile(`"npsso"\s*:\s*"([^"]+)"`)
	reCode  = regexp.MustCompile(`[?&]code=([^&#\s]+)`)
)

func cleanNPSSO(v string) string {
	v = strings.TrimSpace(v)
	if m := reNPSSO.FindStringSubmatch(v); m != nil {
		return m[1]
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
	AuthURL, LibraryURL string
	Client              *http.Client
	Now                 func() time.Time

	mu     gosync.Mutex
	tokens map[string]accessToken // refresh token → access token
}

// NewProvider returns the PlayStation source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{
		AuthURL: defaultAuthURL, LibraryURL: defaultLibraryURL, Now: time.Now,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			// The authorization step answers with a redirect to the app's custom scheme: read it, do not follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "PlayStation",
		DescriptionKey: "sources.playstation.description",
		Fields: []source.Field{
			{Key: settingNPSSO, LabelKey: "sources.playstation.npsso", HelpKey: "sources.playstation.npssoHelp",
				HelpURL: NPSSOURL, Kind: source.FieldSecret, Required: true},
			{Key: settingSession, Kind: source.FieldState},
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
	req.Header.Set("User-Agent", userAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return "", err
	}

	res.Body.Close()

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
	req.Header.Set("User-Agent", userAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		if res.StatusCode < 500 {
			return tokenResponse{}, ErrSignedOut
		}

		return tokenResponse{}, fmt.Errorf("playstation sign-in: HTTP %d", res.StatusCode)
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
	req.Header.Set("User-Agent", userAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

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
		if strings.Contains(out.Errors[0].Message, "authorized") || res.StatusCode == http.StatusUnauthorized {
			return nil, false, ErrSignedOut
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

func (p *Provider) purchased(ctx context.Context, settings source.Settings) ([]title, error) {
	token, err := p.accessToken(ctx, settings)
	if err != nil {
		return nil, err
	}

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
// inactive entitlements are left out.
func mapTitles(titles []title) []game.ImportedCopy {
	seen := map[string]bool{}

	var out []game.ImportedCopy

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
		out = append(out, game.ImportedCopy{
			ExternalID: "psn:" + id,
			Title:      name,
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: platformName(t.Platform), Status: game.StatusOwned, Origin: "PlayStation Store"},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out
}

// Test implements sync.Provider.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, _, err := p.Fetch(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	titles, err := p.purchased(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	copies := mapTitles(titles)

	var warnings []string
	if len(copies) == 0 {
		warnings = append(warnings, "PlayStation returned no games bought with this account (PS Plus games are not imported)")
	}

	return copies, warnings, nil
}
