package notify_test

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/health"
	"gamevault/internal/application/notify"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/event"
)

// fakeIssues is Health with an answer the test changes between checks.
type fakeIssues struct {
	mu     sync.Mutex
	issues []health.Issue
}

func (f *fakeIssues) Issues(context.Context) []health.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]health.Issue(nil), f.issues...)
}

func (f *fakeIssues) set(issues ...health.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.issues = issues
}

// memIssueMemory is the watcher's memory in memory; it outlives a watcher, like the database.
type memIssueMemory struct {
	mu       sync.Mutex
	notified map[string]event.Issue
	seeded   bool
}

func (m *memIssueMemory) Notified(context.Context) (map[string]event.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Clone(m.notified), nil
}

func (m *memIssueMemory) SaveNotified(_ context.Context, issues map[string]event.Issue) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.notified = maps.Clone(issues)

	return nil
}

func (m *memIssueMemory) Seeded(context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.seeded, nil
}

func (m *memIssueMemory) MarkSeeded(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.seeded = true

	return nil
}

func (m *memIssueMemory) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Sorted(maps.Keys(m.notified))
}

// recorder is a Publisher that keeps what it was given.
type recorder struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *recorder) Publish(_ context.Context, e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, e)
}

func (r *recorder) all() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]event.Event(nil), r.events...)
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = nil
}

var (
	signedOut = health.Issue{
		Key:      "source:src-1",
		Level:    health.LevelError,
		Kind:     health.KindSource,
		Source:   "Example Store",
		Reason:   health.ReasonSourceSignedOut,
		Params:   map[string]string{"code": "401"},
		Message:  "signed out: sign in again",
		Since:    time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC),
		Target:   health.TargetSources,
		TargetID: "src-1",
	}
	diskLow = health.Issue{
		Key:     "disk:config",
		Level:   health.LevelWarning,
		Kind:    health.KindDisk,
		Reason:  health.ReasonDiskLow,
		Message: "little space left",
	}
	staleBackup = health.Issue{
		Key:    "backups",
		Level:  health.LevelWarning,
		Kind:   health.KindBackups,
		Reason: health.ReasonBackupStale,
		Params: map[string]string{"days": "9"},
	}
)

func newWatcher(issues *fakeIssues, memory *memIssueMemory, pub *recorder, clk *clock) *notify.Watcher {
	return notify.NewWatcher(issues, memory, pub, notify.WithWatcherClock(clk.Now))
}

// resolveAfter mirrors the watcher's rule: a told issue resolves after this long missing.
const resolveAfter = 30 * time.Minute

