// Package ubisoft implements the Ubisoft Connect source provider. Ubisoft has no public library
// API and its sign-in has a captcha, so it reuses the session of Ubisoft's own website, the way
// Lutris and the GOG Galaxy plugin do:
//
//  1. the user signs in on connect.ubisoft.com and copies the login data the site keeps in the
//     browser (it holds a long-lived "remember me" ticket);
//  2. each scan exchanges that ticket for a session, keeping the renewed ticket Ubisoft returns;
//  3. the session reads the owned games from the GraphQL API of Ubisoft Connect.
//
// It only reads the library: nothing is bought, installed or changed in the account.
package ubisoft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Ubisoft accounts.
const Type source.Type = "ubisoft"

// Platform is the store name used on copies, matching Humble's Uplay keys.
const Platform = game.PlatformUbisoft

const (
	settingLoginData = "login_data"
	settingSession   = "session"

	defaultAPIURL = "https://public-ubiservices.ubi.com"

	// webAppID is ubisoft.com's sign-in (its tickets are renewed with it); pcAppID is the Ubisoft
	// Connect PC client. connect.ubisoft.com's own app (314d4fef…), used by Lutris and the GOG Galaxy
	// plugin, is no longer allowed to sign in on the public gateway.
	webAppID = "f35adcb5-1911-440c-b1c9-48fdc1701c68"
	pcAppID  = "f68a4bb5-608a-4ff2-8123-be8ef797e0a6"
)

// LoginURL is the sign-in page whose stored login data the user copies.
const LoginURL = "https://connect.ubisoft.com/login?appId=" + webAppID +
	"&genomeId=5b36b900-65d8-47f3-93c8-86bdaa48ab50&lang=en-US&nextUrl=https:%2F%2Fconnect.ubisoft.com%2Fready"

// appIDs are tried in order to renew a session; the one that works is remembered.
var appIDs = []string{webAppID, pcAppID}

var (
	// ErrNoCredentials means nothing usable was pasted.
	ErrNoCredentials = errkind.New(errkind.Invalid, "ubisoft: paste the login data of connect.ubisoft.com (see the help below the field)")

	// ErrSignedOut means Ubisoft no longer accepts the ticket.
	ErrSignedOut = source.SignedOut("ubisoft did not accept the login data: its remember-me ticket is replaced every time it is used, so a value copied earlier stops working. Sign in again on connect.ubisoft.com (a private window is best) and paste the new PRODrememberMe value")

	reField = regexp.MustCompile(`"(rememberMeTicket|ticket|sessionId)"\s*:\s*"([^"]+)"`)

	// reBareTicket is a remember-me ticket pasted on its own, without the JSON around it.
	reBareTicket = regexp.MustCompile(`^[A-Za-z0-9+/=_.\-]{40,}$`)
)

// ownedGamesQuery is the library query of Ubisoft Connect (as used by Lutris). Platform groups tell
// PC games from console ones linked to the account.
const ownedGamesQuery = `query OwnedGames($limit: Int, $offset: Int) {
  viewer {
    games(filterBy: {isOwned: true}, limit: $limit, offset: $offset) {
      totalCount
      nodes {
        id
        spaceId
        name
        viewer { meta { ownedPlatformGroups { name type } } }
      }
    }
  }
}`

// minimalQuery is tried when Ubisoft rejects the full one (fields renamed on their side).
const minimalQuery = `query OwnedGames($limit: Int, $offset: Int) {
  viewer { games(filterBy: {isOwned: true}, limit: $limit, offset: $offset) { totalCount nodes { id spaceId name } } }
}`

// pageSize is the largest page Ubisoft allows ("limit must be between 0 and 50").
const pageSize = 50

// maxPages guards against a paging loop (50 × 100 = 5,000 games).
const maxPages = 100

// loginData is what the site keeps in the browser after signing in.
type loginData struct {
	RememberMeTicket string
	Ticket           string
	SessionID        string
}

// parseLoginData reads the login data the site keeps in Local Storage (the PRODrememberMe entry),
// or a remember-me ticket pasted alone.
func parseLoginData(raw string) loginData {
	var d loginData
	if t := strings.Trim(strings.TrimSpace(raw), `"`); reBareTicket.MatchString(t) {
		d.RememberMeTicket = t
		return d
	}

	for _, m := range reField.FindAllStringSubmatch(raw, -1) {
		switch m[1] {
		case "rememberMeTicket":
			d.RememberMeTicket = m[2]
		case "ticket":
			d.Ticket = m[2]
		case "sessionId":
			d.SessionID = m[2]
		}
	}

	return d
}

