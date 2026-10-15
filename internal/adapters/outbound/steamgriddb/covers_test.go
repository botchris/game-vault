package steamgriddb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// Answers of SteamGridDB's API v2, with invented values. The error bodies were received on
// 2026-10-11 with a bogus key and without one; the data shapes follow the API documentation
// (https://www.steamgriddb.com/static/openapi.yml, version 2.10.0) and the official npm wrapper.
const (
	// invalidKey is the answer to a bogus key (HTTP 401).
	invalidKey = `{"success":false,"errors":["Invalid API key"]}`

	// gameNotFound is the answer for a game it does not know (HTTP 404).
	gameNotFound = `{"success":false,"errors":["Game not found"]}`

	// exampleSearch has one game named "Example Quest" and a sequel.
	exampleSearch = `{"success":true,"data":[
  {"id":5001,"name":"Example Quest","types":["steam"],"verified":true,"release_date":1303171200},
  {"id":5002,"name":"Example Quest 2","types":["steam"],"verified":true}
]}`

	// ambiguousSearch has two different games with the same name (an original and its remake).
	ambiguousSearch = `{"success":true,"data":[
  {"id":6001,"name":"Example Raid","types":[],"verified":true},
  {"id":6002,"name":"Example Raid™","types":["steam"],"verified":true}
]}`

	// exampleGrids has portrait grids with different scores and a landscape one the filter let through.
	exampleGrids = `{"success":true,"page":0,"total":4,"limit":50,"data":[
  {"id":11,"score":2,"style":"alternate","width":600,"height":900,"nsfw":false,"humor":false,"epilepsy":false,"notes":null,"mime":"image/png","language":"en","url":"https://cdn2.steamgriddb.com/grid/low.png","thumb":"https://cdn2.steamgriddb.com/thumb/low.jpg","lock":false,"upvotes":2,"downvotes":0,"author":{"name":"Invented Artist","steam64":"0","avatar":"https://example.invalid/a.jpg"}},
  {"id":12,"score":9,"style":"alternate","width":600,"height":900,"nsfw":false,"humor":false,"epilepsy":false,"url":"https://cdn2.steamgriddb.com/grid/best.png","thumb":"https://cdn2.steamgriddb.com/thumb/best.jpg","author":{"name":"Other Artist"}},
  {"id":13,"score":5,"style":"material","width":600,"height":900,"nsfw":false,"humor":false,"epilepsy":false,"url":"https://cdn2.steamgriddb.com/grid/mid.png","thumb":"","author":{"name":""}},
  {"id":14,"score":99,"style":"alternate","width":920,"height":430,"nsfw":false,"humor":false,"epilepsy":false,"url":"https://cdn2.steamgriddb.com/grid/wide.png","thumb":"https://cdn2.steamgriddb.com/thumb/wide.jpg","author":{"name":"Wide Artist"}},
  {"id":15,"score":1,"style":"alternate","width":600,"height":900,"nsfw":false,"humor":false,"epilepsy":false,"url":"https://cdn2.steamgriddb.com/grid/fourth.png","thumb":"https://cdn2.steamgriddb.com/thumb/fourth.jpg","author":{"name":"Fourth Artist"}}
]}`

	// noGrids is a known game without grids of that size.
	noGrids = `{"success":true,"page":0,"total":0,"limit":50,"data":[]}`
)

// fakeSGDB reproduces SteamGridDB's API: answers by path, and the key it accepts.
type fakeSGDB struct {
	srv *httptest.Server

	mu sync.Mutex

	// answers maps a path to its status and body; unknown paths answer 404 gameNotFound.
	answers map[string]fakeAnswer

	// requests records each request's path and query.
	requests []string
}

type fakeAnswer struct {
	status int
	body   string
}

