package ntfy

import (
	"context"
	"errors"
	"mime"
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
	"gamevault/internal/domain/schema"
)

const testToken = "tk_exampletoken123"

var goodSettings = schema.Settings{
	"topic": "vault-test",
}

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

func decodeHeader(t *testing.T, v string) string {
	t.Helper()

	out, err := new(mime.WordDecoder).DecodeHeader(v)
	require.NoError(t, err)

	return out
}

func TestPlugin(t *testing.T) {
	t.Run("GIVEN the ntfy plugin", func(t *testing.T) {
		tr, err := plugintest.LoadTranslations(".")
		require.NoError(t, err)

		t.Run("THEN it passes the plugin checks", func(t *testing.T) {
			plugintest.Check(t, Plugin(), tr)
		})

		t.Run("THEN it keeps the default pace of one second", func(t *testing.T) {
			_, paced := any(NewConnector()).(notify.Pacer)

			assert.False(t, paced)
		})
	})
}

func TestSend_text(t *testing.T) {
	t.Run("GIVEN a message with a title, a link and a token", func(t *testing.T) {
		f := newFakeNtfy(t)
		settings := schema.Settings{"topic": "vault test", "token": testToken}

		m := notify.Message{
			Title: "New game",
			Text:  "Fish and Chips\narrived",
			Link:  "https://vault.example/games/1",
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), settings, m))

			t.Run("THEN it is a PUT to the topic with the text as the body and the headers set", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, http.MethodPut, calls[0].method)
				assert.Equal(t, "/vault%20test", calls[0].path)
				assert.Equal(t, "Fish and Chips\narrived", string(calls[0].body))
				assert.Equal(t, "New game", calls[0].header.Get("Title"))
				assert.Equal(t, "default", calls[0].header.Get("Priority"))
				assert.Equal(t, "https://vault.example/games/1", calls[0].header.Get("Click"))
				assert.Equal(t, "Bearer "+testToken, calls[0].header.Get("Authorization"))
			})
		})
	})

	t.Run("GIVEN an important message and no token", func(t *testing.T) {
		f := newFakeNtfy(t)

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, notify.Message{
				Title:     "Problem",
				Text:      "x",
				Important: true,
			}))

			t.Run("THEN the priority is high and there is no Authorization", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, "high", calls[0].header.Get("Priority"))
				assert.Empty(t, calls[0].header.Get("Authorization"))
				assert.Empty(t, calls[0].header.Get("Click"))
			})
		})
	})

	t.Run("GIVEN a title and text with non-ASCII characters", func(t *testing.T) {
		f := newFakeNtfy(t)

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, notify.Message{
				Title: "¡Nuevo juego!",
				Text:  "Ñandú aventura",
			}))

			t.Run("THEN the title header is RFC 2047 encoded and decodes to the title", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.True(t, strings.HasPrefix(strings.ToUpper(calls[0].header.Get("Title")), "=?UTF-8?"))
				assert.Equal(t, "¡Nuevo juego!", decodeHeader(t, calls[0].header.Get("Title")))
				assert.Equal(t, "Ñandú aventura", string(calls[0].body))
			})
		})
	})
}

func TestSend_server(t *testing.T) {
	t.Run("GIVEN a server in the settings", func(t *testing.T) {
		f := newFakeNtfy(t)
		c := NewConnector()
		c.API.HTTP = f.srv.Client()

		t.Run("WHEN a message is sent with the fake as the server, a trailing slash included", func(t *testing.T) {
			require.NoError(t, c.Send(ctx10(t), schema.Settings{"server": f.srv.URL + "/", "topic": "vault-test"}, notify.Message{Title: "x"}))

			t.Run("THEN the request reaches it without a double slash", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)
				assert.Equal(t, "/vault-test", calls[0].path)
			})
		})
	})

	t.Run("GIVEN no server in the settings", func(t *testing.T) {
		t.Run("THEN the default is ntfy.sh", func(t *testing.T) {
			assert.Equal(t, "https://ntfy.sh", NewConnector().API.BaseURL)
		})
	})

	for _, server := range []string{"ftp://ntfy.example", "ntfy.example", "file:///etc"} {
		t.Run("GIVEN the server "+server, func(t *testing.T) {
			f := newFakeNtfy(t)

			t.Run("WHEN a message is sent", func(t *testing.T) {
				err := f.connector().Send(ctx10(t), schema.Settings{"server": server, "topic": "t"}, notify.Message{Title: "x"})

				t.Run("THEN it is permanent, says http or https and sends nothing", func(t *testing.T) {
					require.ErrorIs(t, err, notify.ErrPermanent)
					assert.Contains(t, err.Error(), "http")
					assert.Empty(t, f.received())
				})
			})
		})
	}

	t.Run("GIVEN no topic", func(t *testing.T) {
		f := newFakeNtfy(t)

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), schema.Settings{}, notify.Message{Title: "x"})

			t.Run("THEN it is permanent without a request", func(t *testing.T) {
				require.ErrorIs(t, err, notify.ErrPermanent)
				assert.Empty(t, f.received())
			})
		})
	})
}

