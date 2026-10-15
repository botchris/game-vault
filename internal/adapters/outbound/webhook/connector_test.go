package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/application/notify"
	"gamevault/internal/application/plugin/plugintest"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

// retrier is what the dispatcher reads from a rate limit.
type retrier interface {
	// RetryAfter is how long to wait before the next try.
	RetryAfter() time.Duration
}

func ctx10(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func settingsFor(f *fakeHook) schema.Settings {
	return schema.Settings{"url": f.url()}
}

func sampleEvent() event.Event {
	return event.Event{
		ID:   "evt-1",
		Kind: event.KindCollectionAdded,
		At:   time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC),
		Data: event.CollectionAdded{
			Origin:     event.OriginSource,
			OriginName: "Example Store",
			Games: []event.GameRef{{
				ID:      "g1",
				Title:   "Example Game",
				Systems: []string{"PC"},
			}},
		},
	}
}

func TestPlugin(t *testing.T) {
	t.Run("GIVEN the webhook plugin", func(t *testing.T) {
		tr, err := plugintest.LoadTranslations(".")
		require.NoError(t, err)

		t.Run("THEN it passes the plugin checks and keeps the default pace", func(t *testing.T) {
			plugintest.Check(t, Plugin(), tr)

			_, paced := any(NewConnector()).(notify.Pacer)
			assert.False(t, paced)
		})
	})
}

func TestSend_document(t *testing.T) {
	t.Run("GIVEN an event with a link and a local and a public image", func(t *testing.T) {
		f := newFakeHook(t)
		ev := sampleEvent()

		m := notify.Message{
			Title: "New game",
			Text:  "Example Game arrived",
			Link:  "https://vault.example/games/g1",
			Images: []notify.Image{
				{
					Name: "a.jpg",
					Data: []byte("bytes"),
				},
				{URL: "https://img.example/a.jpg"},
			},
			Event: ev,
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), settingsFor(f), m))

			t.Run("THEN it is a JSON POST with exactly the documented fields", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, http.MethodPost, calls[0].method)
				assert.Equal(t, "/hook", calls[0].path)
				assert.Equal(t, "application/json", calls[0].header.Get("Content-Type"))

				var doc map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(calls[0].body, &doc))

				keys := make([]string, 0, len(doc))
				for k := range doc {
					keys = append(keys, k)
				}

				assert.ElementsMatch(t, []string{"version", "event", "at", "data", "message", "images"}, keys)
				assert.JSONEq(t, `1`, string(doc["version"]))
				assert.JSONEq(t, `"collection.added"`, string(doc["event"]))
				assert.JSONEq(t, `"2026-10-15T10:00:00Z"`, string(doc["at"]))
				assert.JSONEq(t, `{"title":"New game","text":"Example Game arrived","link":"https://vault.example/games/g1"}`, string(doc["message"]))
				assert.JSONEq(t, `["https://img.example/a.jpg"]`, string(doc["images"]))
			})

			t.Run("AND data is the payload exactly as the notification codec encodes it", func(t *testing.T) {
				coded, err := notify.EncodeEvent(ev)
				require.NoError(t, err)

				var envelope struct {
					Data json.RawMessage `json:"data"`
				}

				require.NoError(t, json.Unmarshal([]byte(coded), &envelope))

				var doc struct {
					Data json.RawMessage `json:"data"`
				}

				require.NoError(t, json.Unmarshal(f.received()[0].body, &doc))
				assert.JSONEq(t, string(envelope.Data), string(doc.Data))
				assert.JSONEq(t, `{"origin":"source","originName":"Example Store","games":[{"id":"g1","title":"Example Game","systems":["PC"]}],"copiesAdded":0}`, string(doc.Data))
			})
		})
	})

	t.Run("GIVEN a payload with empty lists and no images", func(t *testing.T) {
		f := newFakeHook(t)
		ev := event.Event{
			ID:   "evt-2",
			Kind: event.KindCollectionAdded,
			At:   time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC),
			Data: event.CollectionAdded{Origin: event.OriginManual},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), settingsFor(f), notify.Message{
				Title: "x",
				Event: ev,
			}))

			t.Run("THEN lists are [] and never null, and images is []", func(t *testing.T) {
				body := string(f.received()[0].body)

				assert.Contains(t, body, `"games":[]`)
				assert.Contains(t, body, `"images":[]`)
				assert.NotContains(t, body, "null")
			})
		})
	})
}

