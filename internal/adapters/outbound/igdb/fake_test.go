package igdb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gamevault/internal/domain/schema"
)

// Answers of the real services, with invented values. The error bodies were received on 2026-10-11
// with bogus credentials; the data shapes follow IGDB's API documentation (api-docs.igdb.com).
const (
	// badClient is Twitch's answer to an unknown Client ID (HTTP 400).
	badClient = `{"status":400,"message":"invalid client"}`

	// badSecret is Twitch's answer to a known Client ID with a wrong secret (HTTP 403).
	badSecret = `{"status":403,"message":"invalid client secret"}`

	// authFailure is IGDB's answer to a missing, expired or foreign access token (HTTP 401).
	authFailure = `{
  "message": "Authorization Failure. Have you tried:",
  "Tip 1":   "Ensure you are sending Authorization and Client-ID as headers.",
  "Docs":    "https://api-docs.igdb.com/#authentication"
}`

	// exampleQuest is a full game, as an expanded games query returns it.
	exampleQuest = `{
  "id": 1001,
  "name": "Example Quest",
  "summary": "Example Quest is an invented adventure.",
  "first_release_date": 1303171200,
  "url": "https://www.igdb.com/games/example-quest",
  "cover": {"id": 11, "image_id": "co-example"},
  "genres": [{"id": 31, "name": "Adventure"}, {"id": 9, "name": "Puzzle"}],
  "involved_companies": [
    {"id": 1, "company": {"id": 56, "name": "Invented Studio"}, "developer": true, "publisher": false},
    {"id": 2, "company": {"id": 57, "name": "Invented Publishing"}, "developer": false, "publisher": true}
  ],
  "age_ratings": [
    {"id": 7, "organization": {"id": 1, "name": "ESRB"}, "rating_category": {"id": 4, "rating": "T"}},
    {"id": 8, "organization": {"id": 2, "name": "PEGI"}, "rating_category": {"id": 12, "rating": "Twelve"}}
  ],
  "screenshots": [{"id": 21, "image_id": "sc-one"}, {"id": 22, "image_id": "sc-two"}],
  "videos": [{"id": 31, "name": "Trailer", "video_id": "yt-example"}]
}`
)

// fakeIGDB reproduces Twitch's token endpoint and IGDB's API: tokens it issued work until revoked,
// queries are answered from games keyed by what the body asks for.
type fakeIGDB struct {
	srv *httptest.Server

	mu sync.Mutex

	// tokenCalls counts the access tokens issued.
	tokenCalls int

	// valid holds the access tokens IGDB accepts.
	valid map[string]bool

	// bodies records every query sent to IGDB, by endpoint.
	bodies map[string][]string

	// external answers external_games queries; search answers searches; byID answers "where id".
	external string
	search   string
	byID     string

	// busy makes IGDB answer HTTP 429.
	busy bool
}

func newFakeIGDB(t *testing.T) *fakeIGDB {
	t.Helper()

	f := &fakeIGDB{
		valid:    map[string]bool{},
		bodies:   map[string][]string{},
		external: "[]",
		search:   "[]",
		byID:     "[]",
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeIGDB) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if r.URL.Path == "/oauth2/token" {
		q := r.URL.Query()
		switch {
		case r.Method != http.MethodPost || q.Get("grant_type") != "client_credentials":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":400,"message":"unsupported grant type"}`))
		case q.Get("client_id") != "good-id":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(badClient))
		case q.Get("client_secret") != "good-secret":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(badSecret))
		default:
			f.tokenCalls++
			tok := "token-" + string(rune('0'+f.tokenCalls))
			f.valid[tok] = true
			w.Write([]byte(`{"access_token":"` + tok + `","expires_in":5011271,"token_type":"bearer"}`))
		}

		return
	}

	endpoint := strings.TrimPrefix(r.URL.Path, "/v4/")
	body, _ := io.ReadAll(r.Body)
	f.bodies[endpoint] = append(f.bodies[endpoint], string(body))

	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.Header.Get("Client-ID") != "good-id" || !f.valid[tok] {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(authFailure))

		return
	}

	if f.busy {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"Too Many Requests"}`))

		return
	}

	switch {
	case endpoint == "external_games":
		w.Write([]byte(f.external))
	case strings.Contains(string(body), "search "):
		w.Write([]byte(f.search))
	case strings.Contains(string(body), "where id"):
		w.Write([]byte(f.byID))
	default:
		w.Write([]byte(`[{"id": 1}]`))
	}
}

// revokeTokens makes IGDB refuse every token issued so far, as when one expires early.
func (f *fakeIGDB) revokeTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.valid = map[string]bool{}
}

func (f *fakeIGDB) setBusy(busy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.busy = busy
}

func (f *fakeIGDB) tokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tokenCalls
}

func (f *fakeIGDB) sent(endpoint string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.bodies[endpoint]...)
}

// client returns an IGDB client pointed at the fake, without spacing its requests.
func (f *fakeIGDB) client(now func() time.Time) *Client {
	c := NewClient()
	c.API.BaseURL = f.srv.URL + "/v4"
	c.API.HTTP = f.srv.Client()
	c.Auth.BaseURL = f.srv.URL
	c.Auth.HTTP = f.srv.Client()
	c.limiter = newLimiter(0, 8, now)
	c.now = now

	return c
}

var goodSettings = schema.Settings{
	settingClientID:     "good-id",
	settingClientSecret: "good-secret",
}
