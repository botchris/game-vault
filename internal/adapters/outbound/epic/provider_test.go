package epic

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
	"time"

	"gamevault/internal/domain/source"
)

// fakeEpic mimics the OAuth, library and catalog endpoints.
type fakeEpic struct {
	mu        gosync.Mutex
	exchanges int
	refreshes int
	refresh   string // current valid refresh token
}

func (f *fakeEpic) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /account/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != clientID || p != clientSecret {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}

		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "good-code" || f.exchanges > 0 {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"errorCode":"errors.com.epicgames.account.oauth.authorization_code_not_found"}`)

				return
			}

			f.exchanges++
		case "refresh_token":
			if r.Form.Get("refresh_token") != f.refresh {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"errorCode":"errors.com.epicgames.account.auth_token.invalid_refresh_token"}`)

				return
			}

			f.refreshes++
		}

		f.refresh = fmt.Sprintf("refresh-%d", f.exchanges+f.refreshes)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + f.refresh, "expires_at": time.Now().Add(8 * time.Hour),
			"refresh_token": f.refresh, "refresh_expires_at": time.Now().Add(720 * time.Hour),
			"account_id": "acc1", "displayName": "Player",
		})
	})
	mux.HandleFunc("GET /library/api/public/items", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "bearer access-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"records":[
			  {"namespace":"ns1","catalogItemId":"game1","appName":"Fortnite","sandboxType":"PUBLIC"},
			  {"namespace":"ns1","catalogItemId":"dlc1","sandboxType":"PUBLIC"},
			  {"namespace":"ue","catalogItemId":"asset1","sandboxType":"PUBLIC"}],
			  "responseMetadata":{"nextCursor":"p2"}}`)

			return
		}

		fmt.Fprint(w, `{"records":[
		  {"namespace":"ns2","catalogItemId":"game2","sandboxType":"PUBLIC"},
		  {"namespace":"ns2","catalogItemId":"game2","sandboxType":"PUBLIC"},
		  {"namespace":"ns3","catalogItemId":"dev","sandboxType":"PRIVATE"}],
		  "responseMetadata":{}}`)
	})
	mux.HandleFunc("GET /catalog/api/shared/namespace/{ns}/bulk/items", func(w http.ResponseWriter, r *http.Request) {
		items := map[string]string{
			"game1": `{"title":"Control","categories":[{"path":"games"},{"path":"applications"}]}`,
			"dlc1":  `{"title":"Control: AWE","categories":[{"path":"addons"}],"mainGameItem":{"id":"game1"}}`,
			"game2": `{"title":"Alan Wake","categories":[{"path":"games"}]}`,
		}
		id := r.URL.Query().Get("id")
		fmt.Fprintf(w, `{%q:%s}`, id, items[id])
	})

	return mux
}

func newTestProvider(t *testing.T) (*Provider, *fakeEpic) {
	f := &fakeEpic{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	p := NewProvider()
	p.OAuthURL, p.LibraryURL, p.CatalogURL = srv.URL, srv.URL, srv.URL

	return p, f
}

func TestCleanCode(t *testing.T) {
	for in, want := range map[string]string{
		"abc123":     "abc123",
		` "abc123" `: "abc123",
		`{"warning":"Do not share","redirectUrl":"https://localhost/launcher/authorized?code=abc123","authorizationCode":"abc123","sid":null}`: "abc123",
	} {
		if got, err := cleanCode(in); err != nil || got != want {
			t.Errorf("cleanCode(%q) = %q, %v", in, got, err)
		}
	}

	if _, err := cleanCode("483920"); !errors.Is(err, ErrEmailCode) {
		t.Errorf("an emailed sign-in code should be recognized: %v", err)
	}

	if _, err := cleanCode(`{"redirectUrl":"x","authorizationCode":null}`); err == nil {
		t.Error("a null code should fail")
	}
}

func TestPrepareTestAndFetch(t *testing.T) {
	p, f := newTestProvider(t)
	ctx := context.Background()

	// Test, then Save: both prepare the same one-time code, which is exchanged only once.
	pasted := `{"authorizationCode":"good-code"}`

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

	copies, warnings, err := p.Fetch(ctx, settings)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("fetch: %v %v", err, warnings)
	}

	titles := make([]string, 0, len(copies))
	for _, c := range copies {
		titles = append(titles, c.Title+"|"+c.ExternalID+"|"+c.Details.Platform)
	}

	if got := strings.Join(titles, ","); got != "Control|epic:game1|Epic Games,Alan Wake|epic:game2|Epic Games" {
		t.Fatalf("copies: %s", got)
	}

	// A fresh process has no access token cached: Fetch refreshes and rotates the stored session.
	p2 := NewProvider()

	p2.OAuthURL, p2.LibraryURL, p2.CatalogURL = p.OAuthURL, p.LibraryURL, p.CatalogURL
	if _, _, err := p2.Fetch(ctx, settings); err != nil {
		t.Fatal(err)
	}

	if f.refreshes != 1 || !strings.Contains(settings[settingSession], "refresh-2") {
		t.Fatalf("refreshes=%d session=%s", f.refreshes, settings[settingSession])
	}
}

func TestPrepareErrors(t *testing.T) {
	p, _ := newTestProvider(t)

	ctx := context.Background()
	if _, err := p.Prepare(ctx, source.Settings{}); !errors.Is(err, ErrNoSession) {
		t.Errorf("no code and no session: %v", err)
	}

	if _, err := p.Prepare(ctx, source.Settings{settingAuthCode: "wrong"}); !errors.Is(err, ErrBadCode) {
		t.Errorf("wrong code: %v", err)
	}
	// A stored session without a new code is kept as is.
	s, err := p.Prepare(ctx, source.Settings{settingSession: `{"refresh_token":"x"}`})
	if err != nil || s[settingSession] == "" {
		t.Errorf("stored session: %v %v", s, err)
	}
	// An unknown refresh token means the session expired.
	_, _, err = p.Fetch(ctx, source.Settings{settingSession: `{"refresh_token":"stale","account_id":"acc9"}`})
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("stale session: %v", err)
	}
}