func TestSend_authHeader(t *testing.T) {
	t.Run("GIVEN a header name and its secret value", func(t *testing.T) {
		f := newFakeHook(t)
		settings := schema.Settings{"url": f.url(), "header": "X-Webhook-Secret", "secret": testSecret}

		t.Run("WHEN a message is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), settings, notify.Message{
				Title: "x",
				Event: sampleEvent(),
			}))

			t.Run("THEN the header is on the request", func(t *testing.T) {
				assert.Equal(t, testSecret, f.received()[0].header.Get("X-Webhook-Secret"))
			})
		})
	})

	t.Run("GIVEN only a header name, or only a secret", func(t *testing.T) {
		f := newFakeHook(t)

		t.Run("WHEN messages are sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), schema.Settings{"url": f.url(), "header": "X-Key"}, notify.Message{Event: sampleEvent()}))
			require.NoError(t, f.connector().Send(ctx10(t), schema.Settings{"url": f.url(), "secret": testSecret}, notify.Message{Event: sampleEvent()}))

			t.Run("THEN no custom header is sent", func(t *testing.T) {
				for _, c := range f.received() {
					assert.Empty(t, c.header.Get("X-Key"))

					for _, vs := range c.header {
						assert.NotContains(t, strings.Join(vs, ","), testSecret)
					}
				}
			})
		})
	})

	t.Run("GIVEN an invalid header name", func(t *testing.T) {
		f := newFakeHook(t)

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), schema.Settings{"url": f.url(), "header": "Bad Name", "secret": testSecret}, notify.Message{Event: sampleEvent()})

			t.Run("THEN it is permanent, sends nothing and hides the secret", func(t *testing.T) {
				require.ErrorIs(t, err, notify.ErrPermanent)
				assert.NotContains(t, err.Error(), testSecret)
				assert.Empty(t, f.received())
			})
		})
	})
}

func TestSend_url(t *testing.T) {
	for _, u := range []string{"", "ftp://hooks.example/x", "hooks.example/x", "file:///etc/passwd", "http://"} {
		t.Run("GIVEN the address "+u, func(t *testing.T) {
			f := newFakeHook(t)

			t.Run("WHEN a message is sent and when the connection is tested", func(t *testing.T) {
				for _, err := range []error{
					f.connector().Send(ctx10(t), schema.Settings{"url": u}, notify.Message{Event: sampleEvent()}),
					f.connector().Test(ctx10(t), schema.Settings{"url": u}, notify.Message{}),
				} {
					t.Run("THEN it is permanent and says http or https", func(t *testing.T) {
						require.ErrorIs(t, err, notify.ErrPermanent)
						assert.Equal(t, errkind.Invalid, errkind.Of(err))
						assert.Contains(t, err.Error(), "http")
						assert.Empty(t, f.received())
					})
				}
			})
		})
	}
}

func TestSend_statuses(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
		wantText  string
	}{
		{"204 no content", http.StatusNoContent, false, ""},
		{"a 400", http.StatusBadRequest, true, "HTTP 400"},
		{"a 401", http.StatusUnauthorized, true, "HTTP 401"},
		{"a 404", http.StatusNotFound, true, "HTTP 404"},
		{"a 410", http.StatusGone, true, "HTTP 410"},
		{"a 408", http.StatusRequestTimeout, false, "HTTP 408"},
		{"a 500", http.StatusInternalServerError, false, "HTTP 500"},
		{"a 503", http.StatusServiceUnavailable, false, "HTTP 503"},
	}

	for _, tc := range cases {
		t.Run("GIVEN the receiver answers "+tc.name, func(t *testing.T) {
			f := newFakeHook(t)
			f.status = tc.status
			f.echo = "X-Webhook-Secret"

			t.Run("WHEN a message is sent with a secret header", func(t *testing.T) {
				settings := schema.Settings{"url": f.url(), "header": "X-Webhook-Secret", "secret": testSecret}
				err := f.connector().Send(ctx10(t), settings, notify.Message{Event: sampleEvent()})

				t.Run("THEN only a 2xx is sent, and only a refusal is permanent", func(t *testing.T) {
					if tc.status < 300 {
						require.NoError(t, err)

						return
					}

					require.Error(t, err)
					assert.Equal(t, tc.permanent, errors.Is(err, notify.ErrPermanent))
					assert.Contains(t, err.Error(), tc.wantText)
					assert.NotContains(t, err.Error(), testSecret, "an error page may echo the secret")

					if tc.permanent {
						assert.Equal(t, errkind.Invalid, errkind.Of(err))
					}
				})
			})
		})
	}

	t.Run("GIVEN the receiver limits the rate and asks to wait 9 seconds", func(t *testing.T) {
		f := newFakeHook(t)
		f.status = http.StatusTooManyRequests
		f.header = http.Header{"Retry-After": {"9"}}

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), settingsFor(f), notify.Message{Event: sampleEvent()})

			t.Run("THEN it is transient with the wait", func(t *testing.T) {
				require.Error(t, err)
				assert.False(t, errors.Is(err, notify.ErrPermanent))

				var r retrier

				require.ErrorAs(t, err, &r)
				assert.Equal(t, 9*time.Second, r.RetryAfter())
			})
		})
	})

	t.Run("GIVEN the receiver limits the rate without a wait", func(t *testing.T) {
		f := newFakeHook(t)
		f.status = http.StatusTooManyRequests

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), settingsFor(f), notify.Message{Event: sampleEvent()})

			t.Run("THEN RetryAfter is zero", func(t *testing.T) {
				var r retrier

				require.ErrorAs(t, err, &r)
				assert.Zero(t, r.RetryAfter())
			})
		})
	})
}

