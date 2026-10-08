package ubisoft

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

const pasted = `{"ticket":"old-ticket","sessionId":"old-session","rememberMeTicket":"rm-1","userId":"u1","nameOnPlatform":"player"}`

func fakeUbisoft(t *testing.T, fullQueryWorks bool) *httptest.Server {
	valid := map[string]bool{"rm-1": true}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v3/profiles/sessions", func(w http.ResponseWriter, r *http.Request) {
		rm, _ := strings.CutPrefix(r.Header.Get("Authorization"), "rm_v1 t=")
		if r.Header.Get("Ubi-AppId") == webAppID && !strings.HasPrefix(rm, "web-") {
			w.WriteHeader(http.StatusUnauthorized) // issued by another app
			return
		}

		if !valid[rm] {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"httpCode":401,"message":"The Authorization header is invalid"}`)

			return
		}

		delete(valid, rm)
		valid["rm-2"] = true // Ubisoft rotates the remember-me ticket

		fmt.Fprint(w, `{"ticket":"tk","sessionId":"sid","rememberMeTicket":"rm-2"}`)
	})
	mux.HandleFunc("POST /v1/profiles/me/uplay/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Ubi_v1 t=tk" || r.Header.Get("Ubi-SessionId") != "sid" {
			fmt.Fprint(w, `{"errors":[{"message":"Could not parse authorization header.","extensions":{"code":"INVALID_TICKET"}}]}`)
			return
		}

		var body struct {
			Query     string
			Variables map[string]float64
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.Variables["limit"] > 50 {
			fmt.Fprint(w, `{"errors":[{"message":"Argument 'limit' must be between 0 and 50."}]}`)
			return
		}

		if body.Variables["offset"] > 0 { // second page: the rest of the 52 games
			fmt.Fprint(w, `{"data":{"viewer":{"games":{"totalCount":52,"nodes":[{"id":"g51","spaceId":"s51","name":"Trackmania"},{"id":"g52","spaceId":"s52","name":"Anno 1800"}]}}}}`)
			return
		}

		if strings.Contains(body.Query, "ownedPlatformGroups") && !fullQueryWorks {
			fmt.Fprint(w, `{"errors":[{"message":"Cannot query field \"ownedPlatformGroups\" on type \"GameMeta\"."}]}`)
			return
		}

		if !strings.Contains(body.Query, "ownedPlatformGroups") {
			fmt.Fprint(w, `{"data":{"viewer":{"games":{"totalCount":52,"nodes":[`+filler(47)+`
			  {"id":"g1","spaceId":"s1","name":"Assassin's Creed Valhalla"},{"id":"g2","spaceId":"s2","name":"Far Cry 5"},
			  {"id":"g3","spaceId":"s3","name":"Rayman Legends"}]}}}}`)

			return
		}

		fmt.Fprint(w, `{"data":{"viewer":{"games":{"totalCount":52,"nodes":[`+filler(47)+`
		  {"id":"g1","spaceId":"s1","name":"Assassin's Creed Valhalla","viewer":{"meta":{"ownedPlatformGroups":[{"name":"PC","type":"PC"}]}}},
		  {"id":"g2","spaceId":"s2","name":"Far Cry 5","viewer":{"meta":{"ownedPlatformGroups":[{"name":"PlayStation 4","type":"PS4"}]}}},
		  {"id":"g3","spaceId":"s3","name":"Rayman Legends"}]}}}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// filler returns n console-only games (skipped by the PC filter, kept by the minimal query).
func filler(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, `{"id":"f%d","spaceId":"fs%d","name":"Filler %d","viewer":{"meta":{"ownedPlatformGroups":[{"name":"Xbox One","type":"XBOXONE"}]}}},`, i, i, i)
	}

	return b.String()
}

func newTest(t *testing.T, full bool) *Provider {
	p := NewProvider()
	p.APIURL = fakeUbisoft(t, full).URL

	return p
}

func titles(t *testing.T, p *Provider, settings source.Settings) string {
	t.Helper()

	copies, warnings, err := p.Fetch(context.Background(), settings)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("fetch: %v %v", err, warnings)
	}

	out := make([]string, 0, len(copies))
	for _, c := range copies {
		out = append(out, c.Title+"|"+c.ExternalID+"|"+c.Details.Platform)
	}

	return strings.Join(out, ",")
}

