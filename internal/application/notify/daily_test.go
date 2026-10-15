package notify_test

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // the daylight-saving test needs a real zone, whatever the machine has

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/notify"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// memKeyMemory keeps the thresholds told in memory.
type memKeyMemory struct {
	mu   sync.Mutex
	told map[game.ID][]int
	keep []game.ID
}

func (m *memKeyMemory) KeyThresholds(context.Context) (map[game.ID][]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make(map[game.ID][]int, len(m.told))
	for id, ts := range m.told {
		out[id] = slices.Clone(ts)
	}

	return out, nil
}

func (m *memKeyMemory) RememberKey(_ context.Context, copyID game.ID, threshold int, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.told == nil {
		m.told = map[game.ID][]int{}
	}

	m.told[copyID] = append(m.told[copyID], threshold)

	return nil
}

func (m *memKeyMemory) ForgetKeys(_ context.Context, keep []game.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.keep = slices.Clone(keep)

	for id := range m.told {
		if !slices.Contains(keep, id) {
			delete(m.told, id)
		}
	}

	return nil
}

func (m *memKeyMemory) thresholds(id game.ID) []int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.told[id])
}

// fakeReleases is home's stored list of releases from the user's series.
type fakeReleases struct {
	releases []game.Release
}

func (f *fakeReleases) SeriesReleases(context.Context) ([]game.Release, error) {
	return f.releases, nil
}

// zone is a fixed time zone ahead of UTC, so a test would catch days counted in UTC.
var zone = time.FixedZone("Example", 2*60*60)

// dailyWorld is a daily job with in-memory ports and a clock at 2026-10-15 10:00 in zone.
type dailyWorld struct {
	catalog  *fakeCatalog
	keys     *memKeyMemory
	releases *fakeReleases
	pub      *recorder
	clock    *clock
	daily    *notify.Daily
}

func newDailyWorld(opts ...notify.DailyOption) *dailyWorld {
	w := &dailyWorld{
		catalog:  &fakeCatalog{games: map[game.ID]*game.Game{}},
		keys:     &memKeyMemory{},
		releases: &fakeReleases{},
		pub:      &recorder{},
		clock:    &clock{now: time.Date(2026, 10, 15, 10, 0, 0, 0, zone)},
	}

	all := append([]notify.DailyOption{
		notify.WithDailyClock(w.clock.Now),
		notify.WithDailyLocation(zone),
	}, opts...)
	w.daily = notify.NewDaily(w.catalog, w.keys, w.releases, w.pub, all...)

	return w
}

// addKey adds a game with one key copy on platform, due by redeemBy, and returns the copy's id.
func (w *dailyWorld) addKey(t *testing.T, title, platform string, redeemBy game.Date, status game.Status) game.ID {
	t.Helper()

	g, err := game.New(title, w.clock.Now())
	require.NoError(t, err)

	c, err := g.AddCopy(game.CopyDetails{
		Kind:     game.KindKey,
		Platform: platform,
		Status:   status,
		RedeemBy: redeemBy,
	}, w.clock.Now())
	require.NoError(t, err)

	w.catalog.games[g.ID()] = g

	return c.ID
}

// keysTold returns the keys of the keys.expiring events published, one list per event.
func keysTold(t *testing.T, events []event.Event) [][]event.ExpiringKey {
	t.Helper()

	out := make([][]event.ExpiringKey, 0, len(events))

	for _, e := range events {
		require.Equal(t, event.KindKeysExpiring, e.Kind)

		data, ok := e.Data.(event.KeysExpiring)
		require.True(t, ok)

		out = append(out, data.Keys)
	}

	return out
}

func titlesAndDays(keys []event.ExpiringKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.Game.Title+" "+string(k.RedeemBy)+" "+k.Store+" "+strconv.Itoa(k.DaysLeft))
	}

	return out
}

