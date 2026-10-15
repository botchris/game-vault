package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

func newNotifyStore(t *testing.T) (context.Context, *NotifyStore) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	db, err := Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return ctx, NewNotifyStore(db)
}

func delivery(id string, conn connection.ID, next, created time.Time) notify.Delivery {
	return notify.Delivery{
		ID:         id,
		Connection: conn,
		Event: event.Event{
			ID:   "e-" + id,
			Kind: event.KindCollectionAdded,
			At:   created,
			Data: event.CollectionAdded{
				Origin:     event.OriginSource,
				OriginName: "Humble Bundle",
				Games: []event.GameRef{{
					ID:      "g1",
					Title:   "Hades",
					Systems: []string{"PC"},
				}},
				CopiesAdded: 2,
			},
		},
		State:     notify.StateQueued,
		NextAt:    next,
		CreatedAt: created,
		UpdatedAt: created,
	}
}

func TestNotifyStore_outbox(t *testing.T) {
	ctx, store := newNotifyStore(t)
	t0 := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	a, b := connection.ID("conn-a"), connection.ID("conn-b")

	t.Run("GIVEN two deliveries queued for two connections", func(t *testing.T) {
		require.NoError(t, store.Enqueue(ctx, []notify.Delivery{
			delivery("d2", b, t0, t0.Add(time.Second)),
			delivery("d1", a, t0, t0),
			delivery("d3", a, t0.Add(time.Hour), t0.Add(2*time.Second)),
		}))

		t.Run("WHEN the due ones are read", func(t *testing.T) {
			due, err := store.Due(ctx, t0.Add(time.Minute))
			require.NoError(t, err)

			t.Run("THEN they come in due order, with the event decoded", func(t *testing.T) {
				require.Len(t, due, 2)
				assert.Equal(t, "d1", due[0].ID)
				assert.Equal(t, "d2", due[1].ID)
				assert.Equal(t, a, due[0].Connection)
				assert.Equal(t, notify.StateQueued, due[0].State)
				assert.Equal(t, delivery("d1", a, t0, t0).Event, due[0].Event)
			})
		})

		t.Run("WHEN one is retried", func(t *testing.T) {
			require.NoError(t, store.MarkRetry(ctx, "d1", 1, t0.Add(time.Hour), "timeout", t0.Add(time.Minute)))

			due, err := store.Due(ctx, t0.Add(time.Minute))
			require.NoError(t, err)

			t.Run("THEN it waits and keeps its attempts and reason", func(t *testing.T) {
				require.Len(t, due, 1)
				assert.Equal(t, "d2", due[0].ID)

				recent, err := store.Recent(ctx, a, 20)
				require.NoError(t, err)
				require.Len(t, recent, 2)
				assert.Equal(t, "d3", recent[0].ID, "newest first")
				assert.Equal(t, 1, recent[1].Attempts)
				assert.Equal(t, "timeout", recent[1].LastError)
				assert.True(t, recent[1].NextAt.Equal(t0.Add(time.Hour)))
			})
		})

		t.Run("WHEN they are sent, failed and dropped", func(t *testing.T) {
			old := t0.Add(2 * time.Minute)
			require.NoError(t, store.MarkSent(ctx, "d1", old))
			require.NoError(t, store.MarkFailed(ctx, "d2", "rejected", old))
			require.NoError(t, store.MarkDropped(ctx, "d3", old))

			recent, err := store.Recent(ctx, a, 1)
			require.NoError(t, err)

			t.Run("THEN the states are recorded and Recent honors its limit", func(t *testing.T) {
				require.Len(t, recent, 1)
				assert.Equal(t, notify.StateDropped, recent[0].State)

				got, err := store.Recent(ctx, b, 5)
				require.NoError(t, err)
				require.Len(t, got, 1)
				assert.Equal(t, notify.StateFailed, got[0].State)
				assert.Equal(t, "rejected", got[0].LastError)
			})
		})
	})

	t.Run("GIVEN finished and queued deliveries", func(t *testing.T) {
		require.NoError(t, store.Enqueue(ctx, []notify.Delivery{
			delivery("q1", "conn-c", t0, t0.Add(-48*time.Hour)),
		}))

		t.Run("WHEN old ones are pruned", func(t *testing.T) {
			require.NoError(t, store.Prune(ctx, t0.Add(time.Hour)))

			t.Run("THEN finished ones go and queued ones stay", func(t *testing.T) {
				gone, err := store.Recent(ctx, a, 20)
				require.NoError(t, err)
				assert.Empty(t, gone)

				kept, err := store.Recent(ctx, "conn-c", 20)
				require.NoError(t, err)
				assert.Len(t, kept, 1)
			})
		})

		t.Run("WHEN a connection's deliveries are deleted", func(t *testing.T) {
			require.NoError(t, store.DeleteFor(ctx, "conn-c"))

			t.Run("THEN none remain", func(t *testing.T) {
				got, err := store.Recent(ctx, "conn-c", 20)
				require.NoError(t, err)
				assert.Empty(t, got)
			})
		})
	})
}

