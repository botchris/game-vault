package gog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"testing"

	"gamevault/internal/domain/source"
)

// fakeGOG mimics the auth and embed endpoints.
type fakeGOG struct {
	mu        gosync.Mutex
	exchanges int
	refreshes int
	refresh   string
}

func (f *fakeGOG) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
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
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
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

	if res, err := p.Test(ctx, settings); err != nil || res.Count != 3 {
		t.Fatalf("test: %+v %v", res, err)
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
	if _, err := p2.Test(ctx, settings); err != nil {
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

	if _, err := p.Test(ctx, source.Settings{settingSession: `{"refresh_token":"stale","user_id":"u9"}`}); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("stale session: %v", err)
	}
	// A token GOG no longer accepts makes it redirect to the login page.
	p.tokens = map[string]accessToken{"u1": {token: "revoked", expires: p.Now().Add(1e12)}}
	if err := p.get(ctx, "revoked", p.EmbedURL+"/user/data/games", &struct{}{}); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("redirect to login: %v", err)
	}
}