func TestDaily_keys(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN keys due in 30, 31, 7 and 1 days, a redeemed one and one already past", func(t *testing.T) {
		w := newDailyWorld()
		in30 := w.addKey(t, "Example Game", "Steam", "2026-11-14", game.StatusUnrevealed)
		in31 := w.addKey(t, "Another Game", "GOG", "2026-11-15", game.StatusRevealed)
		in7 := w.addKey(t, "Third Game", "Steam", "2026-10-22", game.StatusUnrevealed)
		in1 := w.addKey(t, "Fourth Game", "GOG", "2026-10-16", game.StatusRevealed)
		redeemed := w.addKey(t, "Fifth Game", "Steam", "2026-10-20", game.StatusRedeemed)
		past := w.addKey(t, "Old Game", "Steam", "2026-10-13", game.StatusUnrevealed)

		t.Run("WHEN the job runs", func(t *testing.T) {
			require.NoError(t, w.daily.RunOnce(ctx))

			t.Run("THEN one message tells the three keys within 30 days, nearest first", func(t *testing.T) {
				told := keysTold(t, w.pub.all())
				require.Len(t, told, 1)
				assert.Equal(t, []string{
					"Fourth Game 2026-10-16 GOG 1",
					"Third Game 2026-10-22 Steam 7",
					"Example Game 2026-11-14 Steam 30",
				}, titlesAndDays(told[0]))
				assert.NotEmpty(t, told[0][0].Game.ID)
				assert.Equal(t, []string{"PC"}, told[0][0].Game.Systems)
			})

			t.Run("AND each key's threshold is remembered", func(t *testing.T) {
				assert.Equal(t, []int{30}, w.keys.thresholds(in30))
				assert.Equal(t, []int{7}, w.keys.thresholds(in7))
				assert.Equal(t, []int{1}, w.keys.thresholds(in1))
				assert.Empty(t, w.keys.thresholds(in31))
			})

			t.Run("AND a key past its deadline is not told (the library's expired view covers it)", func(t *testing.T) {
				assert.Empty(t, w.keys.thresholds(past))
			})

			t.Run("AND only the pending keys are kept in memory", func(t *testing.T) {
				assert.ElementsMatch(t, []game.ID{in30, in31, in7, in1, past}, w.keys.keep)
				assert.NotContains(t, w.keys.keep, redeemed)
			})
		})

		t.Run("WHEN it runs the next day THEN only the key that entered the window is told", func(t *testing.T) {
			w.pub.reset()
			w.clock.Add(24 * time.Hour)
			require.NoError(t, w.daily.RunOnce(ctx))

			told := keysTold(t, w.pub.all())
			require.Len(t, told, 1)
			assert.Equal(t, []string{"Another Game 2026-11-15 GOG 30"}, titlesAndDays(told[0]))
		})

		t.Run("WHEN it runs again the same day THEN nothing is told", func(t *testing.T) {
			w.pub.reset()
			require.NoError(t, w.daily.RunOnce(ctx))
			assert.Empty(t, w.pub.all())
		})

		t.Run("WHEN a key told at 30 days reaches 7 THEN it is told again", func(t *testing.T) {
			w.pub.reset()
			w.clock.now = time.Date(2026, 11, 7, 10, 0, 0, 0, zone)
			require.NoError(t, w.daily.RunOnce(ctx))

			told := keysTold(t, w.pub.all())
			require.Len(t, told, 1)
			assert.Equal(t, []string{"Example Game 2026-11-14 Steam 7"}, titlesAndDays(told[0]))
			assert.Equal(t, []int{30, 7}, w.keys.thresholds(in30))
		})

		t.Run("WHEN it reaches its last day THEN it is told a third time, as today, with the key now a day away", func(t *testing.T) {
			w.pub.reset()
			w.clock.now = time.Date(2026, 11, 14, 10, 0, 0, 0, zone)
			require.NoError(t, w.daily.RunOnce(ctx))

			told := keysTold(t, w.pub.all())
			require.Len(t, told, 1)
			assert.Equal(t, []string{
				"Example Game 2026-11-14 Steam 0",
				"Another Game 2026-11-15 GOG 1",
			}, titlesAndDays(told[0]))
			assert.Equal(t, []int{30, 7, 1}, w.keys.thresholds(in30))
		})
	})

	t.Run("GIVEN a key due tomorrow WHEN the job runs at 01:00 local, still the day before in UTC", func(t *testing.T) {
		w := newDailyWorld()
		w.clock.now = time.Date(2026, 10, 15, 1, 0, 0, 0, zone)
		w.addKey(t, "Example Game", "Steam", "2026-10-16", game.StatusUnrevealed)
		require.NoError(t, w.daily.RunOnce(ctx))

		t.Run("THEN the days count from the local date", func(t *testing.T) {
			told := keysTold(t, w.pub.all())
			require.Len(t, told, 1)
			assert.Equal(t, 1, told[0][0].DaysLeft)
		})
	})

	t.Run("GIVEN no key within the window WHEN the job runs THEN nothing is published", func(t *testing.T) {
		w := newDailyWorld()
		w.addKey(t, "Example Game", "Steam", "", game.StatusUnrevealed)
		w.addKey(t, "Another Game", "Steam", "2027-01-01", game.StatusUnrevealed)
		require.NoError(t, w.daily.RunOnce(ctx))
		assert.Empty(t, w.pub.all())
	})
}