func TestSend_redirect(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run("GIVEN the receiver redirects with "+http.StatusText(status), func(t *testing.T) {
			target := newFakeHook(t)
			f := newFakeHook(t)
			f.status = status
			f.header = http.Header{"Location": {target.url()}}

			t.Run("WHEN a message is sent", func(t *testing.T) {
				err := f.connector().Send(ctx10(t), schema.Settings{"url": f.url(), "header": "X-Webhook-Secret", "secret": testSecret}, notify.Message{Event: sampleEvent()})

				t.Run("THEN it is permanent, says to use the final address and never follows", func(t *testing.T) {
					require.ErrorIs(t, err, notify.ErrPermanent)
					assert.Contains(t, err.Error(), "the webhook answered with a redirect: use its final address")
					assert.Empty(t, target.received(), "the secret must not travel to another address")
				})
			})
		})
	}
}

// TestSend_sharedFailures sends the shared failures: a service that limits the rate, is down or
// does not answer fails and is retried, and none names the secret. A web page or a truncated JSON
// with HTTP 200 is a 2xx: a receiver owes the sender nothing in its body.
func TestSend_sharedFailures(t *testing.T) {
	for _, fail := range apiclienttest.Failures() {
		t.Run("GIVEN the receiver answers with "+fail.Name, func(t *testing.T) {
			srv := httptest.NewServer(fail.Serve)
			t.Cleanup(srv.Close)

			c := NewConnector()
			c.API.HTTP = srv.Client()

			t.Run("WHEN a message is sent and when the connection is tested", func(t *testing.T) {
				settings := schema.Settings{"url": srv.URL, "header": "X-Key", "secret": testSecret}

				for _, err := range []error{
					c.Send(ctx10(t), settings, notify.Message{Event: sampleEvent()}),
					c.Test(ctx10(t), settings, notify.Message{Title: "x"}),
				} {
					t.Run("THEN a failure is transient, names no secret, and a 200 is a success", func(t *testing.T) {
						if fail.Want == nil || fail.SignInPage {
							assert.NoError(t, err)

							return
						}

						require.Error(t, err)
						assert.False(t, errors.Is(err, notify.ErrPermanent))
						assert.NotContains(t, err.Error(), testSecret)
					})
				}
			})
		})
	}

	t.Run("GIVEN the address holds a secret token and nobody listens", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL + "/hooks/" + testSecret
		srv.Close()

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := NewConnector().Send(ctx10(t), schema.Settings{"url": url}, notify.Message{Event: sampleEvent()})

			t.Run("THEN the transport error is transient and does not repeat the address", func(t *testing.T) {
				require.Error(t, err)
				assert.False(t, errors.Is(err, notify.ErrPermanent))
				assert.NotContains(t, err.Error(), testSecret)
			})
		})
	})

	t.Run("GIVEN the caller gives up", func(t *testing.T) {
		f := newFakeHook(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx, settingsFor(f), notify.Message{Event: sampleEvent()})

			t.Run("THEN the error still matches context.Canceled", func(t *testing.T) {
				require.ErrorIs(t, err, context.Canceled)
			})
		})
	})
}

func TestTest(t *testing.T) {
	t.Run("GIVEN good settings", func(t *testing.T) {
		f := newFakeHook(t)

		t.Run("WHEN the connection is tested with the sample message", func(t *testing.T) {
			before := time.Now().UTC().Add(-time.Second)
			err := f.connector().Test(ctx10(t), settingsFor(f), notify.Message{
				Title: "Game Vault",
				Text:  "It works",
				Link:  "https://vault.example",
			})

			t.Run("THEN a test event with empty data and the sample is posted", func(t *testing.T) {
				require.NoError(t, err)

				var doc struct {
					Version int             `json:"version"`
					Event   string          `json:"event"`
					At      time.Time       `json:"at"`
					Data    json.RawMessage `json:"data"`
					Message struct {
						Title string `json:"title"`
						Text  string `json:"text"`
						Link  string `json:"link"`
					} `json:"message"`
					Images []string `json:"images"`
				}

				calls := f.received()
				require.Len(t, calls, 1)
				require.NoError(t, json.Unmarshal(calls[0].body, &doc))

				assert.Equal(t, 1, doc.Version)
				assert.Equal(t, "test", doc.Event)
				assert.JSONEq(t, `{}`, string(doc.Data))
				assert.True(t, doc.At.After(before))
				assert.Equal(t, "Game Vault", doc.Message.Title)
				assert.Equal(t, "It works", doc.Message.Text)
				assert.Equal(t, "https://vault.example", doc.Message.Link)
				assert.Empty(t, doc.Images)
			})
		})
	})
}