// state keeps the renewed remember-me ticket, tied to the pasted data it derives from.
type state struct {
	From             string `json:"from"`
	RememberMeTicket string `json:"remember_me_ticket"`
	AppID            string `json:"app_id,omitempty"`
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Provider implements sync.Provider for Ubisoft accounts.
//
// It keeps its own memory of the rotated remember-me ticket and of the open session rather than
// oauthsession's: the ticket is not an OAuth refresh token (no code, no access token lifetime), and
// the pasted ticket and session id are tried before spending it.
type Provider struct {
	// API calls Ubisoft's public gateway (sign-in and library); tests point it at a fake server.
	API *apiclient.Client

	// Now is the clock.
	Now func() time.Time

	mu       gosync.Mutex
	rotated  map[string]state  // hash(pasted) → latest remember-me ticket, until the source is saved
	sessions map[string]cached // hash(pasted) → open session, so tests and scans do not rotate the ticket again
}

type cached struct {
	s       session
	expires time.Time
}

// sessionTTL is how long an open session is reused (Ubisoft's last a few hours).
const sessionTTL = time.Hour

// NewProvider returns the Ubisoft Connect source with its production endpoints.
func NewProvider() *Provider {
	// The gateway serves the session of the user's browser, so requests look like that browser.
	api := apiclient.New(defaultAPIURL)
	api.HTTP.Timeout = 30 * time.Second
	api.UserAgent = apiclient.BrowserUserAgent

	return &Provider{
		API: api,
		Now: time.Now,
	}
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{
	Key:  game.LinkUbisoft,
	Name: "Ubisoft Connect",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Ubisoft Connect",
		DescriptionKey: "sources.ubisoft.description",
		Fields: []schema.Field{
			{
				Key:      settingLoginData,
				LabelKey: "sources.ubisoft.loginData",
				HelpKey:  "sources.ubisoft.loginDataHelp",
				HelpURL:  LoginURL,
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    LoginURL,
					Private: true,
					Capture: schema.Capture{
						Storage: &schema.StorageCapture{
							Origin: "https://connect.ubisoft.com",
							Key:    "PRODrememberMe",
							Path:   "/ready",
						},
					},
				},
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:  settingSession,
				Kind: schema.FieldState,
			},
		},
	}
}

type session struct {
	ticket    string
	sessionID string
	appID     string
}

// session opens a session, spending the remember-me ticket as little as possible because Ubisoft
// replaces it on every use: a session opened recently is reused, then the ticket and session id
// pasted (still valid for a few hours after copying them), and only then the remember-me ticket is
// renewed. The newest remember-me ticket is written back into settings for the caller to persist,
// on every call. fresh skips the reuse (the previous session was rejected).
func (p *Provider) session(ctx context.Context, settings schema.Settings, fresh bool) (session, error) {
	key := hash(settings[settingLoginData])
	d := parseLoginData(settings[settingLoginData])
	st := p.latestState(settings, key)

	if !fresh {
		if s, ok := p.reuse(key, d, st); ok {
			return s, nil
		}
	}

	rememberMe := st.RememberMeTicket
	if rememberMe == "" {
		rememberMe = d.RememberMeTicket
	}

	if rememberMe == "" {
		return session{}, ErrNoCredentials
	}

	sess, next, err := p.renewAny(ctx, rememberMe, st.AppID)
	if err != nil {
		return session{}, err
	}

	next.From = key

	p.mu.Lock()
	if p.rotated == nil {
		p.rotated = map[string]state{}
	}

	if p.sessions == nil {
		p.sessions = map[string]cached{}
	}

	p.rotated[key] = next
	p.sessions[key] = cached{
		s:       sess,
		expires: p.Now().Add(sessionTTL),
	}
	p.mu.Unlock()

	raw, _ := json.Marshal(next)
	settings[settingSession] = string(raw)

	return sess, nil
}

