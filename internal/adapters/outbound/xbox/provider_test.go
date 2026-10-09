package xbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// fakeMicrosoft mimics login.live.com, Xbox Live auth, the Store collections and the catalog.
type fakeMicrosoft struct {
	refresh   string
	exchanges int
	pages     int
}

func (f *fakeMicrosoft) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth20_token.srf", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()

		if r.Form.Get("client_id") != clientID || r.Form.Get("scope") != scope {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
			return
		}

		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "M.C5_good" || f.exchanges > 0 {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}

			f.exchanges++
		case "refresh_token":
			if r.Form.Get("refresh_token") != f.refresh {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
		}

		f.refresh += "+"
		fmt.Fprintf(w, `{"access_token":"live-access","refresh_token":%q,"expires_in":3600,"user_id":"u1"}`, f.refresh)
	})
	mux.HandleFunc("POST /user/authenticate", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Properties struct{ RpsTicket string } }
		json.NewDecoder(r.Body).Decode(&body)

		if body.Properties.RpsTicket != "t=live-access" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		fmt.Fprint(w, `{"Token":"user-token","DisplayClaims":{"xui":[{"uhs":"hash"}]}}`)
	})
	mux.HandleFunc("POST /xsts/authorize", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RelyingParty string
			Properties   struct{ UserTokens []string }
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.RelyingParty != "http://mp.microsoft.com/" || body.Properties.UserTokens[0] != "user-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		fmt.Fprint(w, `{"Token":"store-token","DisplayClaims":{"xui":[{"uhs":"hash"}]}}`)
	})
	mux.HandleFunc("POST /collections", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "XBL3.0 x=hash;store-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var body struct {
			ContinuationToken string
			Beneficiaries     *[]any
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.Beneficiaries == nil { // what the real service answers
			http.Error(w, `{"errors":{"beneficiaries":["Required property 'beneficiaries' not found in JSON."]}}`, http.StatusBadRequest)
			return
		}

		f.pages++

		if body.ContinuationToken == "" {
			fmt.Fprint(w, `{"items":[
			  {"productId":"9FORZA","productKind":"Game","status":"Active","acquisitionType":"Single","skuType":"Full"},
			  {"productId":"9PASS","productKind":"Game","status":"Active","acquisitionType":"Recurring","skuType":"Full"},
			  {"productId":"9TRIAL","productKind":"Game","status":"Active","skuType":"Trial"}],"continuationToken":"p2"}`)

			return
		}

		fmt.Fprint(w, `{"items":[
		  {"productId":"9HALO","productKind":"Game","status":"Active","acquisitionType":"Single"},
		  {"productId":"9DLC","productKind":"Durable","status":"Active","acquisitionType":"Single"},
		  {"productId":"9GONE","productKind":"Game","status":"Revoked","acquisitionType":"Single"}]}`)
	})
	mux.HandleFunc("GET /catalog", func(w http.ResponseWriter, r *http.Request) {
		titles := map[string]string{"9FORZA": "Forza Horizon 5 Standard Edition", "9HALO": "Halo Infinite"}

		var products []string

		for _, id := range strings.Split(r.URL.Query().Get("bigIds"), ",") {
			if title, ok := titles[id]; ok {
				products = append(products, fmt.Sprintf(`{"ProductId":%q,"ProductKind":"Game","LocalizedProperties":[{"ProductTitle":%q,
				  "Images":[{"ImagePurpose":"BoxArt","Uri":"//store-images.s-microsoft.com/%s-box"},{"ImagePurpose":"Poster","Uri":"//store-images.s-microsoft.com/%s-poster"}]}]}`, id, title, id, id))
			}
		}

		fmt.Fprintf(w, `{"Products":[%s]}`, strings.Join(products, ","))
	})

	return mux
}