func TestNotifyStore_dueIgnoresFractionWidth(t *testing.T) {
	ctx, store := newNotifyStore(t)
	whole := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

	t.Run("GIVEN a delivery due on a whole second", func(t *testing.T) {
		require.NoError(t, store.Enqueue(ctx, []notify.Delivery{delivery("d1", "conn-a", whole, whole)}))

		t.Run("WHEN it is read half a second later", func(t *testing.T) {
			due, err := store.Due(ctx, whole.Add(500*time.Millisecond))
			require.NoError(t, err)

			t.Run("THEN it is due", func(t *testing.T) {
				assert.Len(t, due, 1)
			})
		})
	})
}

func TestNotifyStore_memory(t *testing.T) {
	ctx, store := newNotifyStore(t)
	since := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)

	t.Run("GIVEN nothing remembered", func(t *testing.T) {
		t.Run("WHEN the issues are read", func(t *testing.T) {
			got, err := store.Notified(ctx)
			require.NoError(t, err)

			t.Run("THEN there are none and the watcher never ran", func(t *testing.T) {
				assert.Empty(t, got)

				seeded, err := store.Seeded(ctx)
				require.NoError(t, err)
				assert.False(t, seeded)
			})
		})

		t.Run("WHEN issues are saved and the watcher is marked", func(t *testing.T) {
			issues := map[string]event.Issue{
				"source:humble": {
					Key:     "source:humble",
					Level:   "error",
					Kind:    "source",
					Source:  "Humble Bundle",
					Reason:  "signed_out",
					Params:  map[string]string{"name": "Humble"},
					Message: "Signed out",
					Since:   since,
				},
			}
			require.NoError(t, store.SaveNotified(ctx, issues))
			require.NoError(t, store.MarkSeeded(ctx))

			t.Run("THEN they are read back equal, and replaced by the next save", func(t *testing.T) {
				got, err := store.Notified(ctx)
				require.NoError(t, err)
				assert.Equal(t, issues, got)

				seeded, err := store.Seeded(ctx)
				require.NoError(t, err)
				assert.True(t, seeded)

				require.NoError(t, store.SaveNotified(ctx, map[string]event.Issue{}))

				got, err = store.Notified(ctx)
				require.NoError(t, err)
				assert.Empty(t, got)
			})
		})
	})

	t.Run("GIVEN thresholds told for two key copies", func(t *testing.T) {
		at := since.Add(time.Hour)
		require.NoError(t, store.RememberKey(ctx, "c1", 30, at))
		require.NoError(t, store.RememberKey(ctx, "c1", 7, at))
		require.NoError(t, store.RememberKey(ctx, "c1", 7, at), "telling twice is harmless")
		require.NoError(t, store.RememberKey(ctx, "c2", 1, at))

		t.Run("WHEN they are read", func(t *testing.T) {
			got, err := store.KeyThresholds(ctx)
			require.NoError(t, err)

			t.Run("THEN each copy lists its thresholds", func(t *testing.T) {
				assert.ElementsMatch(t, []int{30, 7}, got["c1"])
				assert.Equal(t, []int{1}, got["c2"])
			})
		})

		t.Run("WHEN the copies to keep are only c2", func(t *testing.T) {
			require.NoError(t, store.ForgetKeys(ctx, []game.ID{"c2"}))

			got, err := store.KeyThresholds(ctx)
			require.NoError(t, err)

			t.Run("THEN c1 is forgotten", func(t *testing.T) {
				assert.NotContains(t, got, game.ID("c1"))
				assert.Contains(t, got, game.ID("c2"))
			})
		})

		t.Run("WHEN no copy is kept", func(t *testing.T) {
			require.NoError(t, store.ForgetKeys(ctx, nil))

			got, err := store.KeyThresholds(ctx)
			require.NoError(t, err)

			t.Run("THEN all are forgotten", func(t *testing.T) {
				assert.Empty(t, got)
			})
		})
	})
}

