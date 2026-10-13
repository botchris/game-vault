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
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/adapters/outbound/oauthsession"
	"gamevault/internal/domain/errkind"
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
	// ErrNoCredentials means nothing usable was pasted.
	ErrNoCredentials = errkind.New(errkind.Invalid, "playstation: paste the NPSSO token (sign in on playstation.com, then open the page in the help)")

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

// session is what the source stores between scans.
type session struct {
	RefreshToken string `json:"refresh_token"`
}

// Provider implements sync.Provider for PlayStation accounts.
type Provider struct {
	// Auth calls Sony's OAuth endpoints (authorize, token); tests point it at a fake server.
	Auth *apiclient.Client

	// Library calls the library website's GraphQL endpoint (its BaseURL is the endpoint itself).
	Library *apiclient.Client

	// GameList calls the PlayStation app's played-titles list (its BaseURL is the endpoint itself),
	// read to tell games from apps.
	GameList *apiclient.Client

	// Now is the clock.
	Now func() time.Time

	// Log receives the titles left out as non-games. Optional.
	Log *slog.Logger

	// sessions exchanges the NPSSO for a session and keeps the sessions' access tokens. The NPSSO
	// is not a one-time code: it stays in the settings, so the source does not implement
	// sync.Preparer (which would drop it) and signs in with it only when it has no usable session.
	sessions *oauthsession.Sessions[session]
}

// NewProvider returns the PlayStation source with its production endpoints.
func NewProvider(log *slog.Logger) *Provider {
	auth := newClient(defaultAuthURL)
	// The authorization step answers with a redirect to the app's custom scheme: read it, do not
	// follow it.
	auth.NoRedirects = true

	p := &Provider{
		Auth:     auth,
		Library:  newClient(defaultLibraryURL),
		GameList: newClient(defaultGameListURL),
		Now:      time.Now,
		Log:      log,
	}
	p.sessions = &oauthsession.Sessions[session]{
		CodeField:        settingNPSSO,
		StateField:       settingSession,
		Clean:            func(v string) (string, error) { return cleanNPSSO(v), nil },
		Exchange:         p.exchange,
		Refresh:          p.refresh,
		RefreshToken:     func(s session) string { return s.RefreshToken },
		ErrNoCredentials: ErrNoCredentials,
		ErrSignedOut:     ErrSignedOut,
		Now:              func() time.Time { return p.Now() },
	}

	return p
}

// newClient returns a client of one of Sony's services. They serve the session of the user's
// browser (the NPSSO), so requests look like that browser.
func newClient(baseURL string) *apiclient.Client {
	c := apiclient.New(baseURL)
	c.HTTP.Timeout = 30 * time.Second
	c.UserAgent = apiclient.BrowserUserAgent

	return c
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

// errGrantRefused means the token endpoint refused the code or the refresh token (400
// invalid_grant, 401, 403): the caller says which it was.
var errGrantRefused = errors.New("playstation refused the grant")

// errSessionRefused means Sony refused to renew the stored session: the source signs in again
// with the NPSSO.
var errSessionRefused = errors.New("playstation refused the stored session")

// exchange signs in with the NPSSO (oauthsession.Sessions.Exchange): the NPSSO gives a one-time
// authorization code, traded for a session. A refusal at either step means the NPSSO no longer
// works.
func (p *Provider) exchange(ctx context.Context, npsso string) (oauthsession.Grant[session], error) {
	code, err := p.authorize(ctx, npsso)
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	g, err := p.grant(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}, "")
	if errors.Is(err, errGrantRefused) {
		return oauthsession.Grant[session]{}, ErrSignedOut
	}

	return g, err
}

// refresh renews a session (oauthsession.Sessions.Refresh). A refused refresh token is
// errSessionRefused, after which withToken signs in again with the NPSSO.
func (p *Provider) refresh(ctx context.Context, s session) (oauthsession.Grant[session], error) {
	g, err := p.grant(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.RefreshToken},
		"scope":         {scope},
	}, s.RefreshToken)
	if errors.Is(err, errGrantRefused) {
		return oauthsession.Grant[session]{}, errSessionRefused
	}

	return g, err
}