func TestWatcher(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a first run with two current issues", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		issues.set(signedOut, diskLow)
		w := newWatcher(issues, memory, pub, clk)

		t.Run("WHEN it checks THEN nothing is published and both are remembered as told", func(t *testing.T) {
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
			assert.Equal(t, []string{"disk:config", "source:src-1"}, memory.keys())
			assert.True(t, memory.seeded)
		})
	})

	t.Run("GIVEN a seeded watcher", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))

		t.Run("WHEN a new issue appears in one check THEN nothing is published", func(t *testing.T) {
			issues.set(signedOut)
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
		})

		t.Run("WHEN it is still there in the next check THEN it is told once, with its fields", func(t *testing.T) {
			require.NoError(t, w.Check(ctx))

			got := pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindIssueOpened, got[0].Kind)
			assert.Equal(t, at, got[0].At)
			assert.Equal(t, event.Issue{
				Key:      "source:src-1",
				Level:    "error",
				Kind:     "source",
				Source:   "Example Store",
				Reason:   "source_signed_out",
				Params:   map[string]string{"code": "401"},
				Message:  "signed out: sign in again",
				Since:    signedOut.Since,
				Target:   "sources",
				TargetID: "src-1",
			}, got[0].Data)
			assert.Equal(t, []string{"source:src-1"}, memory.keys())
		})

		t.Run("WHEN it stays THEN it is not told again", func(t *testing.T) {
			pub.reset()
			require.NoError(t, w.Check(ctx))
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
		})

		t.Run("WHEN it is missing for just under half an hour THEN nothing is published", func(t *testing.T) {
			issues.set()
			require.NoError(t, w.Check(ctx))
			clk.Add(resolveAfter - time.Minute)
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
		})

		t.Run("WHEN it has been missing for half an hour THEN it is told as resolved", func(t *testing.T) {
			clk.Add(time.Minute)
			require.NoError(t, w.Check(ctx))

			got := pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindIssueResolved, got[0].Kind)

			data, ok := got[0].Data.(event.Issue)
			require.True(t, ok)
			assert.Equal(t, "source:src-1", data.Key)
			assert.Empty(t, memory.keys())
		})
	})

	t.Run("GIVEN a seeded watcher and a new issue that flaps (present, absent, present)", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))

		for _, present := range []bool{true, false, true, false} {
			if present {
				issues.set(diskLow)
			} else {
				issues.set()
			}

			require.NoError(t, w.Check(ctx))
		}

		t.Run("THEN nothing is published", func(t *testing.T) {
			assert.Empty(t, pub.all())
		})
	})

	t.Run("GIVEN a told issue that flaps (absent, present, absent)", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		issues.set(diskLow)
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))

		for _, present := range []bool{false, true, false, true} {
			if present {
				issues.set(diskLow)
			} else {
				issues.set()
			}

			require.NoError(t, w.Check(ctx))
		}

		t.Run("THEN it is never told as resolved", func(t *testing.T) {
			assert.Empty(t, pub.all())
			assert.Equal(t, []string{"disk:config"}, memory.keys())
		})
	})

	t.Run("GIVEN a told issue whose reason changes", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		issues.set(signedOut)
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))

		failed := signedOut
		failed.Reason = health.ReasonSourceSyncFailed
		failed.Message = "the scan failed"
		issues.set(failed)

		t.Run("WHEN the new reason is seen once THEN nothing is published", func(t *testing.T) {
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
		})

		t.Run("WHEN it is seen twice THEN only the new problem is told, never the old one as resolved", func(t *testing.T) {
			require.NoError(t, w.Check(ctx))

			got := pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindIssueOpened, got[0].Kind)

			data, ok := got[0].Data.(event.Issue)
			require.True(t, ok)
			assert.Equal(t, "source_sync_failed", data.Reason)

			told, err := memory.Notified(ctx)
			require.NoError(t, err)
			assert.Equal(t, "source_sync_failed", told["source:src-1"].Reason)
		})
	})

	t.Run("GIVEN a told issue WHEN the server restarts and a new watcher still sees it", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))
		issues.set(staleBackup)
		require.NoError(t, w.Check(ctx))
		require.NoError(t, w.Check(ctx))
		require.Len(t, pub.all(), 1)
		pub.reset()

		restarted := newWatcher(issues, memory, pub, clk)
		require.NoError(t, restarted.Check(ctx))
		require.NoError(t, restarted.Check(ctx))

		t.Run("THEN nothing is told again", func(t *testing.T) {
			assert.Empty(t, pub.all())
		})

		t.Run("AND once it has been gone for half an hour, it is told as resolved", func(t *testing.T) {
			issues.set()
			require.NoError(t, restarted.Check(ctx))
			clk.Add(resolveAfter)
			require.NoError(t, restarted.Check(ctx))

			got := pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindIssueResolved, got[0].Kind)
		})
	})
}

