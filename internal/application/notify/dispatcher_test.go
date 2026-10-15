package notify_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/notify"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

// pacedConnector is the fake connector with a pace and a script of send errors; it is safe to use
// from the dispatcher's goroutine.
type pacedConnector struct {
	*fakeConnector

	gap time.Duration

	mu   sync.Mutex
	errs []error
	got  []notify.Message
	sent chan string

	// onSend runs inside Send, before it answers (a change made while the message is on its way).
	onSend func()
}

func newPacedConnector(gap time.Duration) *pacedConnector {
	return &pacedConnector{
		fakeConnector: &fakeConnector{typ: "fake"},
		gap:           gap,
		sent:          make(chan string, 64),
	}
}

func (p *pacedConnector) Gap() time.Duration { return p.gap }

func (p *pacedConnector) Send(_ context.Context, _ schema.Settings, m notify.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.onSend != nil {
		p.onSend()
	}

	var err error
	if len(p.errs) > 0 {
		err, p.errs = p.errs[0], p.errs[1:]
	}

	if err == nil {
		p.got = append(p.got, m)
		p.sent <- releaseTitle(m)
	}

	return err
}

// fail makes the next sends fail with errs, in order.
func (p *pacedConnector) fail(errs ...error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.errs = append(p.errs, errs...)
}

func (p *pacedConnector) titles() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]string, 0, len(p.got))
	for _, m := range p.got {
		out = append(out, releaseTitle(m))
	}

	return out
}

func releaseTitle(m notify.Message) string {
	if r, ok := m.Event.Data.(event.Release); ok {
		return r.Title
	}

	return string(m.Event.Kind)
}

// fakeSleep is the dispatcher's clock of waits: each wait moves the world's clock on and is
// recorded.
type fakeSleep struct {
	mu     sync.Mutex
	clock  *clock
	waited []time.Duration
}

func (f *fakeSleep) rhythm() unattended.Rhythm {
	return unattended.Rhythm{
		Sleep: func(ctx context.Context, d time.Duration) bool {
			f.mu.Lock()
			f.waited = append(f.waited, d)
			f.mu.Unlock()
			f.clock.Add(d)

			return ctx.Err() == nil
		},
	}
}

func (f *fakeSleep) waits() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]time.Duration(nil), f.waited...)
}

// dispatchWorld is a world whose connector is paced and whose waits are instant.
type dispatchWorld struct {
	*world

	paced *pacedConnector
	sleep *fakeSleep
	logs  *syncBuffer
}

func newDispatchWorld(t *testing.T, gap time.Duration) *dispatchWorld {
	t.Helper()

	paced := newPacedConnector(gap)
	sleep := &fakeSleep{}
	logs := &syncBuffer{}

	w := newWorld(t,
		notify.WithSending(true),
		notify.WithConnectors(paced),
		notify.WithRhythm(sleep.rhythm()),
		notify.WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
	)
	sleep.clock = w.clock

	return &dispatchWorld{
		world: w,
		paced: paced,
		sleep: sleep,
		logs:  logs,
	}
}

// queue puts a release.out delivery for c into the outbox, created age ago and due now.
func (w *dispatchWorld) queue(t *testing.T, ctx context.Context, c *connection.Connection, title string, age time.Duration) string {
	t.Helper()

	created := w.clock.Now().Add(-age)
	id := fmt.Sprintf("d-%s", title)
	require.NoError(t, w.outbox.Enqueue(ctx, []notify.Delivery{{
		ID:         id,
		Connection: c.ID(),
		Event:      event.New(event.KindReleaseOut, created, event.Release{Title: title}),
		State:      notify.StateQueued,
		NextAt:     created,
		CreatedAt:  created,
		UpdatedAt:  created,
	}}))

	return id
}

func (w *dispatchWorld) delivery(t *testing.T, id string) notify.Delivery {
	t.Helper()

	for _, d := range w.outbox.all() {
		if d.ID == id {
			return d
		}
	}

	t.Fatalf("no delivery %s", id)

	return notify.Delivery{}
}