// choosyRecorder is a recorder that says whether anybody wants an event, as notify.Service does.
type choosyRecorder struct {
	recorder

	wants bool
}

func (c *choosyRecorder) Wants(context.Context, event.Kind) bool { return c.wants }

func TestDaily_keysNobodyWants(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a key due in 7 days and no connection that wants keys.expiring", func(t *testing.T) {
		w := newDailyWorld()
		pub := &choosyRecorder{}
		w.daily = notify.NewDaily(w.catalog, w.keys, w.releases, pub,
			notify.WithDailyClock(w.clock.Now), notify.WithDailyLocation(zone))
		in7 := w.addKey(t, "Third Game", "Steam", "2026-10-22", game.StatusUnrevealed)

		t.Run("WHEN the job runs", func(t *testing.T) {
			require.NoError(t, w.daily.RunOnce(ctx))

			t.Run("THEN nothing is published and the key is not remembered as told", func(t *testing.T) {
				assert.Empty(t, keysTold(t, pub.all()))
				assert.Empty(t, w.keys.thresholds(in7))
			})
		})

		t.Run("WHEN a connection then subscribes and the job runs again", func(t *testing.T) {
			pub.wants = true

			require.NoError(t, w.daily.RunOnce(ctx))

			t.Run("THEN the key is told and remembered", func(t *testing.T) {
				require.Len(t, keysTold(t, pub.all()), 1)
				assert.Equal(t, []int{7}, w.keys.thresholds(in7))
			})
		})
	})
}

func TestDaily_releases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a series release out today, a later one and one without a date", func(t *testing.T) {
		w := newDailyWorld()
		w.releases.releases = []game.Release{
			{
				ProviderGameID: "p-1",
				Title:          "Example Sequel",
				Date:           "2026-10-15",
				Platforms:      []string{"PC", "PS5"},
				CoverURL:       "https://img.example/sequel.jpg",
				URL:            "https://db.example/sequel",
				Because:        "g-1",
				BecauseTitle:   "Example Game",
			},
			{
				Title: "Later Sequel",
				Date:  "2026-11-20",
			},
			{Title: "Unannounced Sequel"},
		}

		t.Run("WHEN the job runs THEN the one out today is told", func(t *testing.T) {
			require.NoError(t, w.daily.RunOnce(ctx))

			got := w.pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindReleaseOut, got[0].Kind)
			assert.Equal(t, event.Release{
				Title:        "Example Sequel",
				Date:         "2026-10-15",
				Platforms:    []string{"PC", "PS5"},
				CoverURL:     "https://img.example/sequel.jpg",
				URL:          "https://db.example/sequel",
				Because:      "g-1",
				BecauseTitle: "Example Game",
			}, got[0].Data)
		})

		t.Run("WHEN it runs again the same day THEN it is not told again", func(t *testing.T) {
			w.pub.reset()
			require.NoError(t, w.daily.RunOnce(ctx))
			assert.Empty(t, w.pub.all())
		})
	})
}

