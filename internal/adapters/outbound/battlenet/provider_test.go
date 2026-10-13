package battlenet

import (
	"context"
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

// fakeAccount mimics account.battle.net: the API needs a SESSION cookie; the renewal path gives a
// new one if the long-lived login cookie is valid, else it ends on the login page.
func fakeAccount(t *testing.T) *httptest.Server {
	srv, _ := fakeAccountPaths(t)

	return srv
}

// fakeAccountPaths is fakeAccount, with the paths it was asked for.
func fakeAccountPaths(t *testing.T) (*httptest.Server, *[]string) {
	var paths []string

	mux := http.NewServeMux()
	signedIn := func(r *http.Request) bool {
		c, err := r.Cookie("SESSION")
		return err == nil && c.Value == "fresh"
	}
	mux.HandleFunc("GET /api/games-and-subs", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("SESSION"); err == nil && c.Value == "outage" {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "<html>maintenance")

			return
		}

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
		// The renewal is a browser's navigation: a request that asks for JSON is refused rather
		// than redirected. This catches the shared client's default Accept leaking in.
		if _, ok := r.Header["Accept"]; ok {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"forbidden"}`)

			return
		}

		if c, err := r.Cookie("BA-tassadar"); err != nil || c.Value != "long-lived" {
			http.Redirect(w, r, "/login/en/", http.StatusFound)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:  "SESSION",
			Value: "fresh",
			Path:  "/",
		})
		http.Redirect(w, r, "/overview", http.StatusFound)
	})
	mux.HandleFunc("GET /login/en/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>login") })
	mux.HandleFunc("GET /overview", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>ok") })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv, &paths
}

func TestFetchRenewsTheSession(t *testing.T) {
	srv := fakeAccount(t)
	p := NewProvider()
	p.Account.BaseURL = srv.URL
	settings := schema.Settings{settingCookies: "Cookie: SESSION=expired; BA-tassadar=long-lived"}

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

// TestTest_readsOneEndpoint checks that "Test connection" reads the games overview only, not the
// classic games list a scan reads too.
func TestTest_readsOneEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a fresh session", func(t *testing.T) {
		srv, paths := fakeAccountPaths(t)
		p := NewProvider()
		p.Account.BaseURL = srv.URL

		t.Run("WHEN the source is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingCookies: "SESSION=fresh"})

			t.Run("THEN it works after one request", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, []string{"/api/games-and-subs"}, *paths)
			})
		})
	})
}

func TestSignedOut(t *testing.T) {
	srv := fakeAccount(t)
	p := NewProvider()
	p.Account.BaseURL = srv.URL

	err := p.Test(context.Background(), schema.Settings{settingCookies: "SESSION=expired; BA-tassadar=revoked"})
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("got %v", err)
	}

	if err := p.Test(context.Background(), schema.Settings{settingCookies: " "}); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("no cookies: %v", err)
	}
}

func TestFetch_outageIsNotSignedOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN account.battle.net answers 503", func(t *testing.T) {
		srv := fakeAccount(t)
		p := NewProvider()
		p.Account.BaseURL = srv.URL

		t.Run("WHEN a scan runs", func(t *testing.T) {
			_, _, err := p.Fetch(ctx, schema.Settings{settingCookies: "SESSION=outage; BA-tassadar=long-lived"})

			t.Run("THEN it is a passing trouble, not a signed-out session", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrUnavailable)
				assert.NotErrorIs(t, err, ErrSignedOut)
			})
		})
	})
}

// TestCollectTitles_idWithoutTitleID pins the id of a game Battle.net lists without a title id: the
// copy's ExternalID is built from it, so it must never change (game.TitleIDKey, not MatchKey).
func TestCollectTitles_idWithoutTitleID(t *testing.T) {
	out := map[string]title{}
	collectTitles(map[string]any{"localizedGameName": "Example Quest™ (Battle.net Key)"}, out, map[string]bool{})

	assert.Equal(t, map[string]title{"example quest": {
		ID:   "example quest",
		Name: "Example Quest™ (Battle.net Key)",
	}}, out)
}

// TestFetch_titlesWithoutAnID checks that games without a title id whose names give no id either
// (no Latin letters or digits) are skipped and counted, instead of all sharing one id.
func TestFetch_titlesWithoutAnID(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an account whose classic games include two without a title id, named in other scripts", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/games-and-subs":
				fmt.Fprint(w, `{"gameAccounts":[{"titleId":5272175,"localizedGameName":"Overwatch® 2"}]}`)
			case "/api/classic-games":
				fmt.Fprint(w, `{"classicGames":[{"localizedGameName":"ディアブロ"},{"localizedGameName":"Варкрафт"},{"localizedGameName":"ディアブロ"}]}`)
			}
		}))
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.Account.BaseURL = srv.URL

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, schema.Settings{settingCookies: "SESSION=fresh"})
			require.NoError(t, err)

			t.Run("THEN only the game with an id is imported, and the others are counted once each", func(t *testing.T) {
				require.Len(t, copies, 1)
				assert.Equal(t, "battlenet:5272175", copies[0].ExternalID)
				assert.Equal(t, []string{"2 Battle.net items were not imported: games without a title id whose name has no Latin letters or digits to make one from"}, warnings)
			})
		})
	})
}