// grant calls the token endpoint. Sony may leave the refresh token out of a refresh's answer: the
// previous one then stays.
func (p *Provider) grant(ctx context.Context, form url.Values, previous string) (oauthsession.Grant[session], error) {
	tok, err := p.token(ctx, form)
	if err != nil {
		return oauthsession.Grant[session]{}, err
	}

	refresh := tok.RefreshToken
	if refresh == "" {
		refresh = previous
	}

	return oauthsession.Grant[session]{
		Session:     session{RefreshToken: refresh},
		AccessToken: tok.AccessToken,
		Lifetime:    time.Duration(tok.ExpiresIn) * time.Second,
	}, nil
}

// authorize turns the NPSSO token into a one-time authorization code.
func (p *Provider) authorize(ctx context.Context, npsso string) (string, error) {
	res, err := p.Auth.Send(ctx, apiclient.Request{
		Path: "/authorize",
		Query: url.Values{
			"access_type":   {"offline"},
			"client_id":     {clientID},
			"redirect_uri":  {redirectURI},
			"response_type": {"code"},
			"scope":         {scope},
		},
		Header: http.Header{
			"Cookie": {"npsso=" + npsso},
			// A browser's navigation, not an API call: no Accept header, as before the shared
			// client, so Sony answers with its redirect.
			"Accept": nil,
		},
	})
	if err != nil {
		return "", fmt.Errorf("playstation sign-in: %w", apiclient.ClassifyError(ctx, err))
	}

	// A 429 or an outage is a passing trouble, not a refused NPSSO: it says nothing about it.
	if apiclient.Transient(res.Status) {
		return "", fmt.Errorf("playstation sign-in: %w", apiclient.ClassifyError(ctx, res.Err()))
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

// token calls the token endpoint with the PlayStation app's credentials.
func (p *Provider) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("token_format", "jwt")

	res, err := p.Auth.Send(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/token",
		Form:   form,
		Header: http.Header{"Authorization": {apiclient.BasicAuth(clientID, clientSecret)}},
	})
	if err != nil {
		return tokenResponse{}, fmt.Errorf("playstation sign-in: %w", apiclient.ClassifyError(ctx, err))
	}

	// Sony refuses a stale refresh token or code with a 400 invalid_grant; a 429 or an outage is a
	// passing trouble, after which the same session still works.
	if !res.OK() {
		if apiclient.Rejected(res.Status) {
			return tokenResponse{}, errGrantRefused
		}

		return tokenResponse{}, fmt.Errorf("playstation sign-in: %w", apiclient.ClassifyError(ctx, res.Err()))
	}

	var tok tokenResponse
	if err := res.Decode(&tok); err != nil || tok.AccessToken == "" {
		return tokenResponse{}, errors.New("playstation sign-in: unexpected answer")
	}

	return tok, nil
}

// signIn signs in with the NPSSO and stores the session it gives in settings. The NPSSO is passed
// alone, so the stored session (missing, or refused) is not kept instead.
func (p *Provider) signIn(ctx context.Context, settings source.Settings) error {
	out, err := p.sessions.Prepare(ctx, source.Settings{settingNPSSO: settings[settingNPSSO]})
	if err != nil {
		return err
	}

	settings[settingSession] = out[settingSession]

	return nil
}

