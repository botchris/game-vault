package auth_test

import (
	"context"
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/event"
)

// recorder is an events.Publisher that keeps what it is given.
type recorder struct {
	events []event.Event
}

func (r *recorder) Publish(_ context.Context, e event.Event) { r.events = append(r.events, e) }

func TestLogin_publishesRepeatedFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server publishing its events", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		c := &clock{now: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
		rec := &recorder{}
		svc := appauth.NewService(sqlite.NewAuthRepository(db), sqlite.NewSettingsRepository(db), plainHasher{}, noCerts{}, c.Now,
			slog.New(slog.NewTextHandler(io.Discard, nil)), appauth.WithEvents(rec))

		from := auth.Request{ClientIP: netip.MustParseAddr("192.0.2.10")}
		fail := func(t *testing.T, r auth.Request) {
			t.Helper()

			_, _, err := svc.Login(ctx, r, "admin", "wrong")
			require.ErrorIs(t, err, auth.ErrBadCredentials)

			c.now = c.now.Add(time.Minute)
		}

		t.Run("WHEN two sign-ins fail from one address", func(t *testing.T) {
			fail(t, from)
			fail(t, from)

			t.Run("THEN nothing is published", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})

		t.Run("WHEN a third fails", func(t *testing.T) {
			fail(t, from)

			t.Run("THEN auth.failed_logins tells the address, the count and the span", func(t *testing.T) {
				require.Len(t, rec.events, 1)
				assert.Equal(t, event.KindFailedLogins, rec.events[0].Kind)
				assert.Equal(t, event.FailedLogins{
					Address: "192.0.2.10",
					Count:   3,
					First:   time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC),
					Last:    time.Date(2026, 10, 15, 9, 2, 0, 0, time.UTC),
				}, rec.events[0].Data)
			})
		})

		t.Run("WHEN a fourth fails in the same hour", func(t *testing.T) {
			fail(t, from)

			t.Run("THEN nothing more is published", func(t *testing.T) {
				assert.Len(t, rec.events, 1)
			})
		})

		t.Run("WHEN three fail from another address", func(t *testing.T) {
			other := auth.Request{ClientIP: netip.MustParseAddr("192.0.2.20")}
			fail(t, other)
			fail(t, other)

			t.Run("THEN they count apart: two are not enough", func(t *testing.T) {
				assert.Len(t, rec.events, 1)
			})

			fail(t, other)

			t.Run("AND the third is published for that address", func(t *testing.T) {
				require.Len(t, rec.events, 2)
				assert.Equal(t, "192.0.2.20", rec.events[1].Data.(event.FailedLogins).Address)
			})
		})

		t.Run("WHEN the hour has passed and three more fail from the first address", func(t *testing.T) {
			c.now = c.now.Add(time.Hour)

			fail(t, from)
			fail(t, from)
			fail(t, from)

			t.Run("THEN a new window is published", func(t *testing.T) {
				require.Len(t, rec.events, 3)
				assert.Equal(t, "192.0.2.10", rec.events[2].Data.(event.FailedLogins).Address)
				assert.Equal(t, 3, rec.events[2].Data.(event.FailedLogins).Count)
			})
		})
	})
}