func newFake(t *testing.T) *fakeSGDB {
	f := &fakeSGDB{answers: map[string]fakeAnswer{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		a, ok := f.answers[r.URL.Path]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Header.Get("Authorization") == "":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"success":false,"errors":["Authentication Required"]}`))
		case r.Header.Get("Authorization") != "Bearer good-key":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(invalidKey))
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(gameNotFound))
		default:
			if strings.HasPrefix(a.body, "<") {
				w.Header().Set("Content-Type", "text/html")
			}

			w.WriteHeader(a.status)
			w.Write([]byte(a.body))
		}
	}))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeSGDB) answer(path string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.answers[path] = fakeAnswer{
		status: status,
		body:   body,
	}
}

func (f *fakeSGDB) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.requests...)
}

// covers returns the provider on the fake server, without the throttle's waits.
func (f *fakeSGDB) covers() *Covers {
	api := apiclient.New(f.srv.URL)
	api.HTTP = f.srv.Client()

	return &Covers{API: api}
}

var goodSettings = schema.Settings{
	settingConsent: schema.ConsentGiven,
	settingAPIKey:  "good-key",
}

func TestCovers_applies(t *testing.T) {
	p := NewCovers()

	for _, tc := range []struct {
		name string
		q    media.GameQuery
		want bool
	}{
		{"a PC edition, first pass", media.GameQuery{System: game.SystemPC}, false},
		{"a game without copies, first pass", media.GameQuery{}, false},
		{"a PS2 edition, first pass", media.GameQuery{System: "PS2"}, false},
		{"a PC edition on the fallback pass", media.GameQuery{
			System:   game.SystemPC,
			Fallback: true,
		}, true},
		{"a PS2 edition on the fallback pass", media.GameQuery{
			System:   "PS2",
			Fallback: true,
		}, true},
		{"a virtual PC edition (store links only)", media.GameQuery{
			System:    game.SystemPC,
			Links:     game.Links{game.LinkSteam: "1"},
			Fallback:  true,
			LinksOnly: true,
		}, false},
	} {
		assert.Equal(t, tc.want, p.Applies(tc.q), tc.name)
	}
}

func TestCovers_descriptor(t *testing.T) {
	t.Run("GIVEN the SteamGridDB cover provider", func(t *testing.T) {
		d := NewCovers().Descriptor()

		t.Run("THEN it starts disabled and asks for a ticked consent before the key", func(t *testing.T) {
			assert.False(t, d.EnabledByDefault)
			require.Len(t, d.Fields, 2)
			assert.Equal(t, schema.FieldConsent, d.Fields[0].Kind)
			assert.True(t, d.Fields[0].Required)
			assert.Equal(t, TermsURL, d.Fields[0].HelpURL)
			assert.Equal(t, schema.FieldSecret, d.Fields[1].Kind)
			assert.True(t, d.Fields[1].Required)
		})

		t.Run("THEN it cannot be enabled without the consent, even with a key", func(t *testing.T) {
			err := d.Fields.Validate(schema.Settings{settingAPIKey: "good-key"}, d.Name)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "accept the risk")
		})
	})
}

func TestCovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	steamGame := media.GameQuery{
		Title:    "Example Quest",
		System:   game.SystemPC,
		Links:    game.Links{game.LinkSteam: "620"},
		Fallback: true,
	}
	discGame := media.GameQuery{
		Title:    "Example Quest [PS2]",
		System:   "PS2",
		Fallback: true,
	}

	t.Run("GIVEN a game linked to Steam that SteamGridDB has grids for", func(t *testing.T) {
		f := newFake(t)
		f.answer("/grids/steam/620", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, steamGame, goodSettings)
			require.NoError(t, err)

			t.Run("THEN the three best portrait grids come first, labeled as community art", func(t *testing.T) {
				require.Len(t, got, 3)
				assert.Equal(t, "https://cdn2.steamgriddb.com/grid/best.png", got[0].URL)
				assert.Equal(t, "https://cdn2.steamgriddb.com/thumb/best.jpg", got[0].ThumbURL)
				assert.Equal(t, "SteamGridDB: community art by Other Artist", got[0].Label)
				assert.Equal(t, ID, got[0].Provider)
				assert.Equal(t, "https://cdn2.steamgriddb.com/grid/mid.png", got[1].URL)
				assert.Equal(t, "https://cdn2.steamgriddb.com/grid/mid.png", got[1].ThumbURL, "no thumbnail: the image itself")
				assert.Equal(t, "SteamGridDB: community art", got[1].Label)
				assert.Equal(t, "https://cdn2.steamgriddb.com/grid/low.png", got[2].URL)
			})

			t.Run("AND it asked once, by AppID, for static portrait grids without tagged images", func(t *testing.T) {
				sent := f.sent()
				require.Len(t, sent, 1)
				assert.True(t, strings.HasPrefix(sent[0], "/grids/steam/620?"), sent[0])

				for _, param := range []string{"dimensions=600x900", "types=static", "nsfw=false", "humor=false", "epilepsy=false"} {
					assert.Contains(t, sent[0], param)
				}
			})

			t.Run("AND its images come from the declared host", func(t *testing.T) {
				assert.Contains(t, f.covers().ImageHosts(), "cdn2.steamgriddb.com")
			})
		})
	})

	t.Run("GIVEN a game linked to Steam that SteamGridDB knows without grids", func(t *testing.T) {
		f := newFake(t)
		f.answer("/grids/steam/620", http.StatusOK, noGrids)
		f.answer("/search/autocomplete/Example Quest", http.StatusOK, exampleSearch)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, steamGame, goodSettings)

			t.Run("THEN there is no candidate, and no title search for the same game", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, got)
				assert.Len(t, f.sent(), 1)
			})
		})
	})

	t.Run("GIVEN a game linked to Steam whose AppID SteamGridDB does not know", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example Quest", http.StatusOK, exampleSearch)
		f.answer("/grids/game/5001", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, steamGame, goodSettings)
			require.NoError(t, err)

			t.Run("THEN its title is searched instead", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "Example Quest", got[0].Title)
				assert.Len(t, f.sent(), 3)
			})
		})
	})

	t.Run("GIVEN a disc whose game SteamGridDB knows under exactly its title", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example Quest", http.StatusOK, exampleSearch)
		f.answer("/grids/game/5001", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, discGame, goodSettings)
			require.NoError(t, err)

			t.Run("THEN the title without its platform tag finds the game, not its sequel", func(t *testing.T) {
				require.Len(t, got, 3)
				assert.Equal(t, "Example Quest", got[0].Title)

				sent := f.sent()
				require.Len(t, sent, 2)
				assert.Equal(t, "/search/autocomplete/Example%20Quest?", sent[0])
				assert.True(t, strings.HasPrefix(sent[1], "/grids/game/5001?"), sent[1])
			})
		})
	})

	t.Run("GIVEN titles whose edition words or year name another game than the original", func(t *testing.T) {
		f := newFake(t)
		// The edition words and the year are dropped from the first search, so it lists the
		// original; the raw title's search lists it too, and nothing else.
		f.answer("/search/autocomplete/Example", http.StatusOK, exampleSearch)
		f.answer("/search/autocomplete/Example Remastered", http.StatusOK, exampleSearch)
		f.answer("/search/autocomplete/Doom", http.StatusOK, `{"success":true,"data":[{"id":8001,"name":"Doom","types":["steam"],"verified":true}]}`)
		f.answer("/search/autocomplete/Doom (2016)", http.StatusOK, `{"success":true,"data":[{"id":8001,"name":"Doom","types":["steam"],"verified":true}]}`)
		f.answer("/grids/game/5001", http.StatusOK, exampleGrids)
		f.answer("/grids/game/8001", http.StatusOK, exampleGrids)

		for _, title := range []string{"Example Remastered", "Doom (2016)"} {
			t.Run("WHEN the covers of "+title+" are asked", func(t *testing.T) {
				got, err := f.covers().Covers(ctx, media.GameQuery{
					Title:    title,
					Fallback: true,
				}, goodSettings)

				t.Run("THEN the original's art is not proposed: it is another game's box", func(t *testing.T) {
					require.NoError(t, err)
					assert.Empty(t, got)
				})
			})
		}
	})

	t.Run("GIVEN a remaster only the raw title's search finds", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example", http.StatusOK, exampleSearch)
		f.answer("/search/autocomplete/Example Remastered", http.StatusOK, `{"success":true,"data":[
  {"id":7001,"name":"Example Remastered","types":["steam"],"verified":true},
  {"id":5001,"name":"Example Quest","types":["steam"],"verified":true}
]}`)
		f.answer("/grids/game/7001", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, media.GameQuery{
				Title:    "Example Remastered",
				Fallback: true,
			}, goodSettings)
			require.NoError(t, err)

			t.Run("THEN the second search, with the raw title, finds the remaster", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "Example Remastered", got[0].Title)

				sent := f.sent()
				require.Len(t, sent, 3)
				assert.Equal(t, "/search/autocomplete/Example?", sent[0])
				assert.Equal(t, "/search/autocomplete/Example%20Remastered?", sent[1])
				assert.True(t, strings.HasPrefix(sent[2], "/grids/game/7001?"), sent[2])
			})
		})
	})

	t.Run("GIVEN a title with ' - ' where SteamGridDB names the game with ': '", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example - A Wild Adventure", http.StatusOK, `{"success":true,"data":[
  {"id":9001,"name":"Example: A Wild Adventure","types":["steam"],"verified":true},
  {"id":9002,"name":"Example: A Wild Adventure 2","types":[],"verified":false}
]}`)
		f.answer("/grids/game/9001", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, media.GameQuery{
				Title:    "Example - A Wild Adventure",
				System:   game.SystemPC,
				Fallback: true,
			}, goodSettings)
			require.NoError(t, err)

			t.Run("THEN the punctuation does not matter, every word still does", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "Example: A Wild Adventure", got[0].Title)
			})
		})
	})

	t.Run("GIVEN a key whose title carries its store tag", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example Quest", http.StatusOK, exampleSearch)
		f.answer("/grids/game/5001", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, media.GameQuery{
				Title:    "Example Quest (Steam Key)",
				Fallback: true,
			}, goodSettings)
			require.NoError(t, err)

			t.Run("THEN the game is found: a store tag is not part of its name", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "Example Quest", got[0].Title)
			})
		})
	})

	t.Run("GIVEN a search that lists the same game twice", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example Quest", http.StatusOK, `{"success":true,"data":[
  {"id":5001,"name":"Example Quest","types":["steam"],"verified":true},
  {"id":5001,"name":"Example Quest","types":["gog"],"verified":true}
]}`)
		f.answer("/grids/game/5001", http.StatusOK, exampleGrids)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, media.GameQuery{
				Title:    "Example Quest",
				Fallback: true,
			}, goodSettings)

			t.Run("THEN it is one game, not an ambiguous search", func(t *testing.T) {
				require.NoError(t, err)
				assert.NotEmpty(t, got)
			})
		})
	})

	t.Run("GIVEN a title SteamGridDB has two games for", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example Raid", http.StatusOK, ambiguousSearch)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := f.covers().Covers(ctx, media.GameQuery{
				Title:    "Example Raid",
				Fallback: true,
			}, goodSettings)

			t.Run("THEN nothing is proposed: a wrong box is worse than none", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, got)
				assert.Len(t, f.sent(), 1)
			})
		})
	})

	t.Run("GIVEN a title SteamGridDB only has similar names for, or none", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/Example", http.StatusOK, exampleSearch)
		f.answer("/search/autocomplete/Nothing Like It", http.StatusOK, `{"success":true,"data":[]}`)

		for _, title := range []string{"Example", "Nothing Like It"} {
			t.Run("WHEN the covers of "+title+" are asked", func(t *testing.T) {
				got, err := f.covers().Covers(ctx, media.GameQuery{
					Title:    title,
					Fallback: true,
				}, goodSettings)

				t.Run("THEN there is no candidate and no error", func(t *testing.T) {
					require.NoError(t, err)
					assert.Empty(t, got)
				})
			})
		}
	})

	t.Run("GIVEN settings without the consent or the key", func(t *testing.T) {
		f := newFake(t)

		for name, s := range map[string]schema.Settings{
			"no consent": {settingAPIKey: "good-key"},
			"no key":     {settingConsent: schema.ConsentGiven},
			"nothing":    nil,
		} {
			t.Run("WHEN covers are asked with "+name, func(t *testing.T) {
				_, err := f.covers().Covers(ctx, steamGame, s)

				t.Run("THEN it is a settings problem, and nothing is sent", func(t *testing.T) {
					require.ErrorIs(t, err, ErrNotConfigured)
					require.ErrorIs(t, err, provider.ErrBadSettings)
					assert.Empty(t, f.sent())
				})
			})
		}
	})
}

func TestCovers_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{
		Title:    "Example Quest",
		Links:    game.Links{game.LinkSteam: "620"},
		Fallback: true,
	}

	t.Run("GIVEN a bogus API key", func(t *testing.T) {
		f := newFake(t)
		bad := schema.Settings{
			settingConsent: schema.ConsentGiven,
			settingAPIKey:  "bogus",
		}

		t.Run("WHEN covers are asked", func(t *testing.T) {
			_, err := f.covers().Covers(ctx, q, bad)

			t.Run("THEN it is a settings problem that says to check the key", func(t *testing.T) {
				require.ErrorIs(t, err, ErrBadKey)
				require.ErrorIs(t, err, provider.ErrBadSettings)
			})
		})

		t.Run("WHEN covers are asked again with the same provider", func(t *testing.T) {
			p := f.covers()
			_, err := p.Covers(ctx, q, bad)
			require.ErrorIs(t, err, ErrBadKey)

			before := len(f.sent())
			_, err = p.Covers(ctx, q, bad)

			t.Run("THEN the rejected key is not sent again", func(t *testing.T) {
				require.ErrorIs(t, err, ErrBadKey)
				assert.Len(t, f.sent(), before)
			})

			t.Run("AND another key is tried at once", func(t *testing.T) {
				f.answer("/grids/steam/620", http.StatusOK, noGrids)

				_, err := p.Covers(ctx, q, goodSettings)
				require.NoError(t, err)
				assert.Len(t, f.sent(), before+1)
			})

			t.Run("AND the rejected key is tried again after a long back-off", func(t *testing.T) {
				p.now = func() time.Time { return time.Now().Add(keyBackoff + time.Minute) }

				_, err := p.Covers(ctx, q, bad)
				require.ErrorIs(t, err, ErrBadKey)
				assert.Len(t, f.sent(), before+2)
			})
		})

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := f.covers().Test(ctx, bad)

			t.Run("THEN it says SteamGridDB rejected the key", func(t *testing.T) {
				require.ErrorIs(t, err, ErrBadKey)
				assert.Equal(t, "the service rejected the API key: check it in the provider settings", err.Error())
			})
		})
	})

	t.Run("GIVEN a key SteamGridDB rejected earlier", func(t *testing.T) {
		f := newFake(t)
		p := f.covers()
		bad := schema.Settings{
			settingConsent: schema.ConsentGiven,
			settingAPIKey:  "bogus",
		}
		_, err := p.Covers(ctx, q, bad)
		require.ErrorIs(t, err, ErrBadKey)

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			before := len(f.sent())
			err := p.Test(ctx, bad)

			t.Run("THEN Test still asks SteamGridDB: the user wants a fresh answer", func(t *testing.T) {
				require.ErrorIs(t, err, ErrBadKey)
				assert.Len(t, f.sent(), before+1)
			})
		})
	})

	t.Run("GIVEN a search path that answers 404", func(t *testing.T) {
		for name, body := range map[string]string{
			"with the API's JSON": gameNotFound,
			"with a web page":     `<html><body>Not Found</body></html>`,
		} {
			t.Run("WHEN the connection is tested and the answer is "+name, func(t *testing.T) {
				f := newFake(t)
				f.answer("/search/autocomplete/example", http.StatusNotFound, body)
				err := f.covers().Test(ctx, goodSettings)

				t.Run("THEN only the API's own not-found answer counts as working", func(t *testing.T) {
					if body == gameNotFound {
						assert.NoError(t, err)
						return
					}

					assert.ErrorIs(t, err, provider.ErrUnavailable)
				})
			})
		}
	})

	t.Run("GIVEN a good API key", func(t *testing.T) {
		f := newFake(t)
		f.answer("/search/autocomplete/example", http.StatusOK, `{"success":true,"data":[]}`)

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := f.covers().Test(ctx, goodSettings)

			t.Run("THEN it works, with one request", func(t *testing.T) {
				require.NoError(t, err)
				assert.Len(t, f.sent(), 1)
			})
		})
	})

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"rate-limits it (HTTP 429)":            {http.StatusTooManyRequests, `{"success":false,"errors":["Too many requests"]}`},
		"is down (HTTP 502)":                   {http.StatusBadGateway, `<html><body>Bad gateway</body></html>`},
		"is down (HTTP 503)":                   {http.StatusServiceUnavailable, `{"success":false,"errors":["Maintenance"]}`},
		"answers a bot check (HTTP 200)":       {http.StatusOK, `<!DOCTYPE html><html><title>Just a moment...</title></html>`},
		"answers a forbidden key (HTTP 403)":   {http.StatusForbidden, `{"success":false,"errors":["Forbidden"]}`},
		"answers a Cloudflare page (HTTP 403)": {http.StatusForbidden, `<!DOCTYPE html><html><title>Attention Required! | Cloudflare</title></html>`},
		"answers a Cloudflare page (HTTP 401)": {http.StatusUnauthorized, `<html><body>Unauthorized</body></html>`},
	} {
		t.Run("GIVEN SteamGridDB "+name, func(t *testing.T) {
			f := newFake(t)
			f.answer("/grids/steam/620", tc.status, tc.body)

			t.Run("WHEN covers are asked", func(t *testing.T) {
				_, err := f.covers().Covers(ctx, q, goodSettings)
				require.Error(t, err)

				t.Run("THEN it is a provider-level problem, so the edition is not marked missing", func(t *testing.T) {
					switch {
					case !strings.HasPrefix(tc.body, "<") && tc.status == http.StatusForbidden:
						assert.ErrorIs(t, err, provider.ErrBadSettings)
					case tc.status == http.StatusTooManyRequests:
						assert.ErrorIs(t, err, provider.ErrRateLimited)
						assert.NotErrorIs(t, err, provider.ErrUnavailable)
					default: // 5xx and web pages: the service, not the key or the game
						assert.ErrorIs(t, err, provider.ErrUnavailable)
						assert.NotErrorIs(t, err, provider.ErrBadSettings)
					}
				})

				t.Run("AND its text does not name SteamGridDB, which the caller puts before it", func(t *testing.T) {
					assert.NotContains(t, err.Error(), "SteamGridDB")
				})
			})
		})
	}

	t.Run("GIVEN SteamGridDB stops answering", func(t *testing.T) {
		f := newFake(t)
		p := f.covers()
		f.srv.Close()

		t.Run("WHEN covers are asked", func(t *testing.T) {
			_, err := p.Covers(ctx, q, goodSettings)

			t.Run("THEN it counts as unavailable", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrUnavailable)
				assert.Contains(t, err.Error(), "try again later")
				assert.NotContains(t, err.Error(), "SteamGridDB")
			})
		})
	})

	for name, body := range map[string]string{
		"a malformed body":         `{"success":true,"data":[{"id":`,
		"data of another shape":    `{"success":true,"data":{"id":1}}`,
		"success false with a 200": `{"success":false,"errors":["Something odd"]}`,
	} {
		t.Run("GIVEN SteamGridDB answers "+name, func(t *testing.T) {
			f := newFake(t)
			f.answer("/grids/steam/620", http.StatusOK, body)

			t.Run("WHEN covers are asked", func(t *testing.T) {
				got, err := f.covers().Covers(ctx, q, goodSettings)

				t.Run("THEN the service is unavailable (nothing is said about the game), with a message to report", func(t *testing.T) {
					require.Error(t, err)
					assert.Empty(t, got)
					assert.ErrorIs(t, err, provider.ErrUnavailable)
					assert.NotErrorIs(t, err, provider.ErrBadSettings)
					assert.Contains(t, err.Error(), "report this message")
				})
			})
		})
	}
}

func TestWithoutStoreTags(t *testing.T) {
	for title, want := range map[string]string{
		"Example Quest (Steam Key)":             "Example Quest",
		"Example Quest - GOG Key":               "Example Quest",
		"Example Quest [PS2]":                   "Example Quest",
		"Example Quest (PC/Mac)":                "Example Quest",
		"Example Quest™ Steam CD Key":           "Example Quest",
		"Example Remastered":                    "Example Remastered",
		"Doom (2016)":                           "Doom (2016)",
		"Super Metroid [SNES]":                  "Super Metroid",
		"Gunstar Heroes (Mega Drive / Genesis)": "Gunstar Heroes",
		"Example: Definitive Edition":           "Example: Definitive Edition",
		"Example Quest (Director's Cut)":        "Example Quest (Director's Cut)",
		"Example Quest (Gold) Steam Key":        "Example Quest (Gold)",
	} {
		assert.Equal(t, want, withoutStoreTags(title), title)
	}
}

func TestNewCovers_boundedWait(t *testing.T) {
	t.Run("GIVEN the production provider", func(t *testing.T) {
		p := NewCovers()

		t.Run("THEN one request cannot keep a cover waiting for long", func(t *testing.T) {
			assert.LessOrEqual(t, p.API.HTTP.Timeout, 8*time.Second)
		})
	})
}
