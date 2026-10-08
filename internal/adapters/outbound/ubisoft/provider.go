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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Ubisoft accounts.
const Type source.Type = "ubisoft"

// Platform is the store name used on copies, matching Humble's Uplay keys.
const Platform = "Ubisoft Connect"

const (
	settingLoginData = "login_data"
	settingSession   = "session"

	defaultAPIURL = "https://public-ubiservices.ubi.com"
	// webAppID is ubisoft.com's sign-in (its tickets are renewed with it); pcAppID is the Ubisoft
	// Connect PC client. connect.ubisoft.com's own app (314d4fef…), used by Lutris and the GOG Galaxy
	// plugin, is no longer allowed to sign in on the public gateway.
	webAppID  = "f35adcb5-1911-440c-b1c9-48fdc1701c68"
	pcAppID   = "f68a4bb5-608a-4ff2-8123-be8ef797e0a6"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Safari/537.36"
)

// LoginURL is the sign-in page whose stored login data the user copies.
const LoginURL = "https://connect.ubisoft.com/login?appId=" + webAppID +
	"&genomeId=5b36b900-65d8-47f3-93c8-86bdaa48ab50&lang=en-US&nextUrl=https:%2F%2Fconnect.ubisoft.com%2Fready"

// appIDs are tried in order to renew a session; the one that works is remembered.
var appIDs = []string{webAppID, pcAppID}

var (
	// ErrNoLoginData means nothing usable was pasted.
	ErrNoLoginData = errors.New("ubisoft: paste the login data of connect.ubisoft.com (see the help below the field)")
	// ErrSignedOut means Ubisoft no longer accepts the ticket.
	ErrSignedOut = errors.New("ubisoft did not accept the login data: its remember-me ticket is replaced every time it is used, so a value copied earlier stops working. Sign in again on connect.ubisoft.com (a private window is best) and paste the new PRODrememberMe value")

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
	RememberMeTicket, Ticket, SessionID string
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

// Provider implements sync.Provider and sync.Tester for Ubisoft accounts.
type Provider struct {
	APIURL string
	Client *http.Client
	Now    func() time.Time

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
	return &Provider{APIURL: defaultAPIURL, Client: &http.Client{Timeout: 30 * time.Second}, Now: time.Now}
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Ubisoft Connect",
		DescriptionKey: "sources.ubisoft.description",
		Fields: []source.Field{
			{Key: settingLoginData, LabelKey: "sources.ubisoft.loginData", HelpKey: "sources.ubisoft.loginDataHelp",
				HelpURL: LoginURL, Kind: source.FieldSecret, Required: true},
			{Key: settingSession, Kind: source.FieldState},
		},
	}
}

type session struct {
	ticket, sessionID, appID string
}

// session opens a session, spending the remember-me ticket as little as possible because Ubisoft
// replaces it on every use: a session opened recently is reused, then the ticket and session id
// pasted (still valid for a few hours after copying them), and only then the remember-me ticket is
// renewed. The new remember-me ticket is kept in memory and written back into settings for the
// caller to persist. fresh skips the reuse (the previous session was rejected).
func (p *Provider) session(ctx context.Context, settings source.Settings, fresh bool) (session, error) {
	pasted := settings[settingLoginData]
	key := hash(pasted)
	d := parseLoginData(pasted)

	p.mu.Lock()
	c, open := p.sessions[key]
	st := p.rotated[key]
	p.mu.Unlock()

	var saved state
	if json.Unmarshal([]byte(settings[settingSession]), &saved) != nil || saved.From != key {
		saved = state{}
	}

	if st.RememberMeTicket == "" {
		st = saved
	}
	// The ticket in memory may be newer than the saved one (a test, a scan or a keep-alive rotated
	// it): always hand it back for the caller to persist, or a restart would lose it.
	if st.RememberMeTicket != "" && st != saved {
		raw, _ := json.Marshal(st)
		settings[settingSession] = string(raw)
	}

	if !fresh && open && p.Now().Before(c.expires) {
		return c.s, nil
	}

	if !fresh && st.RememberMeTicket == "" && d.Ticket != "" && d.SessionID != "" {
		return session{d.Ticket, d.SessionID, webAppID}, nil // checked by the first query
	}

	if st.RememberMeTicket != "" {
		d.RememberMeTicket = st.RememberMeTicket
	}

	if d.RememberMeTicket == "" {
		return session{}, ErrNoLoginData
	}

	order := appIDs
	if st.AppID != "" {
		order = append([]string{st.AppID}, appIDs...)
	}

	err := ErrSignedOut

	for _, app := range order {
		var (
			sess session
			rm   string
		)

		sess, rm, err = p.renew(ctx, d.RememberMeTicket, app)
		if errors.Is(err, ErrSignedOut) {
			continue // a ticket may only be renewable by the app that issued it
		}

		if err != nil {
			return session{}, err
		}

		if rm == "" {
			rm = d.RememberMeTicket
		}

		next := state{From: key, RememberMeTicket: rm, AppID: app}

		p.mu.Lock()
		if p.rotated == nil {
			p.rotated = map[string]state{}
		}

		if p.sessions == nil {
			p.sessions = map[string]cached{}
		}

		p.rotated[key] = next
		p.sessions[key] = cached{s: sess, expires: p.Now().Add(sessionTTL)}
		p.mu.Unlock()

		raw, _ := json.Marshal(next)
		settings[settingSession] = string(raw)

		return sess, nil
	}

	return session{}, err
}

