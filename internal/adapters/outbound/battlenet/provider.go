// Package battlenet implements the Battle.net source provider. Blizzard's public API has no list of
// owned games, and signing in needs a captcha, so it reuses the browser's session on
// account.battle.net, the way Playnite and the GOG Galaxy plugin do: the user copies the cookies
// of that site and scans read the "Games & subscriptions" data its own pages load.
//
// The site's session is short-lived; when it expires Game Vault renews it through Battle.net's
// single sign-on (the longer-lived login cookie), like the browser does, and keeps the renewed
// cookies. It only reads: nothing is changed in the account.
package battlenet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Battle.net accounts.
const Type source.Type = "battlenet"

// Platform is the store name used on copies.
const Platform = "Battle.net"

const (
	settingCookies = "cookies"
	settingSession = "session"

	defaultAccountURL = "https://account.battle.net"

	// renewPath starts the single sign-on that gives account.battle.net a fresh session.
	renewPath = "/oauth2/authorization/account-settings"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Safari/537.36"
)

var (
	// ErrNoCookies means nothing usable was pasted.
	ErrNoCookies = errors.New("battle.net: paste the cookies of account.battle.net (see the help below the field)")

	// ErrSignedOut means Battle.net no longer accepts the cookies.
	ErrSignedOut = errors.New("battle.net did not accept the cookies: sign in again on account.battle.net and paste them again")
)

// Provider implements sync.Provider for Battle.net accounts.
type Provider struct {
	AccountURL string
	Timeout    time.Duration
}

// NewProvider returns the Battle.net source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{AccountURL: defaultAccountURL, Timeout: 30 * time.Second}
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Battle.net",
		DescriptionKey: "sources.battlenet.description",
		Fields: []source.Field{
			{Key: settingCookies, LabelKey: "sources.battlenet.cookies", HelpKey: "sources.battlenet.cookiesHelp",
				HelpURL: defaultAccountURL + "/overview", Kind: source.FieldSecret, Required: true},
			{Key: settingSession, Kind: source.FieldState},
		},
	}
}

// getJSON reads an API path, renewing the site session once if it expired. Renewed cookies are
// written back into settings for the caller to persist.
func (p *Provider) getJSON(ctx context.Context, settings source.Settings, path string, out any) error {
	c := browsersession.Current(settings[settingCookies], settings[settingSession])
	if len(c) == 0 {
		return ErrNoCookies
	}

	sess, err := browsersession.New(p.AccountURL, "battle.net", c, p.Timeout)
	if err != nil {
		return err
	}

	err = p.api(ctx, sess.Client, path, out)
	if errors.Is(err, ErrSignedOut) {
		if err := p.renew(ctx, sess.Client); err != nil {
			return err
		}

		err = p.api(ctx, sess.Client, path, out)
	}

	if err != nil {
		return err
	}
	// Keep every cookie the site renewed (a refreshed session, the renewal's new session…).
	settings[settingSession] = browsersession.Remember(settings[settingCookies], settings[settingSession], sess.Renewed())

	return nil
}

// KeepAliveInterval implements sync.KeepAliver: the site's session expires when idle.
func (p *Provider) KeepAliveInterval() time.Duration { return 15 * time.Minute }

// KeepAlive implements sync.KeepAliver with a light request, as an open browser tab would.
func (p *Provider) KeepAlive(ctx context.Context, settings source.Settings) error {
	var overview any
	return p.getJSON(ctx, settings, "/api/games-and-subs", &overview)
}

func (p *Provider) api(ctx context.Context, client *http.Client, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.AccountURL+path, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || strings.Contains(res.Request.URL.Path, "login"):
		return ErrSignedOut
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("battle.net %s: HTTP %d", path, res.StatusCode)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return ErrSignedOut // an HTML page instead of JSON: not signed in
	}

	return nil
}

// renew follows the single sign-on round trip; it fails if it ends on a login page.
func (p *Provider) renew(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.AccountURL+renewPath, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", userAgent)

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Only the status and the final URL matter; draining the body lets the connection be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))

	if res.StatusCode >= 400 || strings.Contains(res.Request.URL.Path, "login") {
		// Where the sign-in round trip ended (no query string: it may carry tokens).
		return fmt.Errorf("%w (session renewal ended at %s%s, HTTP %d)", ErrSignedOut, res.Request.URL.Host, res.Request.URL.Path, res.StatusCode)
	}

	return nil
}

// title is one game the account has, wherever it appears in the API answers.
type title struct {
	ID   string
	Name string
}

// collectTitles walks a JSON answer for objects naming a game ("localizedGameName"), so small
// changes in how Battle.net nests them do not break scans.
func collectTitles(v any, out map[string]title) {
	switch x := v.(type) {
	case map[string]any:
		if name, ok := x["localizedGameName"].(string); ok && strings.TrimSpace(name) != "" {
			id := fmt.Sprint(firstOf(x, "titleId", "gameTitleId", "productId"))
			if id == "" || id == "<nil>" {
				id = game.MatchKey(name)
			}

			if _, seen := out[id]; !seen {
				out[id] = title{ID: id, Name: strings.TrimSpace(name)}
			}
		}

		for _, child := range x {
			collectTitles(child, out)
		}
	case []any:
		for _, child := range x {
			collectTitles(child, out)
		}
	}
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if f, ok := v.(float64); ok {
				return int64(f)
			}

			return v
		}
	}

	return nil
}

func (p *Provider) titles(ctx context.Context, settings source.Settings) (map[string]title, error) {
	found := map[string]title{}

	var games any
	if err := p.getJSON(ctx, settings, "/api/games-and-subs", &games); err != nil {
		return nil, err
	}

	collectTitles(games, found)
	// Older games (Diablo II, Warcraft III…) are listed apart; their absence is not an error.
	var classic any
	if err := p.getJSON(ctx, settings, "/api/classic-games", &classic); err == nil {
		collectTitles(classic, found)
	}

	return found, nil
}

// Test implements sync.Provider.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, err := p.titles(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	found, err := p.titles(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	var warnings []string
	if len(found) == 0 {
		warnings = append(warnings, "battle.net returned no games for this account")
	}
	// One copy per game, even if the account has it in several regions.
	byName := map[string]title{}
	for _, t := range found {
		if prev, ok := byName[game.MatchKey(t.Name)]; !ok || t.ID < prev.ID {
			byName[game.MatchKey(t.Name)] = t
		}
	}

	out := make([]game.ImportedCopy, 0, len(byName))
	for _, t := range byName {
		out = append(out, game.ImportedCopy{
			ExternalID: "battlenet:" + t.ID,
			Title:      t.Name,
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: Platform, Status: game.StatusOwned, Origin: "Battle.net"},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out, warnings, nil
}
