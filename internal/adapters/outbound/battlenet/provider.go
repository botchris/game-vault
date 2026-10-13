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
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Battle.net accounts.
const Type source.Type = "battlenet"

// Platform is the store name used on copies.
const Platform = game.PlatformBattlenet

const (
	settingCookies = "cookies"
	settingSession = "session"

	defaultAccountURL = "https://account.battle.net"

	// renewPath starts the single sign-on that gives account.battle.net a fresh session.
	renewPath = "/oauth2/authorization/account-settings"
)

var (
	// ErrNoCredentials means nothing usable was pasted.
	ErrNoCredentials = errkind.New(errkind.Invalid, "battle.net: paste the cookies of account.battle.net (see the help below the field)")

	// ErrSignedOut means Battle.net no longer accepts the cookies.
	ErrSignedOut = source.SignedOut("battle.net did not accept the cookies: sign in again on account.battle.net and paste them again")
)

// Provider implements sync.Provider for Battle.net accounts.
type Provider struct {
	// Account calls account.battle.net; tests point it at a fake server. Each scan sends through a
	// copy whose HTTP client holds the session's cookies.
	Account *apiclient.Client
}

// NewProvider returns the Battle.net source with its production endpoints.
func NewProvider() *Provider {
	// The site serves the session of the user's browser, so requests look like that browser.
	account := apiclient.New(defaultAccountURL)
	account.HTTP.Timeout = 30 * time.Second
	account.UserAgent = apiclient.BrowserUserAgent

	return &Provider{Account: account}
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{
	Key:  game.LinkBattlenet,
	Name: "Battle.net",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Battle.net",
		DescriptionKey: "sources.battlenet.description",
		Fields: []schema.Field{
			{
				Key:      settingCookies,
				LabelKey: "sources.battlenet.cookies",
				HelpKey:  "sources.battlenet.cookiesHelp",
				HelpURL:  defaultAccountURL + "/overview",
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    defaultAccountURL + "/overview",
					When: &schema.When{
						URLPrefix: defaultAccountURL + "/overview",
					},
					Capture: schema.Capture{
						Cookies: &schema.CookiesCapture{
							URL: defaultAccountURL + "/overview",
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

// getJSON reads an API path, renewing the site session once if it expired. Renewed cookies are
// written back into settings for the caller to persist.
func (p *Provider) getJSON(ctx context.Context, settings schema.Settings, path string, out any) error {
	c := browsersession.Current(settings[settingCookies], settings[settingSession])
	if len(c) == 0 {
		return ErrNoCredentials
	}

	sess, err := browsersession.New(p.Account.BaseURL, "battle.net", c, p.Account.HTTP.Timeout)
	if err != nil {
		return err
	}

	account := *p.Account
	account.HTTP = sess.Client

	err = api(ctx, &account, path, out)
	if errors.Is(err, ErrSignedOut) {
		if err := renew(ctx, &account); err != nil {
			return err
		}

		err = api(ctx, &account, path, out)
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
func (p *Provider) KeepAlive(ctx context.Context, settings schema.Settings) error {
	var overview any
	return p.getJSON(ctx, settings, "/api/games-and-subs", &overview)
}

// api reads an API path with the session's client. A refusal, the sign-in page or a web page
// instead of JSON is ErrSignedOut; a 429, an outage or no answer is a passing trouble.
func api(ctx context.Context, account *apiclient.Client, path string, out any) error {
	res, err := account.Send(ctx, apiclient.Request{Path: path})
	if err != nil {
		return fmt.Errorf("battle.net %s: %w", path, apiclient.ClassifyError(ctx, err))
	}

	switch {
	case apiclient.Transient(res.Status):
		return fmt.Errorf("battle.net %s: %w", path, apiclient.ClassifyError(ctx, res.Err()))
	case res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden || strings.Contains(res.URL.Path, "login"):
		return ErrSignedOut
	case !res.OK():
		return fmt.Errorf("battle.net %s: %w", path, res.Err())
	}

	if err := res.Decode(out); err != nil {
		return ErrSignedOut // an HTML page instead of JSON: not signed in
	}

	return nil
}

// renew follows the single sign-on round trip; it fails if it ends on a login page.
func renew(ctx context.Context, account *apiclient.Client) error {
	res, err := account.Send(ctx, apiclient.Request{
		Path: renewPath,
		// A navigation, not an API call: no Accept header, as before the shared client, so the
		// sign-in hops answer with their redirects rather than a JSON refusal.
		Header: http.Header{"Accept": nil},
	})
	if err != nil {
		return fmt.Errorf("battle.net session renewal: %w", apiclient.ClassifyError(ctx, err))
	}

	// A rate limit or an outage on the way says nothing about the session.
	if apiclient.Transient(res.Status) {
		return fmt.Errorf("battle.net session renewal: %w", apiclient.ClassifyError(ctx, res.Err()))
	}

	if res.Status >= 400 || strings.Contains(res.URL.Path, "login") {
		// Where the sign-in round trip ended (no query string: it may carry tokens).
		return fmt.Errorf("%w (session renewal ended at %s%s, HTTP %d)", ErrSignedOut, res.URL.Host, res.URL.Path, res.Status)
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
				out[id] = title{
					ID:   id,
					Name: strings.TrimSpace(name),
				}
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

func (p *Provider) titles(ctx context.Context, settings schema.Settings) (map[string]title, error) {
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
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.titles(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
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
			Links:      game.Links{LinkedStore.Key: t.ID},
			Title:      t.Name,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   Platform,
			},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out, warnings, nil
}