// latestState returns the newest remember-me ticket known for the pasted login data (key is its
// hash): the one in memory, else the saved one. The ticket in memory may be newer than the saved
// one (a test, a scan or a keep-alive rotated it), so it is always written back into settings for
// the caller to persist, or a restart would lose it.
func (p *Provider) latestState(settings schema.Settings, key string) state {
	p.mu.Lock()
	st := p.rotated[key]
	p.mu.Unlock()

	var saved state
	if json.Unmarshal([]byte(settings[settingSession]), &saved) != nil || saved.From != key {
		saved = state{}
	}

	if st.RememberMeTicket == "" {
		return saved
	}

	if st != saved {
		raw, _ := json.Marshal(st)
		settings[settingSession] = string(raw)
	}

	return st
}

// reuse returns a session that needs no renewal: one opened recently for the pasted login data d
// (key is its hash), or, before any remember-me ticket was renewed (st is empty), the ticket and
// session id pasted with it, which the first query checks.
func (p *Provider) reuse(key string, d loginData, st state) (session, bool) {
	p.mu.Lock()
	c, open := p.sessions[key]
	p.mu.Unlock()

	switch {
	case open && p.Now().Before(c.expires):
		return c.s, true
	case st.RememberMeTicket == "" && d.Ticket != "" && d.SessionID != "":
		return session{d.Ticket, d.SessionID, webAppID}, true
	}

	return session{}, false
}

// renewAny renews the remember-me ticket with the first app that accepts it, the remembered one
// (last, possibly empty) first, then the others once each: a ticket may only be renewable by the app
// that issued it. It returns the session and the state to keep (without From); ErrSignedOut when
// every app refused the ticket.
func (p *Provider) renewAny(ctx context.Context, rememberMe, last string) (session, state, error) {
	order := appIDs
	if last != "" {
		order = append([]string{last}, slices.DeleteFunc(slices.Clone(appIDs), func(a string) bool { return a == last })...)
	}

	for _, app := range order {
		sess, rm, err := p.renew(ctx, rememberMe, app)
		if errors.Is(err, ErrSignedOut) {
			continue
		}

		if err != nil {
			return session{}, state{}, err
		}

		if rm == "" {
			rm = rememberMe
		}

		return sess, state{
			RememberMeTicket: rm,
			AppID:            app,
		}, nil
	}

	return session{}, state{}, ErrSignedOut
}

// renew exchanges a remember-me ticket for a session as the given app.
func (p *Provider) renew(ctx context.Context, rememberMe, app string) (session, string, error) {
	var s struct {
		Ticket           string `json:"ticket"`
		SessionID        string `json:"sessionId"`
		RememberMeTicket string `json:"rememberMeTicket"`
	}

	err := p.API.Do(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/v3/profiles/sessions",
		Header: http.Header{
			"Authorization": {"rm_v1 t=" + rememberMe},
			"Ubi-AppId":     {app},
		},
		Body: map[string]bool{"rememberMe": true},
	}, &s)

	switch {
	case apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden):
		return session{}, "", ErrSignedOut
	case err != nil:
		return session{}, "", fmt.Errorf("ubisoft sign-in: %w", apiclient.ClassifyError(ctx, err))
	case s.Ticket == "" || s.SessionID == "":
		return session{}, "", errors.New("ubisoft sign-in: unexpected answer")
	}

	return session{s.Ticket, s.SessionID, app}, s.RememberMeTicket, nil
}

