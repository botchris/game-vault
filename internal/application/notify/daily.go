package notify

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"gamevault/internal/application/events"
	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// ExpiringDays is how many days before its deadline a pending key counts as expiring. It is the
// library's EXPIRING_DAYS (web/src/lib/deadlines.ts; a test keeps them equal), so a message about
// keys expiring names the keys the "Expiring" filter shows.
const ExpiringDays = 30

// KeyThresholds are the days left at which a key is told: on entering the window, then a week and a
// day before its deadline. A key is told once per threshold.
var KeyThresholds = []int{30, 7, 1}

const (
	// dailyFromHour is the local hour the daily job's window opens at, and dailyWindow how long it
	// stays open: the job runs at a random moment of it each day.
	dailyFromHour = 9
	dailyWindow   = time.Hour

	// dateLayout is the layout of game.Date.
	dateLayout = "2006-01-02"
)

// ReleaseSource is the home page's stored list of upcoming releases from the user's series.
type ReleaseSource interface {
	// SeriesReleases returns the releases "from your series", as last fetched.
	SeriesReleases(ctx context.Context) ([]game.Release, error)
}

// Interest is an optional capability of the publisher the daily job is given: whether anybody
// would receive an event of a kind now. The job tells keys only then, since a key it tells is
// remembered as told and never told again at that threshold.
type Interest interface {
	// Wants reports whether an event of kind published now would reach anybody.
	Wants(ctx context.Context, kind event.Kind) bool
}

// DailyOption configures a Daily.
type DailyOption func(*Daily)

// WithDailyClock sets the clock (time.Now by default).
func WithDailyClock(now port.Clock) DailyOption { return func(d *Daily) { d.now = now } }

// WithDailyLogger sets the logger (discarded by default).
func WithDailyLogger(log *slog.Logger) DailyOption { return func(d *Daily) { d.log = log } }

// WithDailyRhythm sets how the job draws its moment and waits for it (math/rand and a real timer by
// default).
func WithDailyRhythm(r unattended.Rhythm) DailyOption { return func(d *Daily) { d.rhythm = r } }

// WithDailyLocation sets the time zone days are counted in (the server's, time.Local, by default).
func WithDailyLocation(loc *time.Location) DailyOption { return func(d *Daily) { d.loc = loc } }

// Daily is the job that runs once a day: it tells the pending keys whose deadline comes near and
// the releases from the user's series that come out that day.
type Daily struct {
	catalog  Catalog
	keys     KeyMemory
	releases ReleaseSource
	pub      events.Publisher
	now      port.Clock
	log      *slog.Logger
	rhythm   unattended.Rhythm
	loc      *time.Location

	// interest is pub's Interest, or nil when it has none (everything is wanted then).
	interest Interest

	// mu guards toldReleases.
	mu sync.Mutex

	// toldReleases are the releases told as out, by title and date. It lives in memory only: a
	// restart on the day of a release may tell it once more, which is no worse than a repeated
	// headline.
	toldReleases map[string]bool
}

// NewDaily returns the daily job over catalog's keys and releases' list, remembering in keys and
// publishing through pub.
func NewDaily(catalog Catalog, keys KeyMemory, releases ReleaseSource, pub events.Publisher, opts ...DailyOption) *Daily {
	d := &Daily{
		catalog:      catalog,
		keys:         keys,
		releases:     releases,
		pub:          pub,
		now:          time.Now,
		log:          slog.New(slog.DiscardHandler),
		loc:          time.Local,
		toldReleases: map[string]bool{},
	}

	for _, opt := range opts {
		opt(d)
	}

	d.interest, _ = pub.(Interest)

	return d
}

// Run runs the job at a random moment between 09:00 and 10:00 local time every day, until ctx
// ends. A server started after that day's moment first runs the next day: keys stay in their
// threshold for days, so none is missed.
func (d *Daily) Run(ctx context.Context) {
	var lastRun time.Time // the local midnight of the day the job last ran; zero before the first run

	for {
		now := d.now().In(d.loc)

		// Once the job ran today, the next run is tomorrow's, whatever moment a new draw lands on.
		day := midnight(now)
		if day.Equal(lastRun) {
			day = day.AddDate(0, 0, 1)
		}

		next := d.moment(day)
		if !next.After(now) {
			day = day.AddDate(0, 0, 1)
			next = d.moment(day)
		}

		if !d.rhythm.Wait(ctx, next.Sub(now)) {
			return
		}

		lastRun = day

		if err := d.RunOnce(ctx); err != nil {
			d.log.Warn("daily notifications", "error", err)
		}
	}
}

// moment draws the moment of day's run: 09:00 local on the clock, built from the date rather than
// added to midnight so a day of 23 or 25 hours keeps it at 09:00, plus a random part of the hour.
func (d *Daily) moment(day time.Time) time.Time {
	y, m, dd := day.Date()

	return time.Date(y, m, dd, dailyFromHour, 0, 0, 0, d.loc).Add(d.rhythm.Between(0, dailyWindow))
}