func TestSend_longMessage(t *testing.T) {
	t.Run("GIVEN a text far over the header limit with non-ASCII characters and an image", func(t *testing.T) {
		f := newFakeNtfy(t)
		text := strings.Repeat("é", 5000)

		m := notify.Message{
			Title: "Long",
			Text:  text,
			Images: []notify.Image{{
				Name:        "a.jpg",
				Data:        []byte("img"),
				ContentType: "image/jpeg",
			}},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN the Message header stays under the limit, cut with an ellipsis", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				raw := calls[0].header.Get("Message")
				assert.LessOrEqual(t, len(raw), 4000)

				decoded := decodeHeader(t, raw)
				assert.True(t, strings.HasSuffix(decoded, "…"))
				assert.Less(t, len([]rune(decoded)), 5000)
			})
		})
	})
}

func TestSend_images(t *testing.T) {
	t.Run("GIVEN a local cover", func(t *testing.T) {
		f := newFakeNtfy(t)

		m := notify.Message{
			Title: "New game",
			Text:  "Line one\nLine two",
			Link:  "https://vault.example/games/1",
			Images: []notify.Image{{
				Name:        "cover.jpg",
				Data:        []byte("JPEGBYTES"),
				ContentType: "image/jpeg",
			}},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN the body is the image with its file name and the text in Message with \\n", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, "JPEGBYTES", string(calls[0].body))
				assert.Equal(t, "cover.jpg", calls[0].header.Get("Filename"))
				assert.Equal(t, `Line one\nLine two`, calls[0].header.Get("Message"))
				assert.Equal(t, "New game", calls[0].header.Get("Title"))
				assert.Equal(t, "https://vault.example/games/1", calls[0].header.Get("Click"))
			})
		})
	})

	t.Run("GIVEN a cover without a name", func(t *testing.T) {
		f := newFakeNtfy(t)

		t.Run("WHEN it is sent", func(t *testing.T) {
			m := notify.Message{
				Title:  "x",
				Images: []notify.Image{{Data: []byte("a")}},
			}
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN a file name is still sent", func(t *testing.T) {
				assert.NotEmpty(t, f.received()[0].header.Get("Filename"))
			})
		})
	})

	t.Run("GIVEN a public image URL", func(t *testing.T) {
		f := newFakeNtfy(t)

		t.Run("WHEN it is sent", func(t *testing.T) {
			m := notify.Message{
				Title:  "Deal",
				Text:   "Cheap",
				Images: []notify.Image{{URL: "https://img.example/a.jpg"}},
			}
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN it goes as the Attach header with the text as the body", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, "https://img.example/a.jpg", calls[0].header.Get("Attach"))
				assert.Equal(t, "Cheap", string(calls[0].body))
				assert.Empty(t, calls[0].header.Get("Filename"))
			})
		})
	})

	t.Run("GIVEN the server says the attachment is too large", func(t *testing.T) {
		f := newFakeNtfy(t)
		f.failWithImage = true

		m := notify.Message{
			Title: "x",
			Text:  "news",
			Images: []notify.Image{{
				Name: "a.jpg",
				Data: []byte("big"),
			}},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), goodSettings, m)

			t.Run("THEN it is resent once as text and succeeds", func(t *testing.T) {
				require.NoError(t, err)

				calls := f.received()
				require.Len(t, calls, 2)
				assert.Empty(t, calls[1].header.Get("Filename"))
				assert.Equal(t, "news", string(calls[1].body))
			})
		})
	})
}

func TestSend_failures(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		permanent bool
		wantText  string
	}{
		{"an unauthorized token", http.StatusUnauthorized, unauthorized, true, "access token"},
		{"a forbidden topic", http.StatusForbidden, forbidden, true, "access token"},
		{"an unknown topic", http.StatusNotFound, notFound, true, "topic"},
		{"a bad request", http.StatusBadRequest, `{"code":40001,"http":400,"error":"bad"}`, true, "bad"},
		{"an attachment too large with nothing to drop", http.StatusRequestEntityTooLarge, tooLarge, true, "too large"},
		{"an outage", http.StatusBadGateway, ``, false, ""},
		{"a server error", http.StatusInternalServerError, `{"code":50001,"http":500,"error":"internal"}`, false, ""},
	}

	for _, tc := range cases {
		t.Run("GIVEN ntfy answers with "+tc.name, func(t *testing.T) {
			f := newFakeNtfy(t)
			f.fail(tc.status, tc.body)

			t.Run("WHEN a message is sent with a token", func(t *testing.T) {
				err := f.connector().Send(ctx10(t), schema.Settings{"topic": "t", "token": testToken}, notify.Message{Title: "x"})

				t.Run("THEN it is permanent only when retrying cannot help, and never names the token", func(t *testing.T) {
					require.Error(t, err)
					assert.Equal(t, tc.permanent, errors.Is(err, notify.ErrPermanent))
					assert.Contains(t, err.Error(), tc.wantText)
					assert.NotContains(t, err.Error(), testToken)

					if tc.permanent {
						assert.Equal(t, errkind.Invalid, errkind.Of(err))
					}
				})
			})
		})
	}

	t.Run("GIVEN ntfy limits the rate and asks to wait 12 seconds", func(t *testing.T) {
		f := newFakeNtfy(t)
		f.fail(http.StatusTooManyRequests, limited)
		f.retryAfter = "12"

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), goodSettings, notify.Message{Title: "x"})

			t.Run("THEN it is transient and says how long to wait", func(t *testing.T) {
				require.Error(t, err)
				assert.False(t, errors.Is(err, notify.ErrPermanent))

				var r retrier

				require.ErrorAs(t, err, &r)
				assert.Equal(t, 12*time.Second, r.RetryAfter())
			})
		})
	})

	t.Run("GIVEN ntfy limits the rate without saying for how long", func(t *testing.T) {
		f := newFakeNtfy(t)
		f.fail(http.StatusTooManyRequests, limited)

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), goodSettings, notify.Message{Title: "x"})

			t.Run("THEN RetryAfter is zero", func(t *testing.T) {
				var r retrier

				require.ErrorAs(t, err, &r)
				assert.Zero(t, r.RetryAfter())
			})
		})
	})

	t.Run("GIVEN nobody listens at the address", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()

		c := NewConnector()
		c.API.BaseURL = url

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := c.Send(ctx10(t), schema.Settings{"topic": "t", "token": testToken}, notify.Message{Title: "x"})

			t.Run("THEN the transport error is transient", func(t *testing.T) {
				require.Error(t, err)
				assert.False(t, errors.Is(err, notify.ErrPermanent))
				assert.NotContains(t, err.Error(), testToken)
			})
		})
	})
}

