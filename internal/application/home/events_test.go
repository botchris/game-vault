package home_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// recorder is an events.Publisher that keeps what it is given.
type recorder struct {
	events []event.Event
}

func (r *recorder) Publish(_ context.Context, e event.Event) { r.events = append(r.events, e) }

func TestRefresh_announcesNewSeriesReleases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an owned game and a database that knows one release of its series", func(t *testing.T) {
		keep := newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary)
		games := catalog{keep}
		sequel := game.Release{
			ProviderGameID: "db-4411",
			Title:          "Lantern Keep II",
			Date:           "2026-11-20",
			Platforms:      []string{"PC"},
			Because:        keep.ID(),
			BecauseTitle:   "Lantern Keep",
		}
		releases := &fakeReleases{series: []game.Release{sequel}}
		cfg := &configs{}
		cfg.enable(releases)

		clk := &clock{start}
		rec := &recorder{}
		svc := home.NewService(cfg, games, ownerOf(games), snapshots{}, prefs{}, home.WithReleases(releases),
			home.WithClock(clk.now), home.WithEvents(rec))

		t.Run("WHEN the releases are gathered for the first time", func(t *testing.T) {
			_, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN nothing is announced: everything would look new", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})

		t.Run("WHEN a later gathering finds a second release", func(t *testing.T) {
			clk.advance(time.Hour)

			remake := game.Release{
				Title:        "Lantern Keep Remade",
				Date:         "2027-02-01",
				Because:      keep.ID(),
				BecauseTitle: "Lantern Keep",
			}
			releases.series = []game.Release{sequel, remake}

			_, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN only the new one is announced", func(t *testing.T) {
				require.Len(t, rec.events, 1)
				assert.Equal(t, event.KindReleaseAnnounced, rec.events[0].Kind)
				assert.Equal(t, event.Release{
					Title:        "Lantern Keep Remade",
					Date:         "2027-02-01",
					Because:      keep.ID(),
					BecauseTitle: "Lantern Keep",
				}, rec.events[0].Data)
			})
		})

		t.Run("WHEN the same releases are gathered again", func(t *testing.T) {
			clk.advance(time.Hour)

			rec.events = nil

			_, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN nothing is announced", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})

		t.Run("WHEN the series releases are asked for", func(t *testing.T) {
			got, err := svc.SeriesReleases(ctx)
			require.NoError(t, err)

			t.Run("THEN they are the ones last gathered", func(t *testing.T) {
				assert.Equal(t, []string{"Lantern Keep II", "Lantern Keep Remade"}, []string{got[0].Title, got[1].Title})
			})
		})
	})
}

func TestRefresh_tellsNewHistoricalLows(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	low := func(title, steam string) game.Deal {
		d := deal(title, steam)
		d.HistoricalLow = true

		return d
	}

	t.Run("GIVEN a game owned on PC and a deals service with one historical low", func(t *testing.T) {
		games := catalog{newGame(t, "Copper Tides", game.Links{game.LinkSteam: "5510"}, steamLibrary)}
		deals := &fakeDeals{lows: []game.Deal{low("Moss Runner", "5520")}}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		rec := &recorder{}
		svc := home.NewService(cfg, games, ownerOf(games), snapshots{}, prefs{}, home.WithDeals(deals),
			home.WithClock(clk.now), home.WithEvents(rec))

		t.Run("WHEN the deals are gathered for the first time", func(t *testing.T) {
			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN nothing is told", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})

		t.Run("WHEN a later gathering adds a low of a game not owned, one of the owned game and a deal that is no low", func(t *testing.T) {
			clk.advance(time.Hour)

			deals.lows = []game.Deal{
				low("Moss Runner", "5520"),
				low("Quiet Orbit", "5530"),
				low("Copper Tides", "5510"),
				deal("Harbor Lights", "5540"),
			}

			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN only the new low of the game not owned is told", func(t *testing.T) {
				require.Len(t, rec.events, 1)
				assert.Equal(t, event.KindHistoricalLow, rec.events[0].Kind)
				assert.Equal(t, event.HistoricalLowOf(low("Quiet Orbit", "5530")), rec.events[0].Data)
			})
		})
	})
}

func TestRefresh_unreadablePreviousGatheringTellsNothingButSaves(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	low := func(title, steam string) game.Deal {
		d := deal(title, steam)
		d.HistoricalLow = true

		return d
	}

	t.Run("GIVEN deals gathered once, with one historical low", func(t *testing.T) {
		games := catalog{newGame(t, "Copper Tides", game.Links{game.LinkSteam: "5510"}, steamLibrary)}
		failNext := false
		deals := &fakeDeals{lows: []game.Deal{low("Moss Runner", "5520")}}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		rec := &recorder{}
		snaps := snapshots{}
		svc := home.NewService(cfg, games, ownerOf(games), unreadable{
			snapshots: snaps,
			failNext:  &failNext,
		}, prefs{},
			home.WithDeals(deals), home.WithClock(clk.now), home.WithEvents(rec))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)

		t.Run("WHEN a gathering finds a new low but the previous gathering cannot be read", func(t *testing.T) {
			clk.advance(time.Hour)

			deals.lows = []game.Deal{low("Moss Runner", "5520"), low("Quiet Orbit", "5530")}
			// The provider is asked after the refresh guard's read, so the failing read is the one
			// that looks for the previous gathering.
			deals.asked = func() { failNext = true }

			_, err := svc.Refresh(ctx, home.BlockDeals)
			deals.asked = nil

			t.Run("THEN the refresh succeeds and tells nothing, as a first gathering would", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, rec.events)
			})

			t.Run("AND what it gathered is saved", func(t *testing.T) {
				assert.Len(t, snaps[home.BlockDeals].HistoricalLows, 2)
			})
		})

		t.Run("WHEN the same lows are gathered again", func(t *testing.T) {
			clk.advance(time.Hour)

			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN nothing is told: the saved gathering is the baseline", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})
	})
}

func TestRefresh_failedGatheringKeepsTheBaseline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	low := func(title, steam string) game.Deal {
		d := deal(title, steam)
		d.HistoricalLow = true

		return d
	}

	t.Run("GIVEN deals gathered once, with one historical low", func(t *testing.T) {
		games := catalog{newGame(t, "Copper Tides", game.Links{game.LinkSteam: "5510"}, steamLibrary)}
		deals := &fakeDeals{lows: []game.Deal{low("Moss Runner", "5520")}}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		rec := &recorder{}
		svc := home.NewService(cfg, games, ownerOf(games), snapshots{}, prefs{}, home.WithDeals(deals),
			home.WithClock(clk.now), home.WithEvents(rec))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)

		t.Run("WHEN the next gathering fails", func(t *testing.T) {
			clk.advance(time.Hour)

			deals.err = errors.New("service unavailable")
			_, _ = svc.Refresh(ctx, home.BlockDeals) // the failure is stored next to the last good data

			t.Run("THEN nothing is told", func(t *testing.T) {
				assert.Empty(t, rec.events)
			})
		})

		t.Run("WHEN a later gathering succeeds with the same low and a new one", func(t *testing.T) {
			clk.advance(time.Hour)

			deals.err = nil
			deals.lows = []game.Deal{low("Moss Runner", "5520"), low("Quiet Orbit", "5530")}

			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN only the new low is told: the failure kept the earlier gathering as the baseline", func(t *testing.T) {
				require.Len(t, rec.events, 1)
				assert.Equal(t, event.HistoricalLowOf(low("Quiet Orbit", "5530")), rec.events[0].Data)
			})
		})
	})
}