func TestDaily_Run(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// run starts the job at start with a rhythm that draws the middle of every range and stops at
	// the second wait; it returns the waits.
	run := func(t *testing.T, start time.Time) (*dailyWorld, []time.Duration) {
		t.Helper()

		var (
			w     *dailyWorld
			waits []time.Duration
		)

		w = newDailyWorld(notify.WithDailyRhythm(unattended.Rhythm{
			Random: func() float64 { return 0.5 },
			Sleep: func(_ context.Context, d time.Duration) bool {
				waits = append(waits, d)
				w.clock.Add(d)

				return len(waits) < 2
			},
		}))
		w.clock.now = start
		w.releases.releases = []game.Release{{
			Title: "Example Sequel",
			Date:  "2026-10-16",
		}}

		w.daily.Run(ctx)

		return w, waits
	}

	t.Run("GIVEN a job started at 10:00, after today's 09:00-10:00 window", func(t *testing.T) {
		w, waits := run(t, time.Date(2026, 10, 15, 10, 0, 0, 0, zone))

		t.Run("THEN it waits for a moment of tomorrow's window, then a day", func(t *testing.T) {
			assert.Equal(t, []time.Duration{23*time.Hour + 30*time.Minute, 24 * time.Hour}, waits)
		})

		t.Run("AND it ran once in between, telling tomorrow's release", func(t *testing.T) {
			got := w.pub.all()
			require.Len(t, got, 1)
			assert.Equal(t, event.KindReleaseOut, got[0].Kind)
		})
	})

	t.Run("GIVEN a job started at 08:00 WHEN it runs THEN it waits for today's window", func(t *testing.T) {
		_, waits := run(t, time.Date(2026, 10, 15, 8, 0, 0, 0, zone))
		require.NotEmpty(t, waits)
		assert.Equal(t, 90*time.Minute, waits[0])
	})
}

func TestDaily_Run_oncePerDay(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// runAt starts the job at start with a rhythm drawing draws in turn, stops at the stop-th wait
	// and returns the local moments it ran at.
	runAt := func(t *testing.T, loc *time.Location, start time.Time, stop int, draws ...float64) []time.Time {
		t.Helper()

		var (
			w     *dailyWorld
			ran   []time.Time
			waits int
			drawn int
		)

		w = newDailyWorld(notify.WithDailyLocation(loc), notify.WithDailyRhythm(unattended.Rhythm{
			Random: func() float64 {
				r := draws[drawn%len(draws)]
				drawn++

				return r
			},
			Sleep: func(_ context.Context, d time.Duration) bool {
				waits++
				if waits == stop {
					return false
				}

				w.clock.Add(d)
				ran = append(ran, w.clock.Now().In(loc))

				return true
			},
		}))
		w.clock.now = start

		w.daily.Run(ctx)

		return ran
	}

	t.Run("GIVEN draws that grow (an early moment, then a later one)", func(t *testing.T) {
		ran := runAt(t, zone, time.Date(2026, 10, 15, 8, 0, 0, 0, zone), 4, 0.2, 0.8)

		t.Run("WHEN it runs for three days THEN it runs once a day, inside the window", func(t *testing.T) {
			require.Len(t, ran, 3)

			for i, at := range ran {
				assert.Equal(t, 15+i, at.Day(), "run %d at %s", i, at)
				assert.Equal(t, 9, at.Hour(), "run %d at %s", i, at)
			}
		})
	})

	t.Run("GIVEN the day the clocks go back (a 25-hour day)", func(t *testing.T) {
		madrid, err := time.LoadLocation("Europe/Madrid")
		require.NoError(t, err)

		ran := runAt(t, madrid, time.Date(2026, 10, 25, 1, 0, 0, 0, madrid), 2, 0.5)

		t.Run("WHEN it runs THEN it is still at 09:30 local", func(t *testing.T) {
			require.Len(t, ran, 1)
			assert.Equal(t, time.Date(2026, 10, 25, 9, 30, 0, 0, madrid), ran[0])
		})
	})
}
