package home_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/game"
)

// steps drives Run deterministically: every random draw is 0.5 (Between gives the middle, Vary
// the duration itself), every wait moves the clock, and the job stops after a number of waits.
type steps struct {
	clk   *clock
	left  int
	waits []time.Duration

	// during runs inside the first wait, after the clock moved: what a person does meanwhile.
	during func()
}

func (st *steps) rhythm() unattended.Rhythm {
	return unattended.Rhythm{
		Random: func() float64 { return 0.5 },
		Sleep: func(_ context.Context, d time.Duration) bool {
			if st.left == 0 {
				return false // stops Run, as a canceled context does
			}

			st.left--
			st.waits = append(st.waits, d)
			st.clk.advance(d)

			if st.during != nil {
				st.during()
				st.during = nil
			}

			return true
		},
	}
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN both blocks configured and never gathered", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)

		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 7,
		}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the job runs for a day", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN both are gathered 3 minutes after start, then deals every 6 hours and releases every 24", func(t *testing.T) {
				// deals and releases at +3m, deals at +6h03, +12h03, +18h03, +24h03, releases at +24h03.
				assert.Equal(t, []time.Duration{3 * time.Minute, 0, 6 * time.Hour, 6 * time.Hour, 6 * time.Hour, 6 * time.Hour, 0}, st.waits)
				assert.Len(t, deals.calls, 10, "five refreshes of two lists")
				assert.Len(t, releases.windows, 2)
			})
		})
	})

	t.Run("GIVEN snapshots gathered an hour and two hours ago", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)

		store := snapshots{
			home.BlockDeals: {
				Block:     home.BlockDeals,
				FetchedAt: start.Add(-time.Hour),
			},
			home.BlockReleases: {
				Block:     home.BlockReleases,
				FetchedAt: start.Add(-2 * time.Hour),
			},
		}
		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 1,
		}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), store, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the server starts", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN nothing is asked at start: deals wait for their interval to end", func(t *testing.T) {
				assert.Equal(t, []time.Duration{5 * time.Hour}, st.waits)
				assert.Len(t, deals.calls, 2)
				assert.Zero(t, releases.calls())
			})
		})
	})

	t.Run("GIVEN snapshots older than their interval (the server was off for days)", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)

		store := snapshots{
			home.BlockDeals: {
				Block:     home.BlockDeals,
				FetchedAt: start.Add(-72 * time.Hour),
				Featured:  []game.Deal{{Title: "Echo Valley"}},
			},
			home.BlockReleases: {
				Block:     home.BlockReleases,
				FetchedAt: start.Add(-30 * time.Hour),
			},
		}
		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 2,
		}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), store, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the server starts", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN both are gathered at a random moment 1–5 minutes later, not at once", func(t *testing.T) {
				assert.Equal(t, []time.Duration{3 * time.Minute, 0}, st.waits)
				assert.Len(t, deals.calls, 2)
				assert.Len(t, releases.windows, 1)
			})
		})
	})

	t.Run("GIVEN no provider configured", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 3,
		}
		svc := home.NewService(&configs{}, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the job runs", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN it sends nothing, and looks again in about an hour", func(t *testing.T) {
				assert.Empty(t, deals.calls)
				assert.Zero(t, releases.calls())
				assert.Equal(t, []time.Duration{3 * time.Minute, 0, time.Hour}, st.waits)
			})
		})
	})

	t.Run("GIVEN deals never gathered, and no releases provider", func(t *testing.T) {
		deals := &fakeDeals{}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 3,
		}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{},
			home.WithDeals(deals), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		st.during = func() {
			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)
		}

		t.Run("WHEN the user refreshes deals just before the job's turn", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN the job does not ask again: deals' next turn is a whole interval after the user's refresh", func(t *testing.T) {
				assert.Len(t, deals.calls, 2, "the user's refresh only")
				// deals at +3m (skipped), releases (not configured) at +3m and +1h03; deals next at +6h03.
				assert.Equal(t, []time.Duration{3 * time.Minute, 0, time.Hour}, st.waits)
			})
		})
	})
}

func TestRun_futureAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals stored while the clock was three days ahead", func(t *testing.T) {
		deals := &fakeDeals{}
		cfg := &configs{}
		cfg.enable(deals)

		store := snapshots{home.BlockDeals: {
			Block:     home.BlockDeals,
			FetchedAt: start.Add(72 * time.Hour),
		}}
		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 1,
		}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), store, prefs{},
			home.WithDeals(deals), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the server starts", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN deals are gathered 1–5 minutes later, not after the wrong time", func(t *testing.T) {
				assert.Equal(t, []time.Duration{3 * time.Minute}, st.waits)
				assert.Len(t, deals.calls, 2)
			})
		})
	})

	t.Run("GIVEN deals due, and an attempt stored with a clock a month ahead just before the job's turn", func(t *testing.T) {
		deals := &fakeDeals{}
		cfg := &configs{}
		cfg.enable(deals)

		store := snapshots{}
		clk := &clock{start}
		st := &steps{
			clk:  clk,
			left: 1,
		}
		st.during = func() {
			store[home.BlockDeals] = home.Snapshot{
				Block:       home.BlockDeals,
				AttemptedAt: clk.now().Add(30 * 24 * time.Hour),
			}
		}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), store, prefs{},
			home.WithDeals(deals), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the job's turn comes", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN it gathers deals: the future attempt does not count as a recent refresh", func(t *testing.T) {
				assert.Len(t, deals.calls, 2)
			})
		})
	})
}
