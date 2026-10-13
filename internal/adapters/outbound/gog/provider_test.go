package gog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/source"
)

// fakeGOG mimics the auth and embed endpoints.
type fakeGOG struct {
	mu        gosync.Mutex
	exchanges int
	refreshes int
	refresh   string

	// tokenStatus, when set, is the only answer of the token endpoint (a rate limit, an outage).
	tokenStatus int

	// rejectAll makes the embed API reject every access token, as for a revoked session.
	rejectAll bool
}

func (f *fakeGOG) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			return
		}

		q := r.URL.Query()
		if q.Get("client_id") != clientID || q.Get("client_secret") != clientSecret {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_client"}`)

			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch q.Get("grant_type") {
		case "authorization_code":
			if q.Get("code") != "good-code" || q.Get("redirect_uri") != redirectURI || f.exchanges > 0 {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)

				return
			}

			f.exchanges++
		case "refresh_token":
			if q.Get("refresh_token") != f.refresh {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)

				return
			}

			f.refreshes++
		}

		f.refresh = fmt.Sprintf("refresh-%d", f.exchanges+f.refreshes)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + f.refresh, "expires_in": 3600, "refresh_token": f.refresh, "user_id": "u1",
		})
	})

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if f.rejectAll || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
				http.Redirect(w, r, "/en##openlogin", http.StatusFound) // what GOG does without a session
				return
			}

			h(w, r)
		}
	}
	mux.HandleFunc("GET /user/data/games", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"owned":[1,2,3]}`)
	}))
	mux.HandleFunc("GET /account/getFilteredProducts", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mediaType") != "1" {
			t := "mediaType must be 1"
			http.Error(w, t, http.StatusBadRequest)

			return
		}

		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `{"totalPages":2,"products":[{"id":1,"title":"The Witcher 3: Wild Hunt","isGame":true},{"id":2,"title":"  "}]}`)
			return
		}

		fmt.Fprint(w, `{"totalPages":2,"products":[{"id":3,"title":"Gwent","isGame":true},{"id":1,"title":"The Witcher 3: Wild Hunt","isGame":true}]}`)
	}))

	return mux
}

func newTestProvider(t *testing.T) (*Provider, *fakeGOG) {
	f := &fakeGOG{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	p := NewProvider()
	p.AuthURL, p.EmbedURL = srv.URL, srv.URL

	return p, f
}

func TestCleanCode(t *testing.T) {
	for in, want := range map[string]string{
		"abc123":  "abc123",
		` "abc" `: "abc",
		"https://embed.gog.com/on_login_success?origin=client&code=abc%2D123": "abc-123",
		"embed.gog.com/on_login_success?code=xyz&origin=client":               "xyz",
	} {
		if got := cleanCode(in); got != want {
			t.Errorf("cleanCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrepareTestAndFetch(t *testing.T) {
	p, f := newTestProvider(t)
	ctx := context.Background()

	pasted := "https://embed.gog.com/on_login_success?origin=client&code=good-code"

	settings, err := p.Prepare(ctx, source.Settings{settingAuthCode: pasted})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.Prepare(ctx, source.Settings{settingAuthCode: pasted}); err != nil {
		t.Fatalf("second prepare of the same code: %v", err)
	}

	if f.exchanges != 1 || settings[settingAuthCode] != "" || !strings.Contains(settings[settingSession], "refresh-1") {
		t.Fatalf("exchanges=%d settings=%v", f.exchanges, settings)
	}

	if err := p.Test(ctx, settings); err != nil {
		t.Fatalf("test: %v", err)
	}

	copies, _, err := p.Fetch(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(copies))
	for _, c := range copies {
		got = append(got, c.Title+"|"+c.ExternalID+"|"+c.Details.Platform)
	}

	if s := strings.Join(got, ","); s != "The Witcher 3: Wild Hunt|gog:1|GOG,Gwent|gog:3|GOG" {
		t.Fatalf("copies: %s", s)
	}

	// A fresh process refreshes the session, which rotates the stored refresh token.
	p2 := NewProvider()

	p2.AuthURL, p2.EmbedURL = p.AuthURL, p.EmbedURL
	if err := p2.Test(ctx, settings); err != nil {
		t.Fatal(err)
	}

	if f.refreshes != 1 || !strings.Contains(settings[settingSession], "refresh-2") {
		t.Fatalf("refreshes=%d session=%s", f.refreshes, settings[settingSession])
	}
}

func TestErrors(t *testing.T) {
	p, _ := newTestProvider(t)

	ctx := context.Background()
	if _, err := p.Prepare(ctx, source.Settings{}); !errors.Is(err, ErrNoSession) {
		t.Errorf("no code: %v", err)
	}

	if _, err := p.Prepare(ctx, source.Settings{settingAuthCode: "wrong"}); !errors.Is(err, ErrBadCode) {
		t.Errorf("wrong code: %v", err)
	}

	if err := p.Test(ctx, source.Settings{settingSession: `{"refresh_token":"stale","user_id":"u9"}`}); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("stale session: %v", err)
	}
	// A token GOG no longer accepts makes it redirect to the login page: the token is rejected (the
	// caller renews it once), which is not yet a signed-out session.
	p.tokens = map[string]accessToken{"u1": {
		token:   "revoked",
		expires: p.Now().Add(1e12),
	}}
	if err := p.get(ctx, "revoked", p.EmbedURL+"/user/data/games", &struct{}{}); !errors.Is(err, errTokenRejected) {
		t.Errorf("redirect to login: %v", err)
	}
}

// TestTokenEndpoint_passingTroubles checks that a rate limit, an outage or no answer from the token
// endpoint is not taken for a rejected code or session: signing in again would not help, and on
// GOG it would rotate the credentials for nothing.
func TestTokenEndpoint_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	stored := source.Settings{settingSession: `{"refresh_token":"refresh-s3cret","user_id":"u1"}`}

	for status, want := range map[int]error{
		http.StatusTooManyRequests:    provider.ErrRateLimited,
		http.StatusServiceUnavailable: provider.ErrUnavailable,
		0:                             provider.ErrUnavailable, // no answer at all
	} {
		t.Run(fmt.Sprintf("GIVEN the token endpoint answers HTTP %d", status), func(t *testing.T) {
			p, f := newTestProvider(t)
			f.tokenStatus = status

			if status == 0 {
				p.AuthURL = "http://127.0.0.1:1" // nothing listens there
			}

			t.Run("WHEN a code is pasted", func(t *testing.T) {
				_, err := p.Prepare(ctx, source.Settings{settingAuthCode: "good-code"})

				t.Run("THEN it is a passing trouble, not a rejected code", func(t *testing.T) {
					require.ErrorIs(t, err, want)
					assert.NotErrorIs(t, err, ErrBadCode)
				})
			})

			t.Run("WHEN the source is tested with a stored session", func(t *testing.T) {
				err := p.Test(ctx, maps.Clone(stored))

				t.Run("THEN it is a passing trouble, not a signed-out session", func(t *testing.T) {
					require.ErrorIs(t, err, want)
					assert.NotErrorIs(t, err, source.ErrSignedOut)
				})

				t.Run("AND the error never shows the refresh token sent in the URL", func(t *testing.T) {
					assert.NotContains(t, err.Error(), "refresh-s3cret")
				})
			})
		})
	}
}

// TestTest_renewsARejectedAccessToken checks that an access token GOG rejects (it expired or was
// revoked before its time) is renewed once, instead of asking the user to sign in again.
func TestTest_renewsARejectedAccessToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	stale := func(p *Provider) {
		p.tokens = map[string]accessToken{"u1": {
			token:   "stale-access",
			expires: p.Now().Add(time.Hour),
		}}
	}

	t.Run("GIVEN a cached access token GOG no longer accepts", func(t *testing.T) {
		p, f := newTestProvider(t)
		f.refresh = "refresh-0"

		stale(p)

		settings := source.Settings{settingSession: `{"refresh_token":"refresh-0","user_id":"u1"}`}

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			copies, _, err := p.Fetch(ctx, settings)

			t.Run("THEN the session is refreshed once and the scan works", func(t *testing.T) {
				require.NoError(t, err)
				assert.Len(t, copies, 2)
				assert.Equal(t, 1, f.refreshes)
				assert.Contains(t, settings[settingSession], "refresh-1", "the rotated session is handed back")
			})
		})
	})

	t.Run("GIVEN GOG rejects even a renewed access token", func(t *testing.T) {
		p, f := newTestProvider(t)
		f.refresh, f.rejectAll = "refresh-0", true

		stale(p)

		t.Run("WHEN the source is tested", func(t *testing.T) {
			err := p.Test(ctx, source.Settings{settingSession: `{"refresh_token":"refresh-0","user_id":"u1"}`})

			t.Run("THEN it is signed out after one renewal", func(t *testing.T) {
				require.ErrorIs(t, err, source.ErrSignedOut)
				assert.Equal(t, 1, f.refreshes)
			})
		})
	})
}
