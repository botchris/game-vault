// Package eaapp implements the EA app source provider. EA has no public library API (the old
// Origin API is shut down) and its sign-in has a captcha, so it reuses the browser's session on
// accounts.ea.com, like Battle.net:
//
//  1. the user copies the cookies of accounts.ea.com;
//  2. each scan asks accounts.ea.com for a short-lived access token with them (the same silent
//     sign-in EA's own web pages do), keeping any cookie EA renews on the way;
//  3. the token reads the owned games from the GraphQL API the EA app uses.
//
// It only reads the library: nothing is bought, installed or changed in the account.
package eaapp

import (
	"context"
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

// Type is the source type id of EA accounts.
const Type source.Type = "eaapp"

// Platform is the store name used on copies, matching Humble's Origin / EA keys.
const Platform = "EA App"

const (
	settingCookies = "cookies"
	settingSession = "session"

	defaultAccountsURL = "https://accounts.ea.com"
	defaultGraphQLURL  = "https://service-aggregation-layer.juno.ea.com/graphql"

	// tokenPath is the silent sign-in of EA's web SDK: with a signed-in browser session it answers
	// {"access_token": …}, otherwise {"error": "login_required"}.
	tokenPath = "/connect/auth?client_id=ORIGIN_JS_SDK&response_type=token&redirect_uri=nucleus:rest&prompt=none"
	maxPages  = 50
)

// LoginCheckURL shows a token when the browser is signed in to EA: the page whose cookies to copy.
const LoginCheckURL = defaultAccountsURL + tokenPath

var (
	// ErrNoCredentials means nothing usable was pasted.
	ErrNoCredentials = errkind.New(errkind.Invalid, "ea: paste the cookies of accounts.ea.com (see the help below the field)")

	// ErrSignedOut means EA no longer accepts the cookies.
	ErrSignedOut = source.SignedOut("EA did not accept the cookies: sign in again on ea.com and paste the cookies of accounts.ea.com again")
)

// ownedGamesQuery lists full games bought on EA's own store for PC. EA Play (subscription) games
// are filtered out afterwards by ownership method.
const ownedGamesQuery = `query OwnedGames($next: String) {
  me {
    ownedGameProducts(locale: "DEFAULT", entitlementEnabled: true, storefronts: [EA],
      type: [DIGITAL_FULL_GAME, PACKAGED_FULL_GAME], platforms: [PC], paging: {limit: 100, next: $next}) {
      next
      items {
        originOfferId
        product {
          id
          name
          baseItem { title gameType }
          gameProductUser { ownershipMethods }
        }
      }
    }
  }
}`

// Provider implements sync.Provider for EA accounts.
type Provider struct {
	// Accounts calls accounts.ea.com for the access token; tests point it at a fake server. Each
	// sign-in sends through a copy whose HTTP client holds the session's cookies.
	Accounts *apiclient.Client

	// Library calls the EA app's GraphQL endpoint (its BaseURL is the endpoint itself).
	Library *apiclient.Client
}

// NewProvider returns the EA app source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{
		Accounts: newClient(defaultAccountsURL),
		Library:  newClient(defaultGraphQLURL),
	}
}

// newClient returns a client of one of EA's services. They serve the session of the user's
// browser, so requests look like that browser.
func newClient(baseURL string) *apiclient.Client {
	c := apiclient.New(baseURL)
	c.HTTP.Timeout = 30 * time.Second
	c.UserAgent = apiclient.BrowserUserAgent

	return c
}