// RunOnce tells, now, the keys that reached a threshold and the releases out today.
func (d *Daily) RunOnce(ctx context.Context) error {
	today := midnight(d.now().In(d.loc))

	return errors.Join(d.tellKeys(ctx, today), d.tellReleases(ctx, today))
}

// tellKeys publishes one message with every pending key that reached a threshold not told yet,
// nearest deadline first, then remembers them and forgets the keys no longer pending. With nobody
// to tell, it neither publishes nor remembers: a connection subscribing later still hears of them.
func (d *Daily) tellKeys(ctx context.Context, today time.Time) error {
	games, err := d.catalog.List(ctx)
	if err != nil {
		return fmt.Errorf("reading the games for expiring keys: %w", err)
	}

	told, err := d.keys.KeyThresholds(ctx)
	if err != nil {
		return fmt.Errorf("reading the key deadlines told: %w", err)
	}

	type due struct {
		copyID    game.ID
		threshold int
		key       event.ExpiringKey
	}

	var (
		pending []game.ID
		tell    []due
	)

	for _, g := range games {
		for _, c := range g.Copies() {
			if !c.IsPendingKey() {
				continue
			}

			pending = append(pending, c.ID)

			days, ok := d.daysLeft(c.RedeemBy, today)
			if !ok || days < 0 || days > ExpiringDays {
				continue
			}

			threshold := thresholdOf(days)
			if slices.Contains(told[c.ID], threshold) {
				continue
			}

			tell = append(tell, due{
				copyID:    c.ID,
				threshold: threshold,
				key: event.ExpiringKey{
					Game:     event.RefOf(g),
					Store:    c.Platform,
					RedeemBy: c.RedeemBy,
					DaysLeft: days,
				},
			})
		}
	}

	if len(tell) > 0 && (d.interest == nil || d.interest.Wants(ctx, event.KindKeysExpiring)) {
		slices.SortFunc(tell, func(a, b due) int {
			return cmp.Or(cmp.Compare(a.key.DaysLeft, b.key.DaysLeft), cmp.Compare(a.key.Game.Title, b.key.Game.Title))
		})

		keys := make([]event.ExpiringKey, 0, len(tell))
		for _, t := range tell {
			keys = append(keys, t.key)
		}

		d.pub.Publish(ctx, event.New(event.KindKeysExpiring, d.now(), event.KeysExpiring{Keys: keys}))

		for _, t := range tell {
			if err := d.keys.RememberKey(ctx, t.copyID, t.threshold, d.now()); err != nil {
				return fmt.Errorf("remembering a key deadline told: %w", err)
			}
		}
	}

	if err := d.keys.ForgetKeys(ctx, pending); err != nil {
		return fmt.Errorf("forgetting the keys no longer pending: %w", err)
	}

	return nil
}

// tellReleases publishes each release from the user's series that comes out today, once.
func (d *Daily) tellReleases(ctx context.Context, today time.Time) error {
	releases, err := d.releases.SeriesReleases(ctx)
	if err != nil {
		return fmt.Errorf("reading the releases from your series: %w", err)
	}

	date := game.Date(today.Format(dateLayout))

	d.mu.Lock()
	defer d.mu.Unlock()

	// Only today's releases matter: forget the ones told on another day.
	for k := range d.toldReleases {
		if !strings.HasPrefix(k, string(date)+"|") {
			delete(d.toldReleases, k)
		}
	}

	for _, r := range releases {
		key := string(r.Date) + "|" + r.Title
		if r.Date != date || d.toldReleases[key] {
			continue
		}

		d.toldReleases[key] = true
		d.pub.Publish(ctx, event.New(event.KindReleaseOut, d.now(), event.ReleaseOf(r)))
	}

	return nil
}

// daysLeft returns the whole days from today (a local midnight) until date, as the library's
// daysUntil counts them (web/src/lib/deadlines.ts): rounded, so a day of 23 or 25 hours at a
// daylight-saving change still counts as one. False when the key has no valid date.
func (d *Daily) daysLeft(date game.Date, today time.Time) (int, bool) {
	if date == "" {
		return 0, false
	}

	deadline, err := time.ParseInLocation(dateLayout, string(date), d.loc)
	if err != nil {
		return 0, false
	}

	return int(math.Round(deadline.Sub(today).Hours() / 24)), true
}

// thresholdOf is the threshold a key days from its deadline falls in: the smallest of
// KeyThresholds that is at least days (30 for 8-30 days, 7 for 2-7, 1 for 0-1).
func thresholdOf(days int) int {
	best := KeyThresholds[0]

	for _, t := range KeyThresholds {
		if t >= days && t < best {
			best = t
		}
	}

	return best
}

// midnight is the start of t's day, in t's location.
func midnight(t time.Time) time.Time {
	y, m, day := t.Date()

	return time.Date(y, m, day, 0, 0, 0, 0, t.Location())
}
