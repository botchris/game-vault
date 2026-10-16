package notify

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"gamevault/internal/application/events"
	"gamevault/internal/application/health"
	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/event"
)

const (
	// watchEvery is how often the watcher compares Health with what it told. Health's checks are
	// cheap local reads, so a fixed rhythm is fine: nothing here reaches the network.
	watchEvery = time.Minute

	// watchMargin is how many consecutive checks a problem must be seen in to be told as open, so a
	// source failing once does not notify.
	watchMargin = 2

	// resolveAfter is how long a told problem must be absent, without a break, to be told as
	// resolved. Health keeps some problems only in memory (provider statuses that expire, keep-alive
	// failures): they vanish on a restart or on a timer without being fixed, so a short absence
	// is no news. A told problem that comes back starts the wait again.
	resolveAfter = 30 * time.Minute

	// startGrace is how long after the watcher starts nothing is told as resolved: the problems
	// Health keeps in memory are gone right after a restart, fixed or not.
	startGrace = 30 * time.Minute
)

// WatcherOption configures a Watcher.
type WatcherOption func(*Watcher)

// WithWatcherClock sets the clock (time.Now by default).
func WithWatcherClock(now port.Clock) WatcherOption { return func(w *Watcher) { w.now = now } }

// WithWatcherLogger sets the logger (discarded by default).
func WithWatcherLogger(log *slog.Logger) WatcherOption { return func(w *Watcher) { w.log = log } }

// WithWatcherRhythm sets how the watcher waits between checks (a real timer by default).
func WithWatcherRhythm(r unattended.Rhythm) WatcherOption {
	return func(w *Watcher) { w.rhythm = r }
}

// Watcher tells the problems Health finds as they open and close. The issues told as open are kept
// in IssueMemory, so a restart neither repeats them nor forgets to tell they were resolved; the
// very first run tells nothing and only remembers what is already wrong.
type Watcher struct {
	issues Issues
	memory IssueMemory
	pub    events.Publisher
	now    port.Clock
	log    *slog.Logger
	rhythm unattended.Rhythm

	// mu guards the state below: Run and a test's Check never overlap, but nothing forbids it.
	mu sync.Mutex

	// loaded is true once seeded and notified were read from the memory.
	loaded bool
	seeded bool

	// started is when the watcher was made, for startGrace.
	started time.Time

	// notified are the issues told as open, by key, as the memory keeps them.
	notified map[string]event.Issue

	// seen counts the consecutive checks each problem not told yet was present in (a new key, or a
	// told key whose reason changed).
	seen map[string]int

	// missingSince is when each told problem was first seen absent, in the current absence.
	missingSince map[string]time.Time
}

// NewWatcher returns a watcher of issues that remembers in memory and publishes through pub.
func NewWatcher(issues Issues, memory IssueMemory, pub events.Publisher, opts ...WatcherOption) *Watcher {
	w := &Watcher{
		issues:       issues,
		memory:       memory,
		pub:          pub,
		now:          time.Now,
		log:          slog.New(slog.DiscardHandler),
		seen:         map[string]int{},
		missingSince: map[string]time.Time{},
	}

	for _, opt := range opts {
		opt(w)
	}

	w.started = w.now()

	return w
}

// Run checks every minute until ctx ends, the first time at once.
func (w *Watcher) Run(ctx context.Context) {
	for {
		if err := w.Check(ctx); err != nil {
			w.log.Warn("watching health", "error", err)
		}

		if !w.rhythm.Wait(ctx, watchEvery) {
			return
		}
	}
}

