package notify_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

func TestService_connections(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a service with one connector type", func(t *testing.T) {
		w := newWorld(t)

		t.Run("THEN it lists that type", func(t *testing.T) {
			types := w.svc.Types()
			require.Len(t, types, 1)
			assert.Equal(t, connection.Type("fake"), types[0].Type)
		})

		t.Run("WHEN a connection of an unknown type is saved THEN it is refused", func(t *testing.T) {
			_, err := w.svc.Save(ctx, "", "pigeon", connection.Config{
				Events:   []event.Kind{event.KindIssueOpened},
				Language: "en",
			})
			assert.ErrorIs(t, err, connection.ErrUnknownType)
		})

		t.Run("WHEN a connection without its required settings is saved THEN it is refused", func(t *testing.T) {
			_, err := w.svc.Save(ctx, "", "fake", connection.Config{
				Events:   []event.Kind{event.KindIssueOpened},
				Language: "en",
			})
			require.Error(t, err)

			list, err := w.svc.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, list)
		})

		t.Run("WHEN a saved connection is saved again with its secret masked", func(t *testing.T) {
			c := w.connect(t, ctx, "Phone", true, event.KindIssueOpened)

			_, err := w.svc.Save(ctx, c.ID(), "", connection.Config{
				Name:     "Phone 2",
				Enabled:  true,
				Events:   []event.Kind{event.KindIssueOpened},
				Language: "es",
				Settings: schema.Settings{
					"token": schema.SecretPlaceholder,
					"chat":  "43",
				},
			})
			require.NoError(t, err)

			t.Run("THEN it keeps the secret and takes the rest", func(t *testing.T) {
				got, err := w.svc.Get(ctx, c.ID())
				require.NoError(t, err)
				assert.Equal(t, "t-123", got.Settings()["token"])
				assert.Equal(t, "43", got.Settings()["chat"])
				assert.Equal(t, "Phone 2", got.Name())
				assert.Equal(t, "es", got.Language())
			})
		})
	})

	t.Run("GIVEN a connection with queued deliveries", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(true))
		gone := w.connect(t, ctx, "Gone", true, event.KindIssueOpened)
		kept := w.connect(t, ctx, "Kept", true, event.KindIssueOpened)
		w.svc.Publish(ctx, event.New(event.KindIssueOpened, w.clock.Now(), event.Issue{Key: "disk:config"}))
		require.Len(t, w.outbox.all(), 2)

		t.Run("WHEN it is deleted", func(t *testing.T) {
			require.NoError(t, w.svc.Delete(ctx, gone.ID()))

			t.Run("THEN it and its deliveries are gone, the others' stay", func(t *testing.T) {
				_, err := w.svc.Get(ctx, gone.ID())
				require.ErrorIs(t, err, connection.ErrNotFound)

				ds := w.outbox.all()
				require.Len(t, ds, 1)
				assert.Equal(t, kept.ID(), ds[0].Connection)

				recent, err := w.svc.Deliveries(ctx, kept.ID(), 20)
				require.NoError(t, err)
				assert.Len(t, recent, 1)
			})
		})

		t.Run("WHEN an unknown connection is deleted THEN it is not found", func(t *testing.T) {
			assert.ErrorIs(t, w.svc.Delete(ctx, "nope"), connection.ErrNotFound)
		})
	})
}