// renew exchanges a remember-me ticket for a session as the given app.
func (p *Provider) renew(ctx context.Context, rememberMe, app string) (session, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.APIURL+"/v3/profiles/sessions", strings.NewReader(`{"rememberMe":true}`))
	if err != nil {
		return session{}, "", err
	}

	req.Header.Set("Authorization", "rm_v1 t="+rememberMe)
	p.headers(req, app)

	res, err := p.Client.Do(req)
	if err != nil {
		return session{}, "", err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return session{}, "", ErrSignedOut
	}

	if res.StatusCode != http.StatusOK {
		return session{}, "", fmt.Errorf("ubisoft sign-in: HTTP %d", res.StatusCode)
	}

	var s struct {
		Ticket           string `json:"ticket"`
		SessionID        string `json:"sessionId"`
		RememberMeTicket string `json:"rememberMeTicket"`
	}
	if err := json.Unmarshal(body, &s); err != nil || s.Ticket == "" || s.SessionID == "" {
		return session{}, "", errors.New("ubisoft sign-in: unexpected answer")
	}

	return session{s.Ticket, s.SessionID, app}, s.RememberMeTicket, nil
}

func (p *Provider) headers(req *http.Request, app string) {
	req.Header.Set("Ubi-AppId", app)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
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
	body, _ := json.Marshal(map[string]any{"operationName": "OwnedGames", "query": q,
		"variables": map[string]any{"limit": pageSize, "offset": offset}})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.APIURL+"/v1/profiles/me/uplay/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Authorization", "Ubi_v1 t="+s.ticket)
	req.Header.Set("Ubi-SessionId", s.sessionID)
	p.headers(req, s.appID)

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))

	var out struct {
		Data struct {
			Viewer struct {
				Games struct {
					TotalCount int    `json:"totalCount"`
					Nodes      []node `json:"nodes"`
				} `json:"games"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, 0, fmt.Errorf("ubisoft library: HTTP %d, unexpected answer", res.StatusCode)
	}

	if len(out.Errors) > 0 {
		e := out.Errors[0]
		if e.Extensions.Code == "INVALID_TICKET" || e.Extensions.Code == "UNAUTHENTICATED" {
			return nil, 0, ErrSignedOut
		}

		return nil, 0, fmt.Errorf("ubisoft library: %s", e.Message)
	}

	g := out.Data.Viewer.Games

	return g.Nodes, g.TotalCount, nil
}

// all reads every page of a query.
func (p *Provider) all(ctx context.Context, s session, q string) ([]node, error) {
	var nodes []node
	for page := 0; page < maxPages; page++ {
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

func (p *Provider) owned(ctx context.Context, settings source.Settings) ([]node, error) {
	read := func(fresh bool) ([]node, error) {
		s, err := p.session(ctx, settings, fresh)
		if err != nil {
			return nil, err
		}

		nodes, err := p.all(ctx, s, ownedGamesQuery)
		if err != nil && !errors.Is(err, ErrSignedOut) {
			nodes, err = p.all(ctx, s, minimalQuery)
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
func (p *Provider) KeepAlive(ctx context.Context, settings source.Settings) error {
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
// left out (they are not in the Ubisoft Connect library).
func mapNodes(nodes []node) []game.ImportedCopy {
	seen := map[string]bool{}

	var out []game.ImportedCopy

	for _, n := range nodes {
		title := strings.TrimSpace(n.Name)

		id := n.SpaceID
		if id == "" {
			id = n.ID
		}

		if title == "" || id == "" || seen[id] || !onPC(n) {
			continue
		}

		seen[id] = true
		out = append(out, game.ImportedCopy{
			ExternalID: "ubisoft:" + id,
			Title:      title,
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: Platform, Status: game.StatusOwned, Origin: "Ubisoft Connect"},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out
}

// Test implements sync.Tester.
func (p *Provider) Test(ctx context.Context, settings source.Settings) (sync.TestResult, error) {
	copies, _, err := p.Fetch(ctx, settings)
	return sync.TestResult{Count: len(copies), Unit: "copies"}, err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	nodes, err := p.owned(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	copies := mapNodes(nodes)

	var warnings []string
	if len(copies) == 0 {
		warnings = append(warnings, "Ubisoft returned no PC games for this account")
	}

	return copies, warnings, nil
}