// syncBuffer is a log destination safe for the dispatcher's goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// rateLimited is a 429-style error that says how long to wait.
type rateLimited struct{ wait time.Duration }

func (e rateLimited) Error() string { return "HTTP 429" }

func (e rateLimited) RetryAfter() time.Duration { return e.wait }

func TestDispatcher_pace(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN two due deliveries for one connection paced at 3 s", func(t *testing.T) {
		w := newDispatchWorld(t, 3*time.Second)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		first := w.queue(t, ctx, c, "Example One", time.Minute)
		second := w.queue(t, ctx, c, "Example Two", 30*time.Second)

		t.Run("WHEN the dispatcher runs one round", func(t *testing.T) {
			w.svc.Round(ctx)

			t.Run("THEN both are sent, in order, 3 s apart", func(t *testing.T) {
				assert.Equal(t, []string{"Example One", "Example Two"}, w.paced.titles())
				assert.Equal(t, []time.Duration{3 * time.Second}, w.sleep.waits())
			})

			t.Run("AND both are marked sent", func(t *testing.T) {
				assert.Equal(t, notify.StateSent, w.delivery(t, first).State)
				assert.Equal(t, notify.StateSent, w.delivery(t, second).State)
			})
		})
	})

	t.Run("GIVEN a connector without a pace", func(t *testing.T) {
		sleep := &fakeSleep{}
		w := newWorld(t, notify.WithSending(true), notify.WithRhythm(sleep.rhythm()))
		sleep.clock = w.clock
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		dw := &dispatchWorld{
			world: w,
			sleep: sleep,
		}
		dw.queue(t, ctx, c, "Example One", time.Minute)
		dw.queue(t, ctx, c, "Example Two", time.Minute)

		t.Run("WHEN the dispatcher runs one round THEN it leaves a second between messages", func(t *testing.T) {
			w.svc.Round(ctx)

			assert.Len(t, w.connector.sent, 2)
			assert.Equal(t, []time.Duration{time.Second}, sleep.waits())
		})
	})
}

func TestDispatcher_retries(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a connector that keeps failing with a plain error", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", 0)
		w.paced.fail(errors.New("not answering"), errors.New("not answering"), errors.New("not answering"),
			errors.New("not answering"), errors.New("not answering"), errors.New("not answering"))

		t.Run("WHEN it is tried each time it is due", func(t *testing.T) {
			waits := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

			t.Run("THEN it is retried after 1 min, 5 min, 30 min, 2 h and 6 h", func(t *testing.T) {
				for i, wait := range waits {
					start := w.clock.Now()
					w.svc.Round(ctx)

					d := w.delivery(t, id)
					require.Equal(t, notify.StateQueued, d.State, "attempt %d", i+1)
					assert.Equal(t, i+1, d.Attempts)
					assert.Equal(t, start.Add(wait), d.NextAt, "attempt %d", i+1)
					assert.Equal(t, "not answering", d.LastError)

					w.clock.Add(wait)
				}
			})

			t.Run("AND after the sixth failure it is given up", func(t *testing.T) {
				w.svc.Round(ctx)

				d := w.delivery(t, id)
				assert.Equal(t, notify.StateFailed, d.State)
				assert.Equal(t, "not answering", d.LastError)
			})

			t.Run("AND the log names the connection, never its settings", func(t *testing.T) {
				logs := w.logs.String()
				assert.Contains(t, logs, "Phone")
				assert.NotContains(t, logs, "t-123")
			})
		})
	})

	t.Run("GIVEN a connector asking to wait longer than the next retry", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", 0)
		w.paced.fail(rateLimited{wait: 10 * time.Minute})

		t.Run("WHEN it fails THEN the next attempt waits what it asked", func(t *testing.T) {
			start := w.clock.Now()
			w.svc.Round(ctx)

			assert.Equal(t, start.Add(10*time.Minute), w.delivery(t, id).NextAt)
		})
	})

	t.Run("GIVEN a connector asking to wait a whole day", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", 0)
		w.paced.fail(rateLimited{wait: 24 * time.Hour})

		t.Run("WHEN it fails THEN the next attempt waits 6 h at most", func(t *testing.T) {
			start := w.clock.Now()
			w.svc.Round(ctx)

			assert.Equal(t, start.Add(6*time.Hour), w.delivery(t, id).NextAt)
		})
	})

	t.Run("GIVEN a connector asking to wait less than the next retry", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", 0)
		w.paced.fail(rateLimited{wait: 5 * time.Second})

		t.Run("WHEN it fails THEN the scheduled retry stands", func(t *testing.T) {
			start := w.clock.Now()
			w.svc.Round(ctx)

			assert.Equal(t, start.Add(time.Minute), w.delivery(t, id).NextAt)
		})
	})
}

