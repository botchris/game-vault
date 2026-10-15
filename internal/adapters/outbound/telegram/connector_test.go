package telegram

import (
	"context"
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
	"gamevault/internal/domain/schema"
)

var goodSettings = schema.Settings{
	"token": testToken,
	"chat":  "1001",
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

func TestPlugin(t *testing.T) {
	t.Run("GIVEN the Telegram plugin", func(t *testing.T) {
		tr, err := plugintest.LoadTranslations(".")
		require.NoError(t, err)

		t.Run("THEN it passes the plugin checks", func(t *testing.T) {
			plugintest.Check(t, Plugin(), tr)
		})

		t.Run("THEN it paces messages three seconds apart and can find the chat", func(t *testing.T) {
			c := NewConnector()

			assert.Equal(t, 3*time.Second, c.Gap())
			assert.Implements(t, (*notify.ChatFinder)(nil), c)
			assert.Implements(t, (*notify.Pacer)(nil), c)
		})
	})
}

func TestSend_text(t *testing.T) {
	t.Run("GIVEN a message without images and a title, a link and markup in the text", func(t *testing.T) {
		f := newFakeTelegram(t)

		m := notify.Message{
			Title:     "New game",
			Text:      "Fish & <Chips> arrived",
			Link:      "https://vault.example/games/1?a=1&b=2",
			LinkLabel: "open",
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN it is one escaped HTML sendMessage without a link preview", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, "sendMessage", calls[0].method)
				assert.Equal(t, "1001", calls[0].json["chat_id"])
				assert.Equal(t, "HTML", calls[0].json["parse_mode"])
				assert.Equal(t,
					"<b>New game</b>\nFish &amp; &lt;Chips&gt; arrived\n<a href=\"https://vault.example/games/1?a=1&amp;b=2\">open</a>",
					calls[0].json["text"])
				assert.Equal(t, map[string]any{"is_disabled": true}, calls[0].json["link_preview_options"])
			})
		})
	})
}

func TestSend_longText(t *testing.T) {
	t.Run("GIVEN a text longer than a message and a caption can hold", func(t *testing.T) {
		long := strings.Repeat("é", 5000)

		t.Run("WHEN it is sent as a message", func(t *testing.T) {
			f := newFakeTelegram(t)
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, notify.Message{
				Title: "T",
				Text:  long,
			}))

			t.Run("THEN it is cut at 4096 characters with an ellipsis", func(t *testing.T) {
				text := f.received()[0].json["text"].(string)

				assert.Equal(t, 4096, visible(text))
				assert.True(t, strings.HasSuffix(text, "…"))
			})
		})

		t.Run("WHEN it is sent with a photo", func(t *testing.T) {
			f := newFakeTelegram(t)
			m := notify.Message{
				Title: "T",
				Text:  long,
				Images: []notify.Image{{
					Name:        "a.jpg",
					Data:        []byte("x"),
					ContentType: "image/jpeg",
				}},
			}
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN the caption is cut at 1024 characters", func(t *testing.T) {
				caption := f.received()[0].fields["caption"]

				assert.Equal(t, 1024, visible(caption))
				assert.True(t, strings.HasSuffix(caption, "…"))
			})
		})
	})
}

// visible counts the characters Telegram counts: the text after its tags are read.
func visible(s string) int {
	s = strings.NewReplacer("<b>", "", "</b>", "").Replace(s)

	return len([]rune(s))
}

func TestSend_photo(t *testing.T) {
	t.Run("GIVEN a message with one local image", func(t *testing.T) {
		f := newFakeTelegram(t)

		m := notify.Message{
			Title: "Cover",
			Text:  "Added",
			Images: []notify.Image{{
				Name:        "cover.jpg",
				Data:        []byte("JPEGBYTES"),
				ContentType: "image/jpeg",
			}},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN it is a multipart sendPhoto with the bytes and the caption", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, "sendPhoto", calls[0].method)
				assert.Contains(t, calls[0].contentType, "multipart/form-data")
				assert.Equal(t, "1001", calls[0].fields["chat_id"])
				assert.Equal(t, "HTML", calls[0].fields["parse_mode"])
				assert.Equal(t, "<b>Cover</b>\nAdded", calls[0].fields["caption"])
				assert.Equal(t, []byte("JPEGBYTES"), calls[0].files["photo"])
			})
		})
	})

	t.Run("GIVEN a message with one public image", func(t *testing.T) {
		f := newFakeTelegram(t)

		m := notify.Message{
			Title:  "Deal",
			Images: []notify.Image{{URL: "https://img.example/a.jpg"}},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN the photo is the URL, not an upload", func(t *testing.T) {
				c := f.received()[0]

				assert.Equal(t, "https://img.example/a.jpg", c.fields["photo"])
				assert.Empty(t, c.files)
			})
		})
	})
}

