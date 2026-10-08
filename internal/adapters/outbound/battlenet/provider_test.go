package battlenet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gamevault/internal/adapters/outbound/browsersession"
	"gamevault/internal/domain/source"
)

// fakeAccount mimics account.battle.net: the API needs a SESSION cookie; the renewal path gives a
// new one if the long-lived login cookie is valid, else it ends on the login page.
func fakeAccount(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	signedIn := func(r *http.Request) bool {
		c, err := r.Cookie("SESSION")
		return err == nil && c.Value == "fresh"
	}
	mux.HandleFunc("GET /api/games-and-subs", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(r) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "<html>")

			return
		}

		fmt.Fprint(w, `{"gameAccounts":[
		  {"titleId":5730135,"localizedGameName":"World of Warcraft®","gameAccountRegion":"EU"},
		  {"titleId":5730136,"localizedGameName":"World of Warcraft®","gameAccountRegion":"US"},
		  {"titleId":5272175,"localizedGameName":"Overwatch® 2"}],
		  "subscriptions":[{"localizedGameName":"World of Warcraft®","titleId":5730135}]}`)
	})
	mux.HandleFunc("GET /api/classic-games", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		fmt.Fprint(w, `{"classicGames":[{"localizedGameName":"Diablo® II"}]}`)
	})
	mux.HandleFunc("GET "+renewPath, func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("BA-tassadar"); err != nil || c.Value != "long-lived" {
			http.Redirect(w, r, "/login/en/", http.StatusFound)
			return
		}

		http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "fresh", Path: "/"})
		http.Redirect(w, r, "/overview", http.StatusFound)
	})
	mux.HandleFunc("GET /login/en/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>login") })
	mux.HandleFunc("GET /overview", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>ok") })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestFetchRenewsTheSession(t *testing.T) {
	srv := fakeAccount(t)
	p := NewProvider()
	p.AccountURL = srv.URL
	settings := source.Settings{settingCookies: "Cookie: SESSION=expired; BA-tassadar=long-lived"}

	copies, warnings, err := p.Fetch(context.Background(), settings)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("fetch: %v %v", err, warnings)
	}

	titles := make([]string, 0, len(copies))
	for _, c := range copies {
		titles = append(titles, c.Title+"|"+c.Details.Platform)
	}

	if got := strings.Join(titles, ","); got != "Diablo® II|Battle.net,Overwatch® 2|Battle.net,World of Warcraft®|Battle.net" {
		t.Fatalf("copies: %s", got)
	}

	if !strings.Contains(settings[settingSession], `"SESSION":"fresh"`) {
		t.Fatalf("the renewed session should be kept: %q", settings[settingSession])
	}
	// The next scan uses the renewed session directly.
	if c := browsersession.Current(settings[settingCookies], settings[settingSession]); c["SESSION"] != "fresh" || c["BA-tassadar"] != "long-lived" {
		t.Fatalf("cookies: %v", c)
	}
	// Pasting new cookies discards what was renewed from the old ones.
	settings[settingCookies] = "SESSION=other"
	if c := browsersession.Current(settings[settingCookies], settings[settingSession]); c["SESSION"] != "other" {
		t.Fatalf("cookies after a new paste: %v", c)
	}
}

func TestSignedOut(t *testing.T) {
	srv := fakeAccount(t)
	p := NewProvider()
	p.AccountURL = srv.URL

	_, err := p.Test(context.Background(), source.Settings{settingCookies: "SESSION=expired; BA-tassadar=revoked"})
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("got %v", err)
	}

	if _, err := p.Test(context.Background(), source.Settings{settingCookies: " "}); !errors.Is(err, ErrNoCookies) {
		t.Fatalf("no cookies: %v", err)
	}
}
