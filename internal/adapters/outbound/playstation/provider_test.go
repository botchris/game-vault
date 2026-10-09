package playstation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gamevault/internal/domain/source"
)

// fakeSony mimics Sony's sign-in and the library GraphQL (only the second hash is registered).
type fakeSony struct {
	refreshes  int
	authorizes int
}

func (f *fakeSony) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("npsso"); err != nil || c.Value != "good-npsso" {
			http.Redirect(w, r, "https://my.account.sony.com/sonyacct/signin/", http.StatusFound)
			return
		}

		f.authorizes++

		w.Header().Set("Location", redirectURI+"/?code=v3.CODE&cid=x")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()

		if u, pw, _ := r.BasicAuth(); u != clientID || pw != clientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "v3.CODE" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
		case "refresh_token":
			if r.Form.Get("refresh_token") != "refresh-ok" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}

			f.refreshes++
		}

		fmt.Fprint(w, `{"access_token":"access","refresh_token":"refresh-ok","expires_in":3600}`)
	})
	mux.HandleFunc("GET /graphql", func(w http.ResponseWriter, r *http.Request) {
		var ext struct{ PersistedQuery struct{ Sha256Hash string } }
		json.Unmarshal([]byte(r.URL.Query().Get("extensions")), &ext)

		if ext.PersistedQuery.Sha256Hash != purchasedQueryHashes[1] {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"message":"Query %s not whitelisted"}`, ext.PersistedQuery.Sha256Hash)

			return
		}

		if r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("Content-Type") != "application/json" {
			fmt.Fprint(w, `{"errors":[{"message":"Access denied! You need to be authorized to perform this action!"}],"data":{"purchasedTitlesRetrieve":null}}`)
			return
		}

		var vars struct{ Start int }
		json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars)

		if vars.Start == 0 {
			var games []string
			for i := range pageSize - 2 {
				games = append(games, fmt.Sprintf(`{"name":"Game %02d","platform":"PS4","entitlementId":"E%d","membership":"NONE","isActive":true}`, i, i))
			}

			games = append(games, `{"name":"Returnal","platform":"PS5","entitlementId":"RET","membership":"NONE","isActive":true}`,
				`{"name":"Plus Game","platform":"PS5","entitlementId":"PLUS","membership":"PS_PLUS","isActive":true}`)
			fmt.Fprintf(w, `{"data":{"purchasedTitlesRetrieve":{"games":[%s],"pageInfo":{"isLast":false,"totalCount":26}}}}`, strings.Join(games, ","))

			return
		}

		fmt.Fprint(w, `{"data":{"purchasedTitlesRetrieve":{"games":[
		  {"name":"Bloodborne","platform":"PS4","entitlementId":"BB","membership":"NONE","isActive":true},
		  {"name":"Expired","platform":"PS4","entitlementId":"EX","membership":"NONE","isActive":false}],"pageInfo":{"isLast":true,"totalCount":26}}}}`)
	})

	return mux
}

func newTest(t *testing.T) (*Provider, *fakeSony) {
	f := &fakeSony{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	p := NewProvider()
	p.AuthURL, p.LibraryURL = srv.URL, srv.URL+"/graphql"

	return p, f
}

func TestFetch(t *testing.T) {
	p, f := newTest(t)
	settings := source.Settings{settingNPSSO: `{"npsso":"good-npsso"}`}

	copies, warnings, err := p.Fetch(context.Background(), settings)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("fetch: %v %v", err, warnings)
	}

	byTitle := map[string]string{}
	for _, c := range copies {
		byTitle[c.Title] = c.Details.Platform + "|" + c.ExternalID
	}

	if len(copies) != pageSize-2+2 || byTitle["Returnal"] != "PS5|psn:RET" || byTitle["Bloodborne"] != "PS4|psn:BB" {
		t.Fatalf("copies (%d): %v", len(copies), byTitle)
	}

	if _, ok := byTitle["Plus Game"]; ok {
		t.Fatal("PS Plus games must not be imported")
	}

	if !strings.Contains(settings[settingSession], "refresh-ok") {
		t.Fatalf("the session should be kept: %q", settings[settingSession])
	}

	// Next process: the refresh token is used, not the NPSSO.
	p2, _ := newTest(t)
	p2.AuthURL, p2.LibraryURL = p.AuthURL, p.LibraryURL
	before := f.authorizes

	if _, _, err := p2.Fetch(context.Background(), settings); err != nil {
		t.Fatal(err)
	}

	if f.authorizes != before || f.refreshes != 1 {
		t.Fatalf("authorizes=%d refreshes=%d", f.authorizes, f.refreshes)
	}
}

func TestSignedOut(t *testing.T) {
	p, _ := newTest(t)
	if _, _, err := p.Fetch(context.Background(), source.Settings{settingNPSSO: "expired"}); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("got %v", err)
	}

	if _, _, err := p.Fetch(context.Background(), source.Settings{}); !errors.Is(err, ErrNoNPSSO) {
		t.Fatalf("got %v", err)
	}
}
