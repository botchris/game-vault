package ubisoft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
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
	p.API.BaseURL = fakeUbisoft(t, full).URL

	return p
}

func titles(t *testing.T, p *Provider, settings schema.Settings) string {
	t.Helper()

	copies, _, err := p.Fetch(context.Background(), settings)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	out := make([]string, 0, len(copies))
	for _, c := range copies {
		out = append(out, c.Title+"|"+c.ExternalID+"|"+c.Details.Platform)
	}

	return strings.Join(out, ",")
}

func TestFetchRotatesTheTicket(t *testing.T) {
	p := newTest(t, true)
	settings := schema.Settings{settingLoginData: pasted}

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

	// The console games linked to the account are left out and counted.
	_, warnings, err := p.Fetch(context.Background(), settings)
	require.NoError(t, err)
	assert.Equal(t, []string{"48 Ubisoft items were not imported: console games, which are not in the Ubisoft Connect library"}, warnings)
}

func TestFallsBackToTheMinimalQuery(t *testing.T) {
	p := newTest(t, false)
	// Without platform groups every game is kept.
	if got := titles(t, p, schema.Settings{settingLoginData: pasted}); !strings.Contains(got, "Far Cry 5") {
		t.Fatalf("copies: %s", got)
	}
}

func TestErrors(t *testing.T) {
	p := newTest(t, true)
	if _, _, err := p.Fetch(context.Background(), schema.Settings{settingLoginData: `{"rememberMeTicket":"revoked"}`}); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("revoked: %v", err)
	}

	if _, _, err := p.Fetch(context.Background(), schema.Settings{settingLoginData: "hello"}); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("garbage: %v", err)
	}
}

func TestFetch_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Ubisoft's gateway rate-limits the sign-in", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.API.BaseURL = srv.URL

		t.Run("WHEN a scan opens a session", func(t *testing.T) {
			_, _, err := p.Fetch(ctx, schema.Settings{settingLoginData: `{"rememberMeTicket":"rm-1"}`})

			t.Run("THEN it is a rate limit, not a signed-out session", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.NotErrorIs(t, err, ErrSignedOut)
			})
		})
	})

	t.Run("GIVEN the library GraphQL is down", func(t *testing.T) {
		var queries int

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v3/profiles/sessions" {
				fmt.Fprint(w, `{"ticket":"tk","sessionId":"sid","rememberMeTicket":"rm-2"}`)
				return
			}

			queries++

			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.API.BaseURL = srv.URL

		t.Run("WHEN a scan reads the library", func(t *testing.T) {
			_, _, err := p.Fetch(ctx, schema.Settings{settingLoginData: `{"rememberMeTicket":"rm-1"}`})

			t.Run("THEN it is an outage, not a signed-out session", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrUnavailable)
				assert.NotErrorIs(t, err, ErrSignedOut)

				t.Run("AND the minimal query is not tried on top", func(t *testing.T) {
					assert.Equal(t, 1, queries)
				})
			})
		})
	})
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
	if _, _, err := p.Fetch(context.Background(), schema.Settings{settingLoginData: onlyRM}); err != nil {
		t.Fatal(err) // "Test connection": settings are thrown away afterwards
	}

	p.sessions = nil // the open session expired
	if _, _, err := p.Fetch(context.Background(), schema.Settings{settingLoginData: onlyRM}); err != nil {
		t.Fatalf("after an unsaved test the rotated ticket should be used: %v", err)
	}
}

// A pasted ticket and session id are used as they are, without spending the remember-me ticket.
func TestPastedSessionIsUsedFirst(t *testing.T) {
	p := newTest(t, true)
	fresh := `{"ticket":"tk","sessionId":"sid","rememberMeTicket":"rm-1"}`

	settings := schema.Settings{settingLoginData: fresh}
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
	p.API.BaseURL = srv.URL
	pasted := `{"rememberMeTicket":"rm-1"}`
	ctx := context.Background()

	if _, _, err := p.Fetch(ctx, schema.Settings{settingLoginData: pasted}); err != nil { // Test: rotates rm-1 → rm-2
		t.Fatal(err)
	}

	saved := schema.Settings{settingLoginData: pasted} // Save stores what was pasted
	if _, _, err := p.Fetch(ctx, saved); err != nil {  // Scan: reuses the open session
		t.Fatal(err)
	}

	if !strings.Contains(saved[settingSession], "rm-2") {
		t.Fatalf("the scan must hand back the rotated ticket to save: %q", saved[settingSession])
	}

	restarted := NewProvider()

	restarted.API.BaseURL = srv.URL
	if _, _, err := restarted.Fetch(ctx, saved); err != nil {
		t.Fatalf("after a restart the saved ticket should work: %v", err)
	}

	if err := restarted.KeepAlive(ctx, saved); err != nil {
		t.Fatalf("keep-alive: %v", err)
	}
}

// TestTest_readsOnePage checks that "Test connection" opens a session and sends the request of a
// scan's first library page, with the scan's page size, and still hands back the rotated ticket.
func TestTest_readsOnePage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a remember-me ticket and a library of two pages", func(t *testing.T) {
		var limits []float64

		inner := fakeUbisoft(t, true).Config.Handler
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/profiles/me/uplay/graphql" {
				raw, _ := io.ReadAll(r.Body)

				var body struct{ Variables map[string]float64 }
				json.Unmarshal(raw, &body)

				limits = append(limits, body.Variables["limit"])
				r.Body = io.NopCloser(bytes.NewReader(raw))
			}

			inner.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.API.BaseURL = srv.URL
		settings := schema.Settings{settingLoginData: `{"rememberMeTicket":"rm-1"}`}

		t.Run("WHEN the source is tested", func(t *testing.T) {
			err := p.Test(ctx, settings)

			t.Run("THEN it works after one library request, the first page of a scan", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, []float64{pageSize}, limits)

				t.Run("AND the rotated ticket is handed back", func(t *testing.T) {
					assert.Contains(t, settings[settingSession], "rm-2")
				})
			})
		})
	})
}

// TestSession_triesEachAppOnce checks that a refused remember-me ticket is offered to each app once:
// the remembered app goes first, and is not tried again in the usual order after it.
func TestSession_triesEachAppOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a saved ticket last renewed by the PC app, which Ubisoft no longer accepts", func(t *testing.T) {
		var apps []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apps = append(apps, r.Header.Get("Ubi-AppId"))

			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.API.BaseURL = srv.URL

		pasted := `{"rememberMeTicket":"rm-1"}`
		saved, err := json.Marshal(state{
			From:             hash(pasted),
			RememberMeTicket: "rm-2",
			AppID:            pcAppID,
		})
		require.NoError(t, err)

		t.Run("WHEN a session is opened", func(t *testing.T) {
			_, err := p.session(ctx, schema.Settings{
				settingLoginData: pasted,
				settingSession:   string(saved),
			}, true)

			t.Run("THEN the user is signed out after one try per app, the remembered one first", func(t *testing.T) {
				require.ErrorIs(t, err, ErrSignedOut)
				assert.Equal(t, []string{pcAppID, webAppID}, apps)
			})
		})
	})
}