// withToken runs call with an access token from the stored session, and once more with a renewed
// one when the library refuses the first (errTokenRejected): only a refused renewal means the user
// must sign in again. Without a session, or when Sony refuses to renew it, it signs in with the
// NPSSO first. The newest session is written back into settings for the caller to persist.
func (p *Provider) withToken(ctx context.Context, settings source.Settings, call func(token string) error) error {
	if _, err := p.sessions.Current(settings); err != nil {
		if err := p.signIn(ctx, settings); err != nil {
			return err
		}
	}

	err := p.sessions.WithRetry(ctx, settings, call)
	if !errors.Is(err, errSessionRefused) {
		return err
	}

	// The refused session may be the one this NPSSO gave minutes ago, still cached under it:
	// forget it, so the NPSSO is authorized again rather than the refused session handed back.
	p.sessions.ForgetCode(settings[settingNPSSO])

	if err := p.signIn(ctx, settings); err != nil {
		return err
	}

	err = p.sessions.WithRetry(ctx, settings, call)
	if errors.Is(err, errSessionRefused) {
		return ErrSignedOut
	}

	return err
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

// page reads one page of purchases with a registered query (by its hash), sent by GET as the
// library website does.
func (p *Provider) page(ctx context.Context, token, hash string, start int) ([]title, bool, error) {
	vars, err := json.Marshal(map[string]any{
		"isActive":            true,
		"platform":            []string{"ps4", "ps5"},
		"size":                pageSize,
		"start":               start,
		"sortBy":              "ACTIVE_DATE",
		"sortDirection":       "desc",
		"subscriptionService": "NONE",
	})
	if err != nil {
		return nil, false, fmt.Errorf("playstation library: %w", err)
	}

	ext, err := json.Marshal(map[string]any{"persistedQuery": map[string]any{
		"version":    1,
		"sha256Hash": hash,
	}})
	if err != nil {
		return nil, false, fmt.Errorf("playstation library: %w", err)
	}

	res, err := p.Library.Send(ctx, apiclient.Request{
		Query: url.Values{
			"operationName": {"getPurchasedGameList"},
			"variables":     {string(vars)},
			"extensions":    {string(ext)},
		},
		Header: http.Header{
			"Authorization": {"Bearer " + token},
			// Sony's GraphQL refuses requests that could be CSRF.
			"Content-Type": {"application/json"},
		},
	})
	if err != nil {
		return nil, false, fmt.Errorf("playstation library: %w", apiclient.ClassifyError(ctx, err))
	}

	// A 401 refuses the token whatever the body says (a web page as well as a GraphQL error).
	if res.Status == http.StatusUnauthorized {
		return nil, false, errTokenRejected
	}

	// A hash Sony no longer runs is refused before any GraphQL is read, with a bare message.
	var refusal struct {
		Message string `json:"message"`
	}
	if !apiclient.Transient(res.Status) && res.Decode(&refusal) == nil && strings.Contains(refusal.Message, "not whitelisted") {
		return nil, false, errQueryRetired
	}

	var data struct {
		Purchased *struct {
			Games    []title `json:"games"`
			PageInfo struct {
				IsLast     bool `json:"isLast"`
				TotalCount int  `json:"totalCount"`
			} `json:"pageInfo"`
		} `json:"purchasedTitlesRetrieve"`
	}

	// A rate limit or an outage is a passing trouble (res.GraphQL checks the status first).
	err = res.GraphQL(&data)

	var ge *apiclient.GraphQLError

	switch {
	case errors.As(err, &ge) && strings.Contains(ge.Message, "authorized"):
		return nil, false, errTokenRejected
	case err != nil:
		return nil, false, fmt.Errorf("playstation library: %w", apiclient.ClassifyError(ctx, err))
	case data.Purchased == nil:
		return nil, false, fmt.Errorf("playstation library: HTTP %d, no library in the answer", res.Status)
	}

	pg := data.Purchased
	last := pg.PageInfo.IsLast || len(pg.Games) < pageSize || start+len(pg.Games) >= pg.PageInfo.TotalCount && pg.PageInfo.TotalCount > 0

	return pg.Games, last, nil
}

var errQueryRetired = errors.New("playstation library: the library query is no longer registered by Sony")

// errTokenRejected means the library refused an access token: it may have expired or been revoked
// before its time. The sessions renew it once (WithRetry); only a refused renewal is signed out.
var errTokenRejected = fmt.Errorf("playstation rejected the access token: %w", apiclient.ErrTokenRejected)

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