// TestSend_sharedFailures sends every shared failure through each kind of message: all of them
// fail, none permanently (a passing trouble is retried) and none names the token.
func TestSend_sharedFailures(t *testing.T) {
	messages := map[string]notify.Message{
		"text": {Title: "x"},
		"photo": {
			Title: "x",
			Images: []notify.Image{{
				Name: "a.jpg",
				Data: []byte("a"),
			}},
		},
	}

	for name, m := range messages {
		for _, fail := range apiclienttest.Failures() {
			t.Run("GIVEN ntfy answers a "+name+" with "+fail.Name, func(t *testing.T) {
				srv := httptest.NewServer(fail.Serve)
				t.Cleanup(srv.Close)

				c := NewConnector()
				c.API.BaseURL = srv.URL
				c.API.HTTP = srv.Client()

				t.Run("WHEN it is sent and when the connection is tested", func(t *testing.T) {
					settings := schema.Settings{"topic": "t", "token": testToken}

					for _, err := range []error{
						c.Send(ctx10(t), settings, m),
						c.Test(ctx10(t), settings, m),
					} {
						t.Run("THEN it fails, is retried and does not name the token", func(t *testing.T) {
							require.Error(t, err)
							assert.False(t, errors.Is(err, notify.ErrPermanent))
							assert.NotContains(t, err.Error(), testToken)
						})
					}
				})
			})
		}
	}
}

func TestTest(t *testing.T) {
	t.Run("GIVEN good settings", func(t *testing.T) {
		f := newFakeNtfy(t)

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := f.connector().Test(ctx10(t), goodSettings, notify.Message{
				Title: "Game Vault",
				Text:  "It works",
			})

			t.Run("THEN a plain message is sent", func(t *testing.T) {
				require.NoError(t, err)

				calls := f.received()
				require.Len(t, calls, 1)
				assert.Equal(t, "It works", string(calls[0].body))
			})
		})
	})

	t.Run("GIVEN a wrong topic", func(t *testing.T) {
		f := newFakeNtfy(t)
		f.fail(http.StatusNotFound, notFound)

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := f.connector().Test(ctx10(t), goodSettings, notify.Message{Title: "x"})

			t.Run("THEN the error says what to check", func(t *testing.T) {
				require.ErrorIs(t, err, notify.ErrPermanent)
				assert.Contains(t, err.Error(), "topic")
			})
		})
	})
}

func TestValidate(t *testing.T) {
	var _ notify.Validator = (*Connector)(nil)

	for _, tc := range []struct {
		name     string
		settings schema.Settings
		valid    bool
	}{
		{"the default server", schema.Settings{"topic": "vault-test"}, true},
		{"a server of one's own", schema.Settings{"topic": "vault-test", "server": "https://ntfy.example.test/"}, true},
		{"a server without a scheme", schema.Settings{"topic": "vault-test", "server": "ntfy.example.test"}, false},
		{"a server with another scheme", schema.Settings{"topic": "vault-test", "server": "ws://ntfy.example.test"}, false},
	} {
		t.Run("GIVEN "+tc.name+" WHEN validated", func(t *testing.T) {
			err := NewConnector().Validate(tc.settings)

			if tc.valid {
				t.Run("THEN it passes", func(t *testing.T) { assert.NoError(t, err) })

				return
			}

			t.Run("THEN it is an Invalid error saying what to fix", func(t *testing.T) {
				require.Error(t, err)
				assert.Equal(t, errkind.Invalid, errkind.Of(err))
				assert.Contains(t, err.Error(), "ntfy server")
			})
		})
	}
}
