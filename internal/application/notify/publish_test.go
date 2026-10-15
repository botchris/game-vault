package notify_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/events"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

var _ events.Publisher = (*notify.Service)(nil)

func added(origin event.Origin, titles ...string) event.CollectionAdded {
	games := make([]event.GameRef, 0, len(titles))
	for _, title := range titles {
		games = append(games, event.GameRef{
			ID:      game.ID("id-" + title),
			Title:   title,
			Systems: []string{"PC"},
		})
	}

	return event.CollectionAdded{
		Origin:     origin,
		OriginName: "Example Store",
		Games:      games,
	}
}

func TestPublish_queuesForSubscribers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN sending on, one connection subscribed to collection.added and one to issue.opened only", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(true))
		phone := w.connect(t, ctx, "Phone", true, event.KindCollectionAdded)
		w.connect(t, ctx, "Desk", true, event.KindIssueOpened)

		t.Run("WHEN a collection.added is published", func(t *testing.T) {
			e := event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginSource, "Example Game"))
			w.svc.Publish(ctx, e)

			t.Run("THEN one delivery is queued, for the first, due now", func(t *testing.T) {
				ds := w.outbox.all()
				require.Len(t, ds, 1)
				assert.Equal(t, phone.ID(), ds[0].Connection)
				assert.Equal(t, notify.StateQueued, ds[0].State)
				assert.Equal(t, w.clock.Now(), ds[0].NextAt)
				assert.Equal(t, e.ID, ds[0].Event.ID)
				assert.NotEmpty(t, ds[0].ID)
			})
		})
	})

	t.Run("GIVEN a disabled connection and a paused one", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(true))
		w.connect(t, ctx, "Off", false, event.KindCollectionAdded)
		paused := w.connect(t, ctx, "Paused", true, event.KindCollectionAdded)
		paused.Pause("the token was rejected", w.clock.Now())
		require.NoError(t, w.conns.Save(ctx, paused))

		t.Run("WHEN a collection.added is published THEN nothing is queued", func(t *testing.T) {
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginSource, "Example Game")))
			assert.Empty(t, w.outbox.all())
		})
	})

	t.Run("GIVEN sending off (a test server on a copy of the config)", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(false))
		w.connect(t, ctx, "Phone", true, event.KindCollectionAdded, event.KindIssueOpened)

		t.Run("WHEN events are published THEN nothing is queued, not even games added by hand", func(t *testing.T) {
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginSource, "Example Game")))
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginManual, "Another Game")))
			w.clock.Add(time.Hour)
			w.svc.FlushBatches(ctx)
			assert.Empty(t, w.outbox.all())
		})
	})

	t.Run("GIVEN sending left at its default", func(t *testing.T) {
		w := newWorld(t)
		w.connect(t, ctx, "Phone", true, event.KindCollectionAdded)

		t.Run("WHEN an event is published THEN nothing is queued", func(t *testing.T) {
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginSource, "Example Game")))
			assert.Empty(t, w.outbox.all())
		})
	})
}

func TestPublish_batchesGamesAddedByHand(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN sending on and a connection subscribed to collection.added", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(true))
		w.connect(t, ctx, "Phone", true, event.KindCollectionAdded)

		t.Run("WHEN two games are added by hand 4 minutes apart", func(t *testing.T) {
			first := added(event.OriginManual, "Example Game")
			first.CopiesAdded = 1
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), first))
			w.clock.Add(4 * time.Minute)
			w.svc.FlushBatches(ctx)
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginManual, "Another Game")))

			t.Run("THEN nothing is queued until 10 minutes after the second", func(t *testing.T) {
				w.clock.Add(9*time.Minute + 59*time.Second)
				w.svc.FlushBatches(ctx)
				assert.Empty(t, w.outbox.all())

				t.Run("AND then one delivery holds both games", func(t *testing.T) {
					w.clock.Add(time.Second)
					w.svc.FlushBatches(ctx)

					ds := w.outbox.all()
					require.Len(t, ds, 1)
					assert.Equal(t, event.KindCollectionAdded, ds[0].Event.Kind)

					payload, ok := ds[0].Event.Data.(event.CollectionAdded)
					require.True(t, ok)
					assert.Equal(t, event.OriginManual, payload.Origin)
					assert.Equal(t, 1, payload.CopiesAdded)
					require.Len(t, payload.Games, 2)
					assert.Equal(t, "Example Game", payload.Games[0].Title)
					assert.Equal(t, "Another Game", payload.Games[1].Title)

					t.Run("AND a later flush queues nothing more", func(t *testing.T) {
						w.clock.Add(time.Hour)
						w.svc.FlushBatches(ctx)
						assert.Len(t, w.outbox.all(), 1)
					})
				})
			})
		})
	})
}

func TestPublish_ownIssue(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN two connections subscribed to issue.opened", func(t *testing.T) {
		w := newWorld(t, notify.WithSending(true))
		broken := w.connect(t, ctx, "Broken", true, event.KindIssueOpened)
		other := w.connect(t, ctx, "Other", true, event.KindIssueOpened)

		t.Run("WHEN the first one's own issue opens", func(t *testing.T) {
			w.svc.Publish(ctx, event.New(event.KindIssueOpened, w.clock.Now(), event.Issue{
				Key:    notify.ConnectionIssueKey(broken.ID()),
				Kind:   "connector",
				Reason: "connector_paused",
			}))

			t.Run("THEN it is queued for the other only", func(t *testing.T) {
				ds := w.outbox.all()
				require.Len(t, ds, 1)
				assert.Equal(t, other.ID(), ds[0].Connection)
				assert.Equal(t, "connector:"+string(broken.ID()), notify.ConnectionIssueKey(broken.ID()))
			})
		})
	})
}