// Check compares Health's issues with the ones told once, and publishes those that opened for
// watchMargin checks in a row and those that have been gone for resolveAfter. A problem whose
// reason changed is a new problem: the new one is told when it lasts, and the old one is never told
// as resolved, since the thing still does not work. A rate limit is never told: it ends by itself.
func (w *Watcher) Check(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.load(ctx); err != nil {
		return err
	}

	current := map[string]event.Issue{}
	order := []string{}

	for _, is := range w.issues.Issues(ctx) {
		if _, dup := current[is.Key]; !dup {
			order = append(order, is.Key)
		}

		current[is.Key] = issueOf(is)
	}

	if !w.seeded {
		return w.seed(ctx, current)
	}

	var tell []event.Event

	now := w.now()

	for _, key := range order {
		is := current[key]
		delete(w.missingSince, key)

		prev, told := w.notified[key]
		if (told && prev.Reason == is.Reason) || selfHealing(is) {
			// A rate limit on a told problem keeps it as it was: the problem is not news, and it is
			// not resolved either.
			delete(w.seen, key)

			continue
		}

		w.seen[key]++
		if w.seen[key] < watchMargin {
			continue
		}

		delete(w.seen, key)
		w.notified[key] = is
		tell = append(tell, event.New(event.KindIssueOpened, now, is))
	}

	// A problem not told yet that went away starts counting again when it comes back.
	for key := range w.seen {
		if _, ok := current[key]; !ok {
			delete(w.seen, key)
		}
	}

	changed := len(tell) > 0

	for _, key := range slices.Sorted(maps.Keys(w.notified)) {
		if _, ok := current[key]; ok {
			continue
		}

		since, ok := w.missingSince[key]
		if !ok {
			since = now
			w.missingSince[key] = now
		}

		if now.Sub(since) < resolveAfter || now.Sub(w.started) < startGrace {
			continue
		}

		// A rate limit remembered by an older version goes quietly, like it came.
		if !selfHealing(w.notified[key]) {
			tell = append(tell, event.New(event.KindIssueResolved, now, w.notified[key]))
		}

		delete(w.notified, key)
		delete(w.missingSince, key)

		changed = true
	}

	if !changed {
		return nil
	}

	// Tell even when saving fails: the change is real, and this watcher's memory is right until
	// a restart (which may then tell it again).
	err := w.memory.SaveNotified(ctx, w.notified)

	for _, e := range tell {
		w.pub.Publish(ctx, e)
	}

	if err != nil {
		return fmt.Errorf("saving the issues told: %w", err)
	}

	return nil
}

// load reads the memory once.
func (w *Watcher) load(ctx context.Context) error {
	if w.loaded {
		return nil
	}

	seeded, err := w.memory.Seeded(ctx)
	if err != nil {
		return fmt.Errorf("reading whether the watcher ran: %w", err)
	}

	notified, err := w.memory.Notified(ctx)
	if err != nil {
		return fmt.Errorf("reading the issues told: %w", err)
	}

	if notified == nil {
		notified = map[string]event.Issue{}
	}

	w.loaded, w.seeded, w.notified = true, seeded, notified

	return nil
}

// seed remembers the current issues as told without telling them: what was wrong before the
// watcher ever ran is no news.
func (w *Watcher) seed(ctx context.Context, current map[string]event.Issue) error {
	if err := w.memory.SaveNotified(ctx, current); err != nil {
		return fmt.Errorf("saving the issues told: %w", err)
	}

	if err := w.memory.MarkSeeded(ctx); err != nil {
		return fmt.Errorf("recording that the watcher ran: %w", err)
	}

	w.seeded, w.notified = true, current

	return nil
}

// selfHealing reports whether a problem ends by itself, so telling it would only be noise.
func selfHealing(is event.Issue) bool {
	return is.Reason == string(health.ReasonProviderRateLimited)
}

// issueOf is a Health issue as an event's payload, field by field.
func issueOf(is health.Issue) event.Issue {
	return event.Issue{
		Key:      is.Key,
		Level:    string(is.Level),
		Kind:     string(is.Kind),
		Source:   is.Source,
		Reason:   string(is.Reason),
		Params:   maps.Clone(is.Params),
		Message:  is.Message,
		Since:    is.Since,
		Target:   string(is.Target),
		TargetID: is.TargetID,
	}
}