func TestDispatcher_permanentFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a connection with three due deliveries", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		first := w.queue(t, ctx, c, "Example One", 3*time.Minute)
		second := w.queue(t, ctx, c, "Example Two", 2*time.Minute)
		third := w.queue(t, ctx, c, "Example Three", time.Minute)

		t.Run("WHEN the connector rejects the first for good", func(t *testing.T) {
			w.paced.fail(fmt.Errorf("the service rejected the token: %w", notify.ErrPermanent))
			w.svc.Round(ctx)

			t.Run("THEN that delivery is given up", func(t *testing.T) {
				assert.Equal(t, notify.StateFailed, w.delivery(t, first).State)
			})

			t.Run("AND the connection is paused with the reason, and saved", func(t *testing.T) {
				got, err := w.svc.Get(ctx, c.ID())
				require.NoError(t, err)
				assert.True(t, got.Paused())
				assert.Contains(t, got.PausedWhy(), "the service rejected the token")
				assert.Equal(t, w.clock.Now(), got.PausedAt())
			})

			t.Run("AND its other deliveries stay queued, untried", func(t *testing.T) {
				assert.Equal(t, notify.StateQueued, w.delivery(t, second).State)
				assert.Equal(t, notify.StateQueued, w.delivery(t, third).State)
				assert.Empty(t, w.paced.titles())
			})

			t.Run("AND a later round while paused sends nothing", func(t *testing.T) {
				w.svc.Round(ctx)

				assert.Empty(t, w.paced.titles())
				assert.Equal(t, notify.StateQueued, w.delivery(t, second).State)
			})
		})

		t.Run("WHEN the connection is unpaused and a round runs", func(t *testing.T) {
			got, err := w.svc.Get(ctx, c.ID())
			require.NoError(t, err)
			got.Unpause()
			require.NoError(t, w.conns.Save(ctx, got))

			w.svc.Round(ctx)

			t.Run("THEN the waiting deliveries are sent, in order", func(t *testing.T) {
				assert.Equal(t, []string{"Example Two", "Example Three"}, w.paced.titles())
				assert.Equal(t, notify.StateSent, w.delivery(t, third).State)
			})
		})
	})
}

func TestDispatcher_permanentFailureRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a delivery whose send fails for good", func(t *testing.T) {
		t.Run("WHEN the user saves new settings while it is on its way", func(t *testing.T) {
			w := newDispatchWorld(t, 0)
			c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
			id := w.queue(t, ctx, c, "Example One", time.Minute)
			w.paced.fail(fmt.Errorf("the service rejected the token: %w", notify.ErrPermanent))
			w.paced.onSend = func() {
				_, err := w.svc.Save(ctx, c.ID(), "", connection.Config{
					Name:     "Phone",
					Enabled:  true,
					Events:   []event.Kind{event.KindReleaseOut},
					Language: "en",
					Settings: schema.Settings{
						"token": "t-456",
						"chat":  "42",
					},
				})
				require.NoError(t, err)
			}

			w.svc.Round(ctx)

			t.Run("THEN the delivery is given up but the connection keeps the new settings, unpaused", func(t *testing.T) {
				assert.Equal(t, notify.StateFailed, w.delivery(t, id).State)

				got, err := w.svc.Get(ctx, c.ID())
				require.NoError(t, err)
				assert.False(t, got.Paused())
				assert.Equal(t, "t-456", got.Settings()["token"])
			})
		})

		t.Run("WHEN the user deletes the connection while it is on its way", func(t *testing.T) {
			w := newDispatchWorld(t, 0)
			c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
			w.queue(t, ctx, c, "Example One", time.Minute)
			w.paced.fail(fmt.Errorf("the service rejected the token: %w", notify.ErrPermanent))
			w.paced.onSend = func() { require.NoError(t, w.conns.Delete(ctx, c.ID())) }

			w.svc.Round(ctx)

			t.Run("THEN it is not brought back", func(t *testing.T) {
				_, err := w.svc.Get(ctx, c.ID())
				assert.ErrorIs(t, err, connection.ErrNotFound)
			})
		})
	})
}