// LinkedStore is the store this package links games to: the source sets the link on every copy it
// imports, and the cover provider reads it.
var LinkedStore = game.Store{
	Key:  "ea",
	Name: "EA app",
}

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "EA app",
		DescriptionKey: "sources.eaapp.description",
		Fields: []source.Field{
			{
				Key:      settingCookies,
				LabelKey: "sources.eaapp.cookies",
				HelpKey:  "sources.eaapp.cookiesHelp",
				HelpURL:  LoginCheckURL,
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    "https://www.ea.com/",
					Hosts:   []string{"accounts.ea.com"},
					When: &schema.When{
						Fetch: &schema.FetchCapture{
							URL:   LoginCheckURL,
							Field: "access_token",
						},
					},
					Capture: schema.Capture{
						Cookies: &schema.CookiesCapture{
							URL: LoginCheckURL,
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

// accessToken signs in silently with the browser cookies. Cookies EA renews are written back into
// settings for the caller to persist.
func (p *Provider) accessToken(ctx context.Context, settings source.Settings) (string, error) {
	c := browsersession.Current(settings[settingCookies], settings[settingSession])
	if len(c) == 0 {
		return "", ErrNoCredentials
	}

	sess, err := browsersession.New(p.Accounts.BaseURL, "ea.com", c, p.Accounts.HTTP.Timeout)
	if err != nil {
		return "", err
	}

	accounts := *p.Accounts
	accounts.HTTP = sess.Client

	res, err := accounts.Send(ctx, apiclient.Request{Path: tokenPath})
	if err != nil {
		return "", fmt.Errorf("ea sign-in: %w", apiclient.ClassifyError(ctx, err))
	}

	// A rate limit or an outage says nothing about the cookies. Any other answer is read whatever
	// its status: EA says "login_required" in the body.
	if apiclient.Transient(res.Status) {
		return "", fmt.Errorf("ea sign-in: %w", apiclient.ClassifyError(ctx, res.Err()))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if res.Decode(&tok) != nil || tok.AccessToken == "" {
		if tok.Error != "" && tok.Error != "login_required" {
			return "", fmt.Errorf("ea sign-in: %s", tok.Error)
		}

		return "", ErrSignedOut
	}

	settings[settingSession] = browsersession.Remember(settings[settingCookies], settings[settingSession], sess.Renewed())

	return tok.AccessToken, nil
}

// KeepAliveInterval implements sync.KeepAliver.
func (p *Provider) KeepAliveInterval() time.Duration { return time.Hour }

// KeepAlive implements sync.KeepAliver: a silent sign-in keeps EA's cookies fresh.
func (p *Provider) KeepAlive(ctx context.Context, settings source.Settings) error {
	_, err := p.accessToken(ctx, settings)
	return err
}

type item struct {
	OriginOfferID string `json:"originOfferId"`
	Product       struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		BaseItem struct {
			Title    string `json:"title"`
			GameType string `json:"gameType"`
		} `json:"baseItem"`
		GameProductUser struct {
			OwnershipMethods []string `json:"ownershipMethods"`
		} `json:"gameProductUser"`
	} `json:"product"`
}

// ownedItems lists the owned games, a page at a time. The token comes from a fresh silent sign-in,
// so a refused one means the cookies no longer sign in.
func (p *Provider) ownedItems(ctx context.Context, token string) ([]item, error) {
	header := http.Header{"Authorization": {"Bearer " + token}}

	var all []item

	next := "0"

	for range maxPages {
		var data struct {
			Me struct {
				OwnedGameProducts struct {
					Next  string `json:"next"`
					Items []item `json:"items"`
				} `json:"ownedGameProducts"`
			} `json:"me"`
		}

		err := p.Library.GraphQL(ctx, "", apiclient.GraphQLBody{
			Query:     ownedGamesQuery,
			Variables: map[string]any{"next": next},
		}, header, &data)

		switch {
		case apiclient.IsGraphQLCode(err, "UNAUTHENTICATED"):
			return nil, ErrSignedOut
		case err != nil:
			return nil, fmt.Errorf("ea library: %w", apiclient.ClassifyError(ctx, err))
		}

		page := data.Me.OwnedGameProducts

		all = append(all, page.Items...)
		if page.Next == "" || page.Next == next || len(page.Items) == 0 {
			return all, nil
		}

		next = page.Next
	}

	return all, nil
}

// subscriptionOnly reports whether a game is only playable through EA Play / Game Pass, so it is
// not really owned.
func subscriptionOnly(methods []string) bool {
	if len(methods) == 0 {
		return false
	}

	for _, m := range methods {
		m = strings.ToUpper(m)
		if !strings.Contains(m, "VAULT") && !strings.Contains(m, "SUBSCRIPTION") {
			return false
		}
	}

	return true
}

// mapItems turns owned products into copies: one per base game, with the product name as the
// edition when it differs ("Battlefield 1 Revolution" of "Battlefield 1").
func mapItems(items []item) []game.ImportedCopy {
	byGame := map[string]game.ImportedCopy{}

	for _, it := range items {
		if subscriptionOnly(it.Product.GameProductUser.OwnershipMethods) {
			continue
		}

		name := strings.TrimSpace(it.Product.Name)

		title := strings.TrimSpace(it.Product.BaseItem.Title)
		if title == "" {
			title = name
		}

		if title == "" {
			continue
		}

		id := it.Product.ID
		if id == "" {
			id = it.OriginOfferID
		}

		if id == "" {
			id = game.MatchKey(title)
		}

		edition := ""
		if name != "" && game.MatchKey(name) != game.MatchKey(title) {
			edition = strings.TrimSpace(strings.TrimPrefix(name, title))

			edition = strings.TrimLeft(edition, " :-–")
			if edition == "" {
				edition = name
			}
		}

		key := game.MatchKey(title)
		if prev, ok := byGame[key]; ok && (prev.Details.Edition != "" || edition == "") {
			continue // keep the first, preferring a named edition
		}

		byGame[key] = game.ImportedCopy{
			ExternalID: "ea:" + id,
			Links:      game.Links{LinkedStore.Key: id},
			Title:      title,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: Platform,
				Status:   game.StatusOwned,
				Origin:   "EA app",
				Edition:  edition,
			},
		}
	}

	out := make([]game.ImportedCopy, 0, len(byGame))
	for _, c := range byGame {
		out = append(out, c)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	return out
}

// Test implements sync.Provider: signs in and lists the library.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, _, err := p.Fetch(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	token, err := p.accessToken(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	items, err := p.ownedItems(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	copies := mapItems(items)

	var warnings []string
	if len(copies) == 0 {
		warnings = append(warnings, "EA returned no games for this account (EA Play games are not imported)")
	}

	return copies, warnings, nil
}
