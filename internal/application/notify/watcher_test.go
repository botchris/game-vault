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

func newWatcher(issues *fakeIssues, memory *memIssueMemory, pub *recorder) *notify.Watcher {
	return notify.NewWatcher(issues, memory, pub, notify.WithWatcherClock(func() time.Time { return at }))
}

func TestWatcher(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a first run with two current issues", func(t *testing.T) {
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		issues.set(signedOut, diskLow)
		w := newWatcher(issues, memory, pub)

		t.Run("WHEN it checks THEN nothing is published and both are remembered as told", func(t *testing.T) {
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
			assert.Equal(t, []string{"disk:config", "source:src-1"}, memory.keys())
			assert.True(t, memory.seeded)
		})
	})

	t.Run("GIVEN a seeded watcher", func(t *testing.T) {
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		w := newWatcher(issues, memory, pub)
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

		t.Run("WHEN it is missing from one check THEN nothing is published", func(t *testing.T) {
			issues.set()
			require.NoError(t, w.Check(ctx))
			assert.Empty(t, pub.all())
		})

		t.Run("WHEN it is missing from the next check too THEN it is told as resolved", func(t *testing.T) {
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
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		w := newWatcher(issues, memory, pub)
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
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		issues.set(diskLow)
		w := newWatcher(issues, memory, pub)
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
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		issues.set(signedOut)
		w := newWatcher(issues, memory, pub)
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
		issues, memory, pub := &fakeIssues{}, &memIssueMemory{}, &recorder{}
		w := newWatcher(issues, memory, pub)
		require.NoError(t, w.Check(ctx))
		issues.set(staleBackup)
		require.NoError(t, w.Check(ctx))
		require.NoError(t, w.Check(ctx))
		require.Len(t, pub.all(), 1)
		pub.reset()

		restarted := newWatcher(issues, memory, pub)
		require.NoError(t, restarted.Check(ctx))
		require.NoError(t, restarted.Check(ctx))

		t.Run("THEN nothing is told again", func(t *testing.T) {
			assert.Empty(t, pub.all())
		})

		t.Run("AND once it goes away for two checks, it is told as resolved", func(t *testing.T) {
			issues.set()
			require.NoError(t, restarted.Check(ctx))
			require.NoError(t, restarted.Check(ctx))

			got := pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindIssueResolved, got[0].Kind)
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
