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

	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/source"
)

func fakeEA(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connect/auth", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "good" {
			fmt.Fprint(w, `{"error_code":"login_required","error":"login_required"}`)
			return
		}

		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "good", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "_nx_mpcid", Value: "renewed", Path: "/"})
		fmt.Fprint(w, `{"access_token":"tok","token_type":"Bearer","expires_in":"3599"}`)
	})
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			fmt.Fprint(w, `{"errors":[{"message":"Not authenticated.","extensions":{"code":"UNAUTHENTICATED"}}],"data":null}`)
			return
		}

		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)

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
	srv := fakeEA(t)
	p := NewProvider()
	p.AccountsURL, p.GraphQLURL = srv.URL, srv.URL+"/graphql"

	return p
}

func TestFetch(t *testing.T) {
	p := newTest(t)
	settings := source.Settings{settingCookies: "Cookie: sid=good; remid=r"}

	copies, warnings, err := p.Fetch(context.Background(), settings)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("fetch: %v %v", err, warnings)
	}

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

func TestSignedOut(t *testing.T) {
	p := newTest(t)
	if _, _, err := p.Fetch(context.Background(), source.Settings{settingCookies: "sid=expired"}); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("got %v", err)
	}

	if _, _, err := p.Fetch(context.Background(), source.Settings{settingCookies: ""}); !errors.Is(err, ErrNoCookies) {
		t.Fatalf("no cookies: %v", err)
	}
}