func newTest(t *testing.T) (*Provider, *fakeMicrosoft) {
	f := &fakeMicrosoft{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	p := NewProvider()
	p.LiveURL, p.UserAuthURL, p.XSTSURL = srv.URL, srv.URL+"/user/authenticate", srv.URL+"/xsts/authorize"
	p.CollectionsURL, p.CatalogURL = srv.URL+"/collections", srv.URL+"/catalog"

	return p, f
}

func TestCleanCode(t *testing.T) {
	for in, want := range map[string]string{
		"https://login.live.com/oauth20_desktop.srf?code=M.C5_abc&lc=1034": "M.C5_abc",
		"M.C5_abc": "M.C5_abc",
	} {
		if got := cleanCode(in); got != want {
			t.Errorf("cleanCode(%q) = %q", in, got)
		}
	}
}

func TestPrepareAndFetch(t *testing.T) {
	p, f := newTest(t)
	ctx := context.Background()
	pasted := "https://login.live.com/oauth20_desktop.srf?code=M.C5_good&lc=1034"

	settings, err := p.Prepare(ctx, source.Settings{settingCode: pasted})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.Prepare(ctx, source.Settings{settingCode: pasted}); err != nil || f.exchanges != 1 {
		t.Fatalf("test then save should share the code: %v (%d exchanges)", err, f.exchanges)
	}

	copies, warnings, err := p.Fetch(ctx, settings)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("fetch: %v %v", err, warnings)
	}

	got := make([]string, 0, len(copies))
	for _, c := range copies {
		got = append(got, c.Title+"|"+c.ExternalID+"|"+c.Details.Platform)
	}

	want := "Forza Horizon 5 Standard Edition|xbox:9FORZA|Microsoft Store / Xbox,Halo Infinite|xbox:9HALO|Microsoft Store / Xbox"
	if s := strings.Join(got, ","); s != want {
		t.Fatalf("copies (no Game Pass, trials, DLC or revoked):\n got %s\nwant %s", s, want)
	}

	if f.pages != 2 {
		t.Fatalf("both collection pages should be read, got %d", f.pages)
	}

	// A new process refreshes the session, which rotates the refresh token.
	p2, _ := newTest(t)
	p2.LiveURL, p2.UserAuthURL, p2.XSTSURL, p2.CollectionsURL, p2.CatalogURL = p.LiveURL, p.UserAuthURL, p.XSTSURL, p.CollectionsURL, p.CatalogURL

	before := settings[settingSession]
	if _, _, err := p2.Fetch(ctx, settings); err != nil {
		t.Fatal(err)
	}

	if settings[settingSession] == before {
		t.Fatal("the rotated refresh token should be written back")
	}
}

func TestErrors(t *testing.T) {
	p, _ := newTest(t)

	ctx := context.Background()
	if _, err := p.Prepare(ctx, source.Settings{}); !errors.Is(err, ErrNoSession) {
		t.Errorf("no code: %v", err)
	}

	if _, err := p.Prepare(ctx, source.Settings{settingCode: "M.C5_wrong"}); !errors.Is(err, ErrBadCode) {
		t.Errorf("wrong code: %v", err)
	}

	if _, _, err := p.Fetch(ctx, source.Settings{settingSession: `{"refresh_token":"stale"}`}); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("stale session: %v", err)
	}
}

func TestCovers(t *testing.T) {
	p, _ := newTest(t)
	c := NewCovers()
	c.CatalogURL = p.CatalogURL

	q := media.CoverQuery{
		Title: "Halo Infinite",
		Links: game.Links{"xbox": "9HALO"},
	}
	if !c.Applies(q) || c.Applies(media.CoverQuery{
		Title: "x",
		Links: game.Links{game.LinkSteam: "1"},
	}) {
		t.Fatal("applies only to games imported from a Microsoft account")
	}

	got, err := c.Covers(context.Background(), q, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].URL != "https://store-images.s-microsoft.com/9HALO-poster" {
		t.Fatalf("candidates: %+v", got)
	}
}