func TestDispatcher_shutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a send that fails because the server is stopping", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", time.Minute)
		w.queue(t, ctx, c, "Example Two", time.Minute)

		roundCtx, stop := context.WithCancel(ctx)

		w.paced.fail(context.Canceled)
		w.paced.onSend = stop

		w.svc.Round(roundCtx)

		t.Run("THEN the delivery stays queued, no attempt counted, nothing logged, the rest untried", func(t *testing.T) {
			d := w.delivery(t, id)
			assert.Equal(t, notify.StateQueued, d.State)
			assert.Zero(t, d.Attempts)
			assert.NotContains(t, w.logs.String(), "sending a notification")
			assert.Empty(t, w.paced.titles())
		})
	})

	t.Run("GIVEN a send that arrives just as the server is stopping", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", time.Minute)

		roundCtx, stop := context.WithCancel(ctx)
		w.paced.onSend = stop

		w.svc.Round(roundCtx)

		t.Run("THEN it is still recorded as sent", func(t *testing.T) {
			assert.Equal(t, notify.StateSent, w.delivery(t, id).State)
		})
	})
}

func TestDispatcher_drops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a delivery created 25 h ago and a fresh one", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		old := w.queue(t, ctx, c, "Example Old", 25*time.Hour)
		fresh := w.queue(t, ctx, c, "Example Fresh", time.Hour)

		t.Run("WHEN a round runs THEN the old one is dropped, not sent, and the fresh one sent", func(t *testing.T) {
			w.svc.Round(ctx)

			assert.Equal(t, notify.StateDropped, w.delivery(t, old).State)
			assert.Equal(t, notify.StateSent, w.delivery(t, fresh).State)
			assert.Equal(t, []string{"Example Fresh"}, w.paced.titles())
		})
	})

	t.Run("GIVEN deliveries for a connection deleted after they were queued", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", time.Minute)
		require.NoError(t, w.conns.Delete(ctx, c.ID()))

		t.Run("WHEN a round runs THEN they are dropped", func(t *testing.T) {
			w.svc.Round(ctx)

			assert.Equal(t, notify.StateDropped, w.delivery(t, id).State)
			assert.Empty(t, w.paced.titles())
		})
	})

	t.Run("GIVEN deliveries for a connection turned off after they were queued", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", false, event.KindReleaseOut)
		id := w.queue(t, ctx, c, "Example One", time.Minute)

		t.Run("WHEN a round runs THEN they are dropped, not sent", func(t *testing.T) {
			w.svc.Round(ctx)

			assert.Equal(t, notify.StateDropped, w.delivery(t, id).State)
			assert.Empty(t, w.paced.titles())
		})
	})
}

func TestDispatcher_prunes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a delivery sent 8 days ago and one sent yesterday", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		c := w.connect(t, ctx, "Phone", true, event.KindReleaseOut)
		old := w.queue(t, ctx, c, "Example Old", 0)
		require.NoError(t, w.outbox.MarkSent(ctx, old, w.clock.Now().Add(-8*24*time.Hour)))
		recent := w.queue(t, ctx, c, "Example Recent", 0)
		require.NoError(t, w.outbox.MarkSent(ctx, recent, w.clock.Now().Add(-24*time.Hour)))

		t.Run("WHEN a round runs THEN the old one is removed and the recent one kept", func(t *testing.T) {
			w.svc.Round(ctx)

			all := w.outbox.all()

			ids := make([]string, 0, len(all))
			for _, d := range all {
				ids = append(ids, d.ID)
			}

			assert.Equal(t, []string{recent}, ids)
		})
	})
}