func TestSend_album(t *testing.T) {
	t.Run("GIVEN a message with a local and a public image", func(t *testing.T) {
		f := newFakeTelegram(t)

		m := notify.Message{
			Title: "Two",
			Text:  "Both",
			Images: []notify.Image{
				{
					Name:        "a.png",
					Data:        []byte("AAA"),
					ContentType: "image/png",
				},
				{URL: "https://img.example/b.jpg"},
			},
		}

		t.Run("WHEN it is sent", func(t *testing.T) {
			require.NoError(t, f.connector().Send(ctx10(t), goodSettings, m))

			t.Run("THEN it is a sendMediaGroup with attach:// and URLs, the caption on the first", func(t *testing.T) {
				calls := f.received()
				require.Len(t, calls, 1)

				assert.Equal(t, "sendMediaGroup", calls[0].method)
				assert.JSONEq(t, `[
 {"type":"photo","media":"attach://p0","caption":"<b>Two</b>\nBoth","parse_mode":"HTML"},
 {"type":"photo","media":"https://img.example/b.jpg"}]`, calls[0].fields["media"])
				assert.Equal(t, []byte("AAA"), calls[0].files["p0"])
				assert.NotContains(t, calls[0].files, "p1")
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
		{"a rejected token", http.StatusUnauthorized, unauthorized, true, "create a new one with @BotFather"},
		{"an unknown chat", http.StatusBadRequest, chatNotFound, true, "send a message to the bot"},
		{"a blocked bot", http.StatusForbidden, blocked, true, "blocked by the user"},
		{"a conflict with a webhook", http.StatusConflict, webhookSet, true, "webhook"},
		{"an outage", http.StatusInternalServerError, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`, false, ""},
	}

	for _, tc := range cases {
		t.Run("GIVEN Telegram answers with "+tc.name, func(t *testing.T) {
			f := newFakeTelegram(t)
			f.fail(tc.status, tc.body)

			t.Run("WHEN a message is sent", func(t *testing.T) {
				err := f.connector().Send(ctx10(t), goodSettings, notify.Message{Title: "x"})

				t.Run("THEN the failure is permanent only when retrying cannot help", func(t *testing.T) {
					require.Error(t, err)
					assert.Equal(t, tc.permanent, errors.Is(err, notify.ErrPermanent))
					assert.Contains(t, err.Error(), tc.wantText)
					assert.NotContains(t, err.Error(), testToken)

					if tc.permanent {
						assert.Equal(t, errkind.Invalid, errkind.Of(err), "a failed Test reaches the API as InvalidArgument")
					}
				})
			})
		})
	}

	t.Run("GIVEN Telegram asks to wait 7 seconds", func(t *testing.T) {
		f := newFakeTelegram(t)
		f.fail(http.StatusTooManyRequests, flood)

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), goodSettings, notify.Message{Title: "x"})

			t.Run("THEN the error is not permanent and says how long to wait", func(t *testing.T) {
				require.Error(t, err)
				assert.False(t, errors.Is(err, notify.ErrPermanent))

				var r retrier

				require.ErrorAs(t, err, &r)
				assert.Equal(t, 7*time.Second, r.RetryAfter())
				assert.NotContains(t, err.Error(), testToken)
			})
		})
	})

	t.Run("GIVEN the token is wrong", func(t *testing.T) {
		f := newFakeTelegram(t)

		t.Run("WHEN a message is sent", func(t *testing.T) {
			bad := schema.Settings{"token": "999:BAD-token", "chat": "1001"}
			err := f.connector().Send(ctx10(t), bad, notify.Message{Title: "x"})

			t.Run("THEN it is permanent and the text lacks the token", func(t *testing.T) {
				require.ErrorIs(t, err, notify.ErrPermanent)
				assert.NotContains(t, err.Error(), "999:BAD-token")
			})
		})
	})

	t.Run("GIVEN no token or no chat", func(t *testing.T) {
		f := newFakeTelegram(t)

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx10(t), schema.Settings{"token": testToken}, notify.Message{Title: "x"})

			t.Run("THEN it is permanent without a request", func(t *testing.T) {
				require.ErrorIs(t, err, notify.ErrPermanent)
				assert.Empty(t, f.received())
			})
		})
	})
}

// TestSend_neverShowsTheToken sends every shared failure through each kind of message and checks
// that nothing returned names the token, which is part of the URL path and so of transport errors.
func TestSend_neverShowsTheToken(t *testing.T) {
	messages := map[string]notify.Message{
		"text": {Title: "x"},
		"photo": {
			Title: "x",
			Images: []notify.Image{{
				Name: "a.jpg",
				Data: []byte("a"),
			}},
		},
		"album": {
			Title: "x",
			Images: []notify.Image{{
				Name: "a.jpg",
				Data: []byte("a"),
			}, {URL: "https://img.example/b.jpg"}},
		},
	}

	for name, m := range messages {
		for _, fail := range apiclienttest.Failures() {
			t.Run("GIVEN Telegram answers a "+name+" with "+fail.Name, func(t *testing.T) {
				f := newFakeTelegram(t)
				srv := httptest.NewServer(fail.Serve)
				t.Cleanup(srv.Close)

				c := f.connector()
				c.API.BaseURL = srv.URL
				c.API.HTTP = srv.Client()

				t.Run("WHEN it is sent and when the connection is tested", func(t *testing.T) {
					for _, err := range []error{
						c.Send(ctx10(t), goodSettings, m),
						c.Test(ctx10(t), goodSettings, m),
					} {
						t.Run("THEN the error fails without naming the token", func(t *testing.T) {
							require.Error(t, err)
							assert.NotContains(t, err.Error(), testToken)
							assert.NotContains(t, err.Error(), "TEST-token")
						})
					}
				})
			})
		}
	}

	t.Run("GIVEN nobody listens at the address", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()

		c := NewConnector()
		c.API.BaseURL = url

		t.Run("WHEN a message is sent and the chat searched", func(t *testing.T) {
			sendErr := c.Send(ctx10(t), goodSettings, notify.Message{Title: "x"})
			_, _, findErr := c.FindChat(ctx10(t), schema.Settings{"token": testToken})

			t.Run("THEN the transport errors do not name the token", func(t *testing.T) {
				require.Error(t, sendErr)
				require.Error(t, findErr)
				assert.NotContains(t, sendErr.Error(), testToken)
				assert.NotContains(t, findErr.Error(), testToken)
				assert.False(t, errors.Is(sendErr, notify.ErrPermanent))
			})
		})
	})

	t.Run("GIVEN the caller gives up", func(t *testing.T) {
		f := newFakeTelegram(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		t.Run("WHEN a message is sent", func(t *testing.T) {
			err := f.connector().Send(ctx, goodSettings, notify.Message{Title: "x"})

			t.Run("THEN the error still matches context.Canceled and hides the token", func(t *testing.T) {
				require.ErrorIs(t, err, context.Canceled)
				assert.NotContains(t, err.Error(), testToken)
			})
		})
	})
}

func TestFindChat(t *testing.T) {
	t.Run("GIVEN a bot with two chats in its updates", func(t *testing.T) {
		f := newFakeTelegram(t)

		t.Run("WHEN the chat is searched", func(t *testing.T) {
			key, value, err := f.connector().FindChat(ctx10(t), schema.Settings{"token": testToken})

			t.Run("THEN it is the chat of the last update", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "chat", key)
				assert.Equal(t, "-100777", value)
				assert.Equal(t, "getUpdates", f.received()[0].method)
			})
		})
	})

	t.Run("GIVEN a bot nobody wrote to", func(t *testing.T) {
		f := newFakeTelegram(t)
		f.updates = noUpdates

		t.Run("WHEN the chat is searched", func(t *testing.T) {
			_, _, err := f.connector().FindChat(ctx10(t), schema.Settings{"token": testToken})

			t.Run("THEN the error says to write to the bot", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "send a message to the bot")
				assert.Equal(t, errkind.Invalid, errkind.Of(err))
			})
		})
	})

	t.Run("GIVEN a rejected token", func(t *testing.T) {
		f := newFakeTelegram(t)

		t.Run("WHEN the chat is searched", func(t *testing.T) {
			_, _, err := f.connector().FindChat(ctx10(t), schema.Settings{"token": "1:BAD"})

			t.Run("THEN it is a permanent error", func(t *testing.T) {
				require.ErrorIs(t, err, notify.ErrPermanent)
				assert.NotContains(t, err.Error(), "1:BAD")
			})
		})
	})
}

func TestTest(t *testing.T) {
	t.Run("GIVEN good settings", func(t *testing.T) {
		f := newFakeTelegram(t)

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := f.connector().Test(ctx10(t), goodSettings, notify.Message{
				Title: "Game Vault",
				Text:  "It works",
			})

			t.Run("THEN a message is sent", func(t *testing.T) {
				require.NoError(t, err)

				calls := f.received()
				require.Len(t, calls, 1)
				assert.Equal(t, "sendMessage", calls[0].method)
			})
		})
	})
}