type node struct {
	ID      string `json:"id"`
	SpaceID string `json:"spaceId"`
	Name    string `json:"name"`
	Viewer  struct {
		Meta struct {
			OwnedPlatformGroups []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"ownedPlatformGroups"`
		} `json:"meta"`
	} `json:"viewer"`
}

// query reads one page of owned games and the total number of them.
func (p *Provider) query(ctx context.Context, s session, q string, offset int) ([]node, int, error) {
	var data struct {
		Viewer struct {
			Games struct {
				TotalCount int    `json:"totalCount"`
				Nodes      []node `json:"nodes"`
			} `json:"games"`
		} `json:"viewer"`
	}

	err := p.API.GraphQL(ctx, "/v1/profiles/me/uplay/graphql", apiclient.GraphQLBody{
		OperationName: "OwnedGames",
		Query:         q,
		Variables: map[string]any{
			"limit":  pageSize,
			"offset": offset,
		},
	}, http.Header{
		"Authorization": {"Ubi_v1 t=" + s.ticket},
		"Ubi-SessionId": {s.sessionID},
		"Ubi-AppId":     {s.appID},
	}, &data)

	// An expired ticket is reported as a GraphQL error code, or by the gateway as a 401: the
	// session is renewed with the remember-me ticket.
	switch {
	case apiclient.IsGraphQLCode(err, "INVALID_TICKET", "UNAUTHENTICATED") || apiclient.IsStatus(err, http.StatusUnauthorized):
		return nil, 0, ErrSignedOut
	case err != nil:
		return nil, 0, fmt.Errorf("ubisoft library: %w", apiclient.ClassifyError(ctx, err))
	}

	g := data.Viewer.Games

	return g.Nodes, g.TotalCount, nil
}

// all reads up to pages pages of a query.
func (p *Provider) all(ctx context.Context, s session, q string, pages int) ([]node, error) {
	var nodes []node
	for range pages {
		batch, total, err := p.query(ctx, s, q, len(nodes))
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, batch...)
		if len(batch) < pageSize || len(nodes) >= total {
			break
		}
	}

	return nodes, nil
}

// owned reads up to pages pages of owned games, opening a new session once when the reused or
// pasted one expired.
func (p *Provider) owned(ctx context.Context, settings schema.Settings, pages int) ([]node, error) {
	read := func(fresh bool) ([]node, error) {
		s, err := p.session(ctx, settings, fresh)
		if err != nil {
			return nil, err
		}

		// Only a query Ubisoft rejects is retried as the minimal one: after a rate limit or an
		// outage, a second query would meet the same trouble.
		nodes, err := p.all(ctx, s, ownedGamesQuery, pages)

		var rejected *apiclient.GraphQLError
		if errors.As(err, &rejected) {
			nodes, err = p.all(ctx, s, minimalQuery, pages)
		}

		return nodes, err
	}

	nodes, err := read(false)
	if errors.Is(err, ErrSignedOut) {
		// The reused or pasted session expired: open a new one with the remember-me ticket.
		nodes, err = read(true)
	}

	return nodes, err
}

// KeepAliveInterval implements sync.KeepAliver. Sessions last a few hours; renewing within that
// keeps the remember-me ticket chain alive and saved.
func (p *Provider) KeepAliveInterval() time.Duration { return 2 * time.Hour }

// KeepAlive implements sync.KeepAliver: opens a new session, rotating and saving the ticket.
func (p *Provider) KeepAlive(ctx context.Context, settings schema.Settings) error {
	_, err := p.session(ctx, settings, true)
	return err
}

// onPC reports whether a game is owned on PC. Games without platform information are kept.
func onPC(n node) bool {
	groups := n.Viewer.Meta.OwnedPlatformGroups
	if len(groups) == 0 {
		return true
	}

	for _, g := range groups {
		if strings.EqualFold(g.Type, "PC") || strings.Contains(strings.ToUpper(g.Name), "PC") {
			return true
		}
	}

	return false
}

// mapNodes turns owned games into copies, one per game; console games linked to the account are
// left out (they are not in the Ubisoft Connect library) and counted in the warnings it returns.
func mapNodes(nodes []node) ([]game.ImportedCopy, []string) {
	seen := map[string]bool{}
	skipped := source.Skipped{Store: "Ubisoft"}

	var out []game.ImportedCopy

	for _, n := range nodes {
		title := strings.TrimSpace(n.Name)

		id := n.SpaceID
		if id == "" {
			id = n.ID
		}

		if title == "" || id == "" || seen[id] {
			continue
		}

		seen[id] = true

		if !onPC(n) {
			skipped.Add("console games, which are not in the Ubisoft Connect library")
			continue
		}

		out = append(out, game.ImportedCopy{
			ExternalID: "ubisoft:" + id,
			Links:      game.Links{LinkedStore.Key: id},
			Title:      title,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   Platform,
			},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out, skipped.Warnings()
}

// Test implements sync.Provider: it opens (or reuses) a session and sends the request of a scan's
// first library page.
// Opening a session renews the remember-me ticket, which is handed back as a scan's is; the open
// session is then reused by a scan that follows, so a test does not cost a second renewal.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.owned(ctx, settings, 1)

	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
	nodes, err := p.owned(ctx, settings, maxPages)
	if err != nil {
		return nil, nil, err
	}

	copies, warnings := mapNodes(nodes)
	if len(copies) == 0 {
		warnings = append(warnings, "Ubisoft returned no PC games for this account")
	}

	return copies, warnings, nil
}