func TestService_address(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	w := newWorld(t)

	for _, bad := range []string{"ftp://x", "vault.example", "http://", "https://vault.example/?a=1", "/games"} {
		t.Run("GIVEN "+bad+" WHEN it is saved THEN it is refused", func(t *testing.T) {
			err := w.svc.SaveAddress(ctx, bad)
			assert.ErrorIs(t, err, notify.ErrBadAddress)
		})
	}

	t.Run("GIVEN an address with a trailing slash WHEN it is saved THEN it is stored without it", func(t *testing.T) {
		require.NoError(t, w.svc.SaveAddress(ctx, " https://vault.example:8080/gv/ "))

		got, err := w.svc.Address(ctx)
		require.NoError(t, err)
		assert.Equal(t, "https://vault.example:8080/gv", got)
	})

	t.Run("GIVEN an empty address WHEN it is saved THEN links are turned off", func(t *testing.T) {
		require.NoError(t, w.svc.SaveAddress(ctx, ""))

		got, err := w.svc.Address(ctx)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestService_test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cfg := connection.Config{
		Enabled:  true,
		Events:   []event.Kind{event.KindIssueOpened},
		Language: "en",
		Settings: schema.Settings{"token": "t-123"},
	}

	t.Run("GIVEN unsaved settings without a chat and sending off", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(false))
		w.connector.found = "777"

		t.Run("WHEN they are tested", func(t *testing.T) {
			found, err := w.svc.Test(ctx, "", "fake", cfg)
			require.NoError(t, err)

			t.Run("THEN the chat is found, returned and used for the sample", func(t *testing.T) {
				assert.Equal(t, map[string]string{"chat": "777"}, found)
				assert.Equal(t, 1, w.connector.finds)
				require.Len(t, w.connector.tested, 1)
				assert.Equal(t, "777", w.connector.tested[0]["chat"])
			})

			t.Run("AND the sample lists the two example games", func(t *testing.T) {
				m := w.connector.sent[0]
				assert.Equal(t, "Game Vault test", m.Title)
				assert.Contains(t, m.Text, "Example Game (PC)")
				assert.Contains(t, m.Text, "Another Game (PC)")
				assert.Empty(t, m.Images)
				assert.Equal(t, event.KindCollectionAdded, m.Event.Kind)
			})

			t.Run("AND nothing is queued", func(t *testing.T) {
				assert.Empty(t, w.outbox.all())
			})
		})
	})

	t.Run("GIVEN settings with a chat WHEN they are tested THEN no chat is looked for", func(t *testing.T) {
		w := newWorld(t)
		withChat := cfg
		withChat.Settings = schema.Settings{
			"token": "t-123",
			"chat":  "42",
		}

		found, err := w.svc.Test(ctx, "", "fake", withChat)
		require.NoError(t, err)
		assert.Empty(t, found)
		assert.Zero(t, w.connector.finds)
	})

	t.Run("GIVEN an unknown type WHEN it is tested THEN it is refused", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.svc.Test(ctx, "", "pigeon", cfg)
		assert.ErrorIs(t, err, connection.ErrUnknownType)
	})

	t.Run("GIVEN a saved connection that was paused", func(t *testing.T) {
		w := newWorld(t)
		c := w.connect(t, ctx, "Phone", true, event.KindIssueOpened)
		c.Pause("the token was rejected", w.clock.Now())
		require.NoError(t, w.conns.Save(ctx, c))

		t.Run("WHEN a test fails THEN it stays paused", func(t *testing.T) {
			w.connector.err = errors.New("unreachable")

			_, err := w.svc.Test(ctx, c.ID(), "", connection.Config{
				Language: "en",
				Settings: schema.Settings{
					"token": schema.SecretPlaceholder,
					"chat":  "42",
				},
			})
			require.Error(t, err)

			got, err := w.svc.Get(ctx, c.ID())
			require.NoError(t, err)
			assert.True(t, got.Paused())
		})

		t.Run("WHEN a test with the masked secret succeeds", func(t *testing.T) {
			w.connector.err = nil

			_, err := w.svc.Test(ctx, c.ID(), "", connection.Config{
				Language: "es",
				Settings: schema.Settings{
					"token": schema.SecretPlaceholder,
					"chat":  "42",
				},
			})
			require.NoError(t, err)

			t.Run("THEN it used the stored secret, in the language asked", func(t *testing.T) {
				last := w.connector.tested[len(w.connector.tested)-1]
				assert.Equal(t, "t-123", last["token"])
				assert.Equal(t, "42", last["chat"])
				assert.Equal(t, "Prueba de Game Vault", w.connector.sent[len(w.connector.sent)-1].Title)
			})

			t.Run("AND the connection is unpaused", func(t *testing.T) {
				got, err := w.svc.Get(ctx, c.ID())
				require.NoError(t, err)
				assert.False(t, got.Paused())
			})
		})
	})
}