func TestWatcher_inMemoryIssues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	rateLimited := health.Issue{
		Key:    "provider:example-covers",
		Level:  health.LevelWarning,
		Kind:   health.KindProvider,
		Source: "Example Covers",
		Reason: health.ReasonProviderRateLimited,
	}

	t.Run("GIVEN a told issue that Health forgets on a restart", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		issues.set(signedOut)
		require.NoError(t, newWatcher(issues, memory, pub, clk).Check(ctx))

		issues.set()
		restarted := newWatcher(issues, memory, pub, clk)

		t.Run("WHEN the new watcher checks every minute for just under half an hour", func(t *testing.T) {
			for range 30 {
				require.NoError(t, restarted.Check(ctx))
				clk.Add(time.Minute - time.Second)
			}

			t.Run("THEN it is not told as resolved", func(t *testing.T) {
				assert.Empty(t, pub.all())
				assert.Equal(t, []string{"source:src-1"}, memory.keys())
			})
		})

		t.Run("WHEN it comes back THEN nothing is told either", func(t *testing.T) {
			issues.set(signedOut)
			clk.Add(time.Hour)
			require.NoError(t, restarted.Check(ctx))
			assert.Empty(t, pub.all())
		})
	})

	t.Run("GIVEN a told issue that disappears for five minutes", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		issues.set(diskLow)
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))
		clk.Add(time.Hour)

		issues.set()

		for range 5 {
			require.NoError(t, w.Check(ctx))
			clk.Add(time.Minute)
		}

		issues.set(diskLow)
		require.NoError(t, w.Check(ctx))

		t.Run("WHEN it then disappears again for less than half an hour", func(t *testing.T) {
			issues.set()

			for range 29 {
				require.NoError(t, w.Check(ctx))
				clk.Add(time.Minute)
			}

			t.Run("THEN nothing is published: the blip reset the count", func(t *testing.T) {
				assert.Empty(t, pub.all())
			})
		})
	})

	t.Run("GIVEN a seeded watcher WHEN a provider is rate limited for a while and then recovers", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))
		issues.set(rateLimited)

		for range 10 {
			clk.Add(time.Minute)
			require.NoError(t, w.Check(ctx))
		}

		issues.set()

		for range 40 {
			clk.Add(time.Minute)
			require.NoError(t, w.Check(ctx))
		}

		t.Run("THEN nothing is published: it ends by itself", func(t *testing.T) {
			assert.Empty(t, pub.all())
			assert.Empty(t, memory.keys())
		})
	})

	t.Run("GIVEN a watcher that told an issue opened", func(t *testing.T) {
		issues, memory, pub, clk := &fakeIssues{}, &memIssueMemory{}, &recorder{}, &clock{now: at}
		w := newWatcher(issues, memory, pub, clk)
		require.NoError(t, w.Check(ctx))
		issues.set(signedOut)
		require.NoError(t, w.Check(ctx))
		require.NoError(t, w.Check(ctx))
		require.Len(t, pub.all(), 1)
		pub.reset()

		t.Run("WHEN it is really fixed and stays gone for half an hour", func(t *testing.T) {
			issues.set()
			require.NoError(t, w.Check(ctx))

			for range 30 {
				clk.Add(time.Minute)
				require.NoError(t, w.Check(ctx))
			}

			t.Run("THEN it is told as resolved once", func(t *testing.T) {
				got := pub.all()
				require.Len(t, got, 1)
				assert.Equal(t, event.KindIssueResolved, got[0].Kind)
				assert.Equal(t, at.Add(resolveAfter), got[0].At)
			})
		})
	})
}

func TestWatcher_Run(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a watcher with an instant rhythm", func(t *testing.T) {
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		issues.set(diskLow)

		var waits []time.Duration

		w := notify.NewWatcher(issues, memory, pub, notify.WithWatcherRhythm(unattended.Rhythm{
			Sleep: func(_ context.Context, d time.Duration) bool {
				waits = append(waits, d)

				return len(waits) < 3
			},
		}))

		t.Run("WHEN it runs until its context ends THEN it checks every minute", func(t *testing.T) {
			w.Run(ctx)
			assert.Equal(t, []time.Duration{time.Minute, time.Minute, time.Minute}, waits)
			assert.True(t, memory.seeded)
		})
	})
}