func TestDispatcher_run(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a running dispatcher whose ticks the test releases", func(t *testing.T) {
		paced := newPacedConnector(0)
		ticks := make(chan time.Duration, 8)
		release := make(chan struct{})
		w := newWorld(t,
			notify.WithSending(true),
			notify.WithConnectors(paced),
			notify.WithRhythm(unattended.Rhythm{
				Sleep: func(ctx context.Context, d time.Duration) bool {
					ticks <- d

					select {
					case <-ctx.Done():
						return false
					case <-release:
						return true
					}
				},
			}),
		)
		w.connect(t, ctx, "Phone", true, event.KindReleaseOut, event.KindCollectionAdded)

		runCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})

		go func() {
			defer close(done)

			w.svc.Run(runCtx)
		}()

		t.Run("THEN after its first round it waits 15 s", func(t *testing.T) {
			assert.Equal(t, 15*time.Second, waitFor(t, ctx, ticks))
		})

		t.Run("WHEN an event is published", func(t *testing.T) {
			w.svc.Publish(ctx, event.New(event.KindReleaseOut, w.clock.Now(), event.Release{Title: "Example Woken"}))

			t.Run("THEN it is sent at once, without waiting for the tick", func(t *testing.T) {
				assert.Equal(t, "Example Woken", waitFor(t, ctx, paced.sent))
				assert.Equal(t, 15*time.Second, waitFor(t, ctx, ticks))
			})
		})

		t.Run("WHEN a game is added by hand and the batch window passes", func(t *testing.T) {
			w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginManual, "Example Hand")))
			w.clock.Add(11 * time.Minute)

			t.Run("THEN the next tick flushes the batch and sends it", func(t *testing.T) {
				release <- struct{}{}

				assert.Equal(t, string(event.KindCollectionAdded), waitFor(t, ctx, paced.sent))
			})
		})

		t.Run("WHEN the context ends THEN Run returns", func(t *testing.T) {
			stop()

			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("Run did not return")
			}
		})
	})
}

// waitFor returns the next value on ch, failing the test when ctx ends first.
func waitFor[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatal("timed out")

		var zero T

		return zero
	}
}

func TestFlushBatches_failingQueueLogsOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game added by hand and a queue that cannot be written", func(t *testing.T) {
		w := newDispatchWorld(t, 0)
		w.connect(t, ctx, "Phone", true, event.KindCollectionAdded)
		w.outbox.enqueueErr = errors.New("database is locked")
		w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginManual, "Example Hand")))
		w.clock.Add(11 * time.Minute)

		t.Run("WHEN the batch is flushed on three ticks", func(t *testing.T) {
			for range 3 {
				w.svc.FlushBatches(ctx)
				w.clock.Add(15 * time.Second)
			}

			t.Run("THEN the failure is logged once", func(t *testing.T) {
				assert.Equal(t, 1, strings.Count(w.logs.String(), "queueing the games added by hand"))
			})
		})

		t.Run("WHEN the queue works again", func(t *testing.T) {
			w.outbox.mu.Lock()
			w.outbox.enqueueErr = nil
			w.outbox.mu.Unlock()
			w.svc.FlushBatches(ctx)

			t.Run("THEN the batch is queued", func(t *testing.T) {
				assert.Len(t, w.outbox.all(), 1)
			})

			t.Run("AND a later failure streak is logged again", func(t *testing.T) {
				w.outbox.mu.Lock()
				w.outbox.enqueueErr = errors.New("database is locked")
				w.outbox.mu.Unlock()
				w.svc.Publish(ctx, event.New(event.KindCollectionAdded, w.clock.Now(), added(event.OriginManual, "Example Later")))
				w.clock.Add(11 * time.Minute)
				w.svc.FlushBatches(ctx)
				w.svc.FlushBatches(ctx)

				assert.Equal(t, 2, strings.Count(w.logs.String(), "queueing the games added by hand"))
			})
		})
	})
}
