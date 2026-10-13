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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/source"
)

// fakeSony mimics Sony's sign-in, the library GraphQL (only the second hash is registered) and
// the PlayStation app's played-titles list.
type fakeSony struct {
	refreshes  int
	authorizes int

	// library, when set, replaces the default purchases with these games (JSON objects), on one page.
	library []string

	// played are the played titles (JSON objects of the gamelist API) the account has.
	played []playedFixture

	// gameListStatus, when set, makes the played-titles list fail with this HTTP status.
	gameListStatus int

	// gameListRaw, when set, is answered as is (with HTTP 200) by the played-titles list.
	gameListRaw string

	// gameListCalls counts the requests to the played-titles list, by categories parameter.
	gameListCalls map[string]int

	// authStatus, when set, is the only answer of the sign-in endpoints (authorize and token): a
	// rate limit, an outage.
	authStatus int

	// rejectAll makes the library refuse every access token, as for a revoked session.
	rejectAll bool
}

func (f *fakeSony) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		if f.authStatus != 0 {
			w.WriteHeader(f.authStatus)
			return
		}

		if c, err := r.Cookie("npsso"); err != nil || c.Value != "good-npsso" {
			http.Redirect(w, r, "https://my.account.sony.com/sonyacct/signin/", http.StatusFound)
			return
		}

		f.authorizes++

		w.Header().Set("Location", redirectURI+"/?code=v3.CODE&cid=x")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if f.authStatus != 0 {
			w.WriteHeader(f.authStatus)
			return
		}

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

		if f.rejectAll || r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("Content-Type") != "application/json" {
			fmt.Fprint(w, `{"errors":[{"message":"Access denied! You need to be authorized to perform this action!"}],"data":{"purchasedTitlesRetrieve":null}}`)
			return
		}

		var vars struct{ Start int }
		json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars)

		if f.library != nil {
			fmt.Fprintf(w, `{"data":{"purchasedTitlesRetrieve":{"games":[%s],"pageInfo":{"isLast":true,"totalCount":%d}}}}`, strings.Join(f.library, ","), len(f.library))
			return
		}

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
	mux.HandleFunc("GET /gamelist", f.gameList)

	return mux
}

func newTest(t *testing.T) (*Provider, *fakeSony) {
	f := &fakeSony{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	p := NewProvider(nil)
	p.AuthURL, p.LibraryURL, p.GameListURL = srv.URL, srv.URL+"/graphql", srv.URL+"/gamelist"

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
	p2.AuthURL, p2.LibraryURL, p2.GameListURL = p.AuthURL, p.LibraryURL, p.GameListURL
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

// TestSignIn_passingTroubles checks that a rate limit or an outage of Sony's sign-in is not taken
// for a refused NPSSO or session: signing in again would not help.
func TestSignIn_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for status, want := range map[int]error{
		http.StatusTooManyRequests:    provider.ErrRateLimited,
		http.StatusServiceUnavailable: provider.ErrUnavailable,
	} {
		t.Run(fmt.Sprintf("GIVEN the sign-in endpoints answer HTTP %d", status), func(t *testing.T) {
			p, f := newTest(t)
			f.authStatus = status

			for name, settings := range map[string]source.Settings{
				"an NPSSO":         {settingNPSSO: "good-npsso"},
				"a stored session": {settingSession: `{"refresh_token":"refresh-ok"}`},
			} {
				t.Run("WHEN the library is scanned with "+name, func(t *testing.T) {
					_, _, err := p.Fetch(ctx, settings)

					t.Run("THEN it is a passing trouble, not a signed-out session", func(t *testing.T) {
						require.ErrorIs(t, err, want)
						assert.NotErrorIs(t, err, source.ErrSignedOut)
					})
				})
			}
		})
	}
}

// TestFetch_renewsARejectedAccessToken checks that an access token the library refuses (it expired
// or was revoked before its time) is renewed once, instead of asking the user to sign in again.
func TestFetch_renewsARejectedAccessToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	stale := func(p *Provider) {
		p.tokens = map[string]accessToken{"refresh-ok": {
			token:   "stale",
			expires: p.Now().Add(time.Hour),
		}}
	}

	t.Run("GIVEN a cached access token the library no longer accepts", func(t *testing.T) {
		p, f := newTest(t)
		stale(p)

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			copies, _, err := p.Fetch(ctx, source.Settings{settingSession: `{"refresh_token":"refresh-ok"}`})

			t.Run("THEN the token is renewed once and the scan works", func(t *testing.T) {
				require.NoError(t, err)
				assert.NotEmpty(t, copies)
				assert.Equal(t, 1, f.refreshes)
			})
		})
	})

	t.Run("GIVEN a library that refuses even a renewed access token", func(t *testing.T) {
		p, f := newTest(t)
		f.rejectAll = true

		stale(p)

		t.Run("WHEN the source is tested", func(t *testing.T) {
			err := p.Test(ctx, source.Settings{settingSession: `{"refresh_token":"refresh-ok"}`})

			t.Run("THEN it is signed out after one renewal", func(t *testing.T) {
				require.ErrorIs(t, err, source.ErrSignedOut)
				assert.Equal(t, 1, f.refreshes)
			})
		})
	})
}

func TestMapTitles_systems(t *testing.T) {
	t.Run("GIVEN titles bought for PS5, for PS4 and for a console Sony names in no known way", func(t *testing.T) {
		got, _ := mapTitles([]title{
			{
				Name:          "Astro Bot",
				EntitlementID: "e1",
				Platform:      "PS5",
			},
			{
				Name:          "Gravity Rush",
				EntitlementID: "e2",
				Platform:      "PS4",
			},
			{
				Name:          "Old Thing",
				EntitlementID: "e3",
				Platform:      "PSX",
			},
		}, nil)

		t.Run("THEN the copies say their console, and the unknown one leaves it to the platform", func(t *testing.T) {
			require.Len(t, got, 3)
			assert.Equal(t, "PS5", got[0].System)
			assert.Equal(t, "PS4", got[1].System)
			assert.Empty(t, got[2].System)
			assert.Equal(t, "PlayStation Store", got[2].Details.Platform)
		})
	})
}
