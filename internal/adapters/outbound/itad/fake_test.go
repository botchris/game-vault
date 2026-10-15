package itad

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Answers of the real API, with invented values. The error bodies were received on 2026-10-15 with
// a bogus key; the data shapes follow ITAD's documentation (docs.isthereanydeal.com) of that day.
const (
	// badKey is ITAD's answer to an unknown or revoked key (HTTP 403, not 401).
	badKey = `{"status_code":403,"reason_phrase":"Invalid or expired api key"}`

	// missingKey is ITAD's answer to a request without a key (HTTP 403).
	missingKey = `{"status_code":403,"reason_phrase":"Missing api key"}`

	// featuredPage is a deals list in ITAD's own order: a historical low on Steam, a game with no
	// expiry and no Steam app, a soundtrack (not a game), and a game without art (ITAD sends an empty
	// array for its assets) whose link is not https.
	featuredPage = `{"nextOffset": 4, "hasMore": true, "list": [
  {"id": "018d937f-0000-7000-8000-000000000001", "slug": "lantern-keep", "title": "Lantern Keep", "type": "game", "mature": false,
   "assets": {"boxart": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/boxart.jpg?t=1",
              "banner400": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/banner400.jpg?t=1"},
   "deal": {"shop": {"id": 61, "name": "Steam"},
            "price": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "regular": {"amount": 19.99, "amountInt": 1999, "currency": "EUR"},
            "cut": 75, "voucher": null,
            "storeLow": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "historyLow": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "flag": "H", "drm": [{"id": 61, "name": "Steam"}], "platforms": [{"id": 1, "name": "Windows"}],
            "timestamp": "2026-10-14T15:25:52+02:00", "expiry": "2026-10-20T19:00:00+02:00",
            "url": "https://itad.link/018d9386-0000-7000-8000-00000000aaaa/"}},
  {"id": "018d937f-0000-7000-8000-000000000002", "slug": "echo-valley", "title": "Echo Valley", "type": "game", "mature": false,
   "assets": {"boxart": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000002/boxart.jpg?t=2"},
   "deal": {"shop": {"id": 35, "name": "GOG"},
            "price": {"amount": 2.99, "amountInt": 299, "currency": "EUR"},
            "regular": {"amount": 9.99, "amountInt": 999, "currency": "EUR"},
            "cut": 70, "voucher": null, "storeLow": null,
            "historyLow": {"amount": 2.99, "amountInt": 299, "currency": "EUR"},
            "flag": null, "drm": [], "platforms": [{"id": 1, "name": "Windows"}],
            "timestamp": "2026-10-14T10:00:00+02:00", "expiry": null,
            "url": "https://itad.link/018d9386-0000-7000-8000-00000000bbbb/"}},
  {"id": "018d937f-0000-7000-8000-000000000003", "slug": "harbor-lights-soundtrack", "title": "Harbor Lights Soundtrack", "type": "dlc", "mature": false,
   "assets": {"boxart": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000003/boxart.jpg?t=3"},
   "deal": {"shop": {"id": 61, "name": "Steam"},
            "price": {"amount": 0.99, "amountInt": 99, "currency": "EUR"},
            "regular": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "cut": 80, "voucher": null, "storeLow": null, "historyLow": null, "flag": null, "drm": [], "platforms": [],
            "timestamp": "2026-10-14T10:00:00+02:00", "expiry": null,
            "url": "https://itad.link/018d9386-0000-7000-8000-00000000cccc/"}},
  {"id": "018d937f-0000-7000-8000-000000000004", "slug": "copper-coast", "title": "Copper Coast", "type": "game", "mature": false,
   "assets": [],
   "deal": {"shop": {"id": 16, "name": "Epic Game Store"},
            "price": {"amount": 9.99, "amountInt": 999, "currency": "EUR"},
            "regular": {"amount": 19.99, "amountInt": 1999, "currency": "EUR"},
            "cut": 50, "voucher": null, "storeLow": null,
            "historyLow": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "flag": null, "drm": [], "platforms": [],
            "timestamp": "2026-10-14T10:00:00+02:00", "expiry": "2026-10-16T17:00:00Z",
            "url": "http://itad.link/018d9386-0000-7000-8000-00000000dddd/"}}
]}`

	// steamLookup is the Steam lookup's answer for the three games: a package listed before the app,
	// a game with no Steam entry (null) and one with an empty list.
	steamLookup = `{
  "018d937f-0000-7000-8000-000000000001": ["sub/99", "app/4410"],
  "018d937f-0000-7000-8000-000000000002": null,
  "018d937f-0000-7000-8000-000000000004": []
}`
)

// fakeITAD reproduces the deals list and the Steam lookup, with ITAD's key check.
type fakeITAD struct {
	srv *httptest.Server

	mu sync.Mutex

	// deals answers /deals/v2.
	deals string

	// queries records the query of every deals request; lookups the ids of every lookup.
	queries []url.Values
	lookups [][]string

	// keys records the ITAD-API-Key header of every request; keyInURL that a key came as a
	// query parameter instead.
	keys     []string
	keyInURL bool

	// busy makes ITAD answer 429 with a Retry-After of two minutes.
	busy bool
}

func newFakeITAD(t *testing.T) *fakeITAD {
	t.Helper()

	f := &fakeITAD{deals: featuredPage}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeITAD) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	key := r.Header.Get("ITAD-API-Key")
	f.keys = append(f.keys, key)

	if r.URL.Query().Has("key") {
		f.keyInURL = true
	}

	switch {
	case key == "":
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(missingKey))

		return
	case key != "good-key":
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(badKey))

		return
	case f.busy:
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)

		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/deals/v2":
		f.queries = append(f.queries, r.URL.Query())
		w.Write([]byte(f.deals))
	case r.Method == http.MethodPost && r.URL.Path == "/lookup/shop/61/id/v1":
		var ids []string

		_ = json.NewDecoder(r.Body).Decode(&ids)
		f.lookups = append(f.lookups, ids)

		w.Write([]byte(steamLookup))
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status_code":404,"reason_phrase":"Not Found"}`))
	}
}

func (f *fakeITAD) set(change func(f *fakeITAD)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	change(f)
}

func (f *fakeITAD) sentQueries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]url.Values(nil), f.queries...)
}

func (f *fakeITAD) sentLookups() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([][]string(nil), f.lookups...)
}

// provider returns the deals provider pointed at the fake, without spacing its requests.
func (f *fakeITAD) provider() *Deals {
	d := NewDeals()
	d.API.BaseURL = f.srv.URL
	d.API.HTTP = f.srv.Client()
	d.throttle = nil

	return d
}