func TestNotifyStore_address(t *testing.T) {
	ctx, store := newNotifyStore(t)

	t.Run("GIVEN no address saved", func(t *testing.T) {
		got, err := store.Address(ctx)
		require.NoError(t, err)
		assert.Empty(t, got)

		t.Run("WHEN one is saved", func(t *testing.T) {
			require.NoError(t, store.SaveAddress(ctx, "http://192.168.1.10:8080"))

			t.Run("THEN it is read back", func(t *testing.T) {
				got, err := store.Address(ctx)
				require.NoError(t, err)
				assert.Equal(t, "http://192.168.1.10:8080", got)
			})
		})
	})
}

func TestNotifyStore_unreadableDeliveries(t *testing.T) {
	ctx, store := newNotifyStore(t)
	t0 := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	a := connection.ID("conn-a")

	t.Run("GIVEN a good delivery between two rows this version cannot read", func(t *testing.T) {
		require.NoError(t, store.Enqueue(ctx, []notify.Delivery{delivery("d2", a, t0, t0.Add(time.Second))}))

		// One row from a newer version (an event kind this one does not know), one damaged.
		for _, bad := range []struct {
			id      string
			raw     string
			created time.Time
		}{
			{"d1", `{"v":1,"id":"e-d1","kind":"future.kind","at":"2026-10-15T08:00:00Z","data":{}}`, t0},
			{"d3", `{not json`, t0.Add(2 * time.Second)},
		} {
			_, err := store.db.conn(ctx).ExecContext(ctx, `INSERT INTO notification_outbox
				(id, connection_id, event, state, attempts, next_at, last_error, created_at, updated_at)
				VALUES (?, ?, ?, ?, 0, ?, '', ?, ?)`,
				bad.id, a, bad.raw, notify.StateQueued, sortableTime(t0), sortableTime(bad.created), sortableTime(bad.created))
			require.NoError(t, err)
		}

		t.Run("WHEN the due ones are read", func(t *testing.T) {
			due, err := store.Due(ctx, t0.Add(time.Minute))

			t.Run("THEN the good one is delivered and the others are skipped", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, due, 1)
				assert.Equal(t, "d2", due[0].ID)
			})
		})

		t.Run("WHEN the recent ones are listed", func(t *testing.T) {
			recent, err := store.Recent(ctx, a, 10)

			t.Run("THEN the unreadable ones show as failed, with the reason", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, recent, 3)
				assert.Equal(t, []string{"d3", "d2", "d1"}, []string{recent[0].ID, recent[1].ID, recent[2].ID})

				for _, d := range []notify.Delivery{recent[0], recent[2]} {
					assert.Equal(t, notify.StateFailed, d.State)
					assert.Equal(t, "unreadable notification (from a newer version?)", d.LastError)
				}

				assert.Equal(t, notify.StateQueued, recent[1].State)
				assert.Equal(t, event.Kind("future.kind"), recent[2].Event.Kind)
			})

			t.Run("AND they are no longer due", func(t *testing.T) {
				due, err := store.Due(ctx, t0.Add(time.Minute))
				require.NoError(t, err)
				require.Len(t, due, 1)
			})
		})
	})
}
