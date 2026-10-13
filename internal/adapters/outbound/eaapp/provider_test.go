package eaapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// fakeEA serves EA's silent sign-in and the library GraphQL; queries collects the library queries
// it received.
func fakeEA(t *testing.T, queries *[]string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connect/auth", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "good" {
			fmt.Fprint(w, `{"error_code":"login_required","error":"login_required"}`)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:  "sid",
			Value: "good",
			Path:  "/",
		})
		http.SetCookie(w, &http.Cookie{
			Name:  "_nx_mpcid",
			Value: "renewed",
			Path:  "/",
		})
		fmt.Fprint(w, `{"access_token":"tok","token_type":"Bearer","expires_in":"3599"}`)
	})
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer busy" {
			// The gateway's rate limit: a 429 whose body may still carry an "errors" list.
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"errors":[{"message":"Too many requests","extensions":{"code":"UNAUTHENTICATED"}}]}`)

			return
		}

		if r.Header.Get("Authorization") != "Bearer tok" {
			fmt.Fprint(w, `{"errors":[{"message":"Not authenticated.","extensions":{"code":"UNAUTHENTICATED"}}],"data":null}`)
			return
		}

		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		*queries = append(*queries, body.Query)

		if !strings.Contains(body.Query, "ownedGameProducts") {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}

		if body.Variables["next"] == "0" {
			fmt.Fprint(w, `{"data":{"me":{"ownedGameProducts":{"next":"p2","items":[
			  {"originOfferId":"Origin.OFR.1","product":{"id":"bf1","name":"Battlefield™ 1 Revolution","baseItem":{"title":"Battlefield™ 1","gameType":"BASE_GAME"},"gameProductUser":{"ownershipMethods":["PURCHASE"]}}},
			  {"originOfferId":"Origin.OFR.2","product":{"id":"bf1std","name":"Battlefield™ 1","baseItem":{"title":"Battlefield™ 1"},"gameProductUser":{"ownershipMethods":["PURCHASE"]}}}]}}}}`)

			return
		}

		fmt.Fprint(w, `{"data":{"me":{"ownedGameProducts":{"next":"","items":[
		  {"originOfferId":"Origin.OFR.3","product":{"id":"me","name":"Mass Effect™ Legendary Edition","baseItem":{"title":"Mass Effect™ Legendary Edition"},"gameProductUser":{"ownershipMethods":["REDEMPTION"]}}},
		  {"originOfferId":"Origin.OFR.4","product":{"id":"fifa","name":"EA SPORTS FC™ 24","baseItem":{"title":"EA SPORTS FC™ 24"},"gameProductUser":{"ownershipMethods":["VAULT"]}}}]}}}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func newTest(t *testing.T) *Provider {
	p, _ := newTestQueries(t)

	return p
}

// newTestQueries returns a provider on a fake EA, and the library queries it receives.
func newTestQueries(t *testing.T) (*Provider, *[]string) {
	var queries []string

	srv := fakeEA(t, &queries)
	p := NewProvider()
	p.Accounts.BaseURL = srv.URL
	p.Library.BaseURL = srv.URL + "/graphql"

	return p, &queries
}

func TestFetch(t *testing.T) {
	p := newTest(t)
	settings := schema.Settings{settingCookies: "Cookie: sid=good; remid=r"}

	copies, warnings, err := p.Fetch(context.Background(), settings)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// The EA Play game is left out and counted.
	assert.Equal(t, []string{"1 EA item was not imported: EA Play games"}, warnings)

	got := make([]string, 0, len(copies))
	for _, c := range copies {
		got = append(got, c.Title+"|"+c.Details.Edition+"|"+c.ExternalID+"|"+c.Details.Platform)
	}

	want := "Battlefield™ 1|Revolution|ea:bf1|EA App,Mass Effect™ Legendary Edition||ea:me|EA App"
	if s := strings.Join(got, ","); s != want {
		t.Fatalf("copies:\n got %s\nwant %s", s, want)
	}

	if c := browsersession.Current(settings[settingCookies], settings[settingSession]); c["_nx_mpcid"] != "renewed" || c["remid"] != "r" {
		t.Fatalf("renewed cookies should be kept: %v", c)
	}
}

// TestTest_readsOnePage checks that "Test connection" signs in and sends the request of a scan's
// first library page, with the scan's page size, and stops there.
func TestTest_readsOnePage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN cookies that sign in and a library of two pages", func(t *testing.T) {
		p, queries := newTestQueries(t)

		t.Run("WHEN the source is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingCookies: "Cookie: sid=good; remid=r"})

			t.Run("THEN it works after one library request, the first page of a scan", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, *queries, 1)
				assert.Contains(t, (*queries)[0], "limit: 100,")
			})
		})
	})
}

func TestSignedOut(t *testing.T) {
	p := newTest(t)
	if _, _, err := p.Fetch(context.Background(), schema.Settings{settingCookies: "sid=expired"}); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("got %v", err)
	}

	if _, _, err := p.Fetch(context.Background(), schema.Settings{settingCookies: ""}); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("no cookies: %v", err)
	}
}

func TestOwnedItems_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN EA's GraphQL gateway rate-limits the token", func(t *testing.T) {
		p := newTest(t)

		t.Run("WHEN the library is read", func(t *testing.T) {
			_, err := p.ownedItems(ctx, "busy", maxPages)

			t.Run("THEN it is a rate limit, not a signed-out session", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.NotErrorIs(t, err, ErrSignedOut)
			})
		})
	})

	t.Run("GIVEN accounts.ea.com is down", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.Accounts.BaseURL = srv.URL

		t.Run("WHEN a scan signs in", func(t *testing.T) {
			_, _, err := p.Fetch(ctx, schema.Settings{settingCookies: "sid=good"})

			t.Run("THEN it is an outage, not a signed-out session", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrUnavailable)
				assert.NotErrorIs(t, err, ErrSignedOut)
			})
		})
	})
}

// TestMapItems_idWithoutProductID pins the ExternalID of a game the EA app lists without a product or
// offer id: it is built from the title and must never change (game.TitleIDKey, not MatchKey).
func TestMapItems_idWithoutProductID(t *testing.T) {
	var it item

	it.Product.Name = "Example Racer™ Deluxe Edition"
	it.Product.BaseItem.Title = "Example Racer™"

	got, warnings := mapItems([]item{it})
	require.Len(t, got, 1)
	assert.Equal(t, "ea:example racer", got[0].ExternalID)
	assert.Empty(t, warnings)
}

// TestMapItems_titlesWithoutAnID checks that games without a product id whose titles give no id
// either (no Latin letters or digits) are skipped and counted, instead of all sharing one id.
func TestMapItems_titlesWithoutAnID(t *testing.T) {
	t.Run("GIVEN two games without a product id, titled in a script the id leaves out", func(t *testing.T) {
		var first, second item

		first.Product.BaseItem.Title = "ドラゴンの冒険"
		second.Product.BaseItem.Title = "Приключение"

		t.Run("WHEN they are mapped", func(t *testing.T) {
			got, warnings := mapItems([]item{first, second})

			t.Run("THEN neither is imported, and both are counted in a warning", func(t *testing.T) {
				assert.Empty(t, got)
				assert.Equal(t, []string{"2 EA items were not imported: " + unkeyed}, warnings)
			})
		})
	})
}