func TestFetchRotatesTheTicket(t *testing.T) {
	p := newTest(t, true)
	settings := source.Settings{settingLoginData: pasted}

	want := "Anno 1800|ubisoft:s52|Ubisoft Connect,Assassin's Creed Valhalla|ubisoft:s1|Ubisoft Connect,Rayman Legends|ubisoft:s3|Ubisoft Connect,Trackmania|ubisoft:s51|Ubisoft Connect"
	if got := titles(t, p, settings); got != want {
		t.Fatalf("copies:\n got %s\nwant %s", got, want)
	}

	if !strings.Contains(settings[settingSession], "rm-2") || !strings.Contains(settings[settingSession], pcAppID) {
		t.Fatalf("the rotated ticket should be kept: %q", settings[settingSession])
	}
	// The next scan uses the rotated ticket (rm-1 is no longer valid).
	if got := titles(t, p, settings); got != want {
		t.Fatalf("second scan: %s", got)
	}
}

func TestFallsBackToTheMinimalQuery(t *testing.T) {
	p := newTest(t, false)
	// Without platform groups every game is kept.
	if got := titles(t, p, source.Settings{settingLoginData: pasted}); !strings.Contains(got, "Far Cry 5") {
		t.Fatalf("copies: %s", got)
	}
}

func TestErrors(t *testing.T) {
	p := newTest(t, true)
	if _, _, err := p.Fetch(context.Background(), source.Settings{settingLoginData: `{"rememberMeTicket":"revoked"}`}); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("revoked: %v", err)
	}

	if _, _, err := p.Fetch(context.Background(), source.Settings{settingLoginData: "hello"}); !errors.Is(err, ErrNoLoginData) {
		t.Fatalf("garbage: %v", err)
	}
}

func TestParseLoginData(t *testing.T) {
	if d := parseLoginData(pasted); d.RememberMeTicket != "rm-1" || d.Ticket != "old-ticket" || d.SessionID != "old-session" {
		t.Fatalf("json: %+v", d)
	}

	bare := strings.Repeat("aB3-_", 20)
	if d := parseLoginData(` "` + bare + `" `); d.RememberMeTicket != bare {
		t.Fatalf("bare ticket: %+v", d)
	}

	if d := parseLoginData("hello"); d != (loginData{}) {
		t.Fatalf("garbage: %+v", d)
	}
}

// Test then Save then Scan of an unsaved source: the ticket rotated by the test must be reused,
// not the pasted one (which Ubisoft has revoked by then).
func TestRotationSurvivesAnUnsavedTest(t *testing.T) {
	p := newTest(t, true)

	onlyRM := `{"rememberMeTicket":"rm-1"}`
	if _, _, err := p.Fetch(context.Background(), source.Settings{settingLoginData: onlyRM}); err != nil {
		t.Fatal(err) // "Test connection": settings are thrown away afterwards
	}

	p.sessions = nil // the open session expired
	if _, _, err := p.Fetch(context.Background(), source.Settings{settingLoginData: onlyRM}); err != nil {
		t.Fatalf("after an unsaved test the rotated ticket should be used: %v", err)
	}
}

// A pasted ticket and session id are used as they are, without spending the remember-me ticket.
func TestPastedSessionIsUsedFirst(t *testing.T) {
	p := newTest(t, true)
	fresh := `{"ticket":"tk","sessionId":"sid","rememberMeTicket":"rm-1"}`

	settings := source.Settings{settingLoginData: fresh}
	if _, _, err := p.Fetch(context.Background(), settings); err != nil {
		t.Fatal(err)
	}

	if settings[settingSession] != "" {
		t.Fatalf("the remember-me ticket should not have been renewed: %q", settings[settingSession])
	}
}

// The scenario that lost the ticket: Test (unsaved) → Save → Scan → restart → Scan.
func TestTicketSurvivesTestSaveScanRestart(t *testing.T) {
	srv := fakeUbisoft(t, true)
	p := NewProvider()
	p.APIURL = srv.URL
	pasted := `{"rememberMeTicket":"rm-1"}`
	ctx := context.Background()

	if _, _, err := p.Fetch(ctx, source.Settings{settingLoginData: pasted}); err != nil { // Test: rotates rm-1 → rm-2
		t.Fatal(err)
	}

	saved := source.Settings{settingLoginData: pasted} // Save stores what was pasted
	if _, _, err := p.Fetch(ctx, saved); err != nil {  // Scan: reuses the open session
		t.Fatal(err)
	}

	if !strings.Contains(saved[settingSession], "rm-2") {
		t.Fatalf("the scan must hand back the rotated ticket to save: %q", saved[settingSession])
	}

	restarted := NewProvider()

	restarted.APIURL = srv.URL
	if _, _, err := restarted.Fetch(ctx, saved); err != nil {
		t.Fatalf("after a restart the saved ticket should work: %v", err)
	}

	if err := restarted.KeepAlive(ctx, saved); err != nil {
		t.Fatalf("keep-alive: %v", err)
	}
}
