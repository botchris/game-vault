package home_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/application/ownership"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// newGame builds a game with its links and copies. Every title is invented.
func newGame(t *testing.T, title string, links game.Links, copies ...game.CopyDetails) *game.Game {
	t.Helper()

	g, err := game.New(title, start)
	require.NoError(t, err)

	if links != nil {
		_, err = g.UpdateInfo(game.Info{
			Title: title,
			Links: links,
		}, start)
		require.NoError(t, err)
	}

	for _, d := range copies {
		_, err := g.AddCopy(d, start)
		require.NoError(t, err)
	}

	return g
}

var (
	steamLibrary = game.CopyDetails{
		Kind:     game.KindLibrary,
		Platform: "Steam",
	}
	gogLibrary = game.CopyDetails{
		Kind:     game.KindLibrary,
		Platform: "GOG",
	}
	ps4Disc = game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	}
	soldDisc = game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
		Status:   game.StatusSold,
	}
)

func deal(title, steam string) game.Deal {
	return game.Deal{
		ProviderGameID: "itad-" + title,
		Title:          title,
		SteamAppID:     steam,
		Shop:           "Steam",
		Price: game.Money{
			Amount:   499,
			Currency: "EUR",
		},
		Regular: game.Money{
			Amount:   1999,
			Currency: "EUR",
		},
		Cut: 75,
	}
}

func titles(deals []game.Deal) []string {
	out := make([]string, 0, len(deals))
	for _, d := range deals {
		out = append(out, d.Title)
	}

	return out
}

func TestHome_states(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN providers compiled in but none enabled", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		clk := &clock{start}
		svc := home.NewService(&configs{}, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now))

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN both blocks say they are not configured, and nothing was asked", func(t *testing.T) {
				assert.Equal(t, home.StateNotConfigured, h.Deals.State)
				assert.Equal(t, home.StateNotConfigured, h.Releases.State)
				assert.Empty(t, deals.calls)
				assert.Zero(t, releases.calls())
			})
		})
	})

	t.Run("GIVEN both providers enabled and nothing gathered yet", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN the blocks were never fetched, and can be refreshed at once", func(t *testing.T) {
				assert.Equal(t, home.StateNeverFetched, h.Deals.State)
				assert.Equal(t, "Deals Co", h.Deals.Provider)
				assert.True(t, h.Deals.RefreshableAt.IsZero())
				assert.Equal(t, home.StateNeverFetched, h.Releases.State)
			})

			t.Run("AND reading never calls a provider (with -no-unattended, nothing else would)", func(t *testing.T) {
				assert.Empty(t, deals.calls)
				assert.Zero(t, releases.calls())
			})
		})
	})

	t.Run("GIVEN deals stored, then ITAD turned off", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		cfg.disable(provider.KindDeals)

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN the block is not configured and the stored deals are not shown as current", func(t *testing.T) {
				assert.Equal(t, home.StateNotConfigured, h.Deals.State)
				assert.Empty(t, h.Featured)
			})
		})
	})

	t.Run("GIVEN deals fetched three days ago, without an error since", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		clk.advance(72 * time.Hour) // the server was off

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN it shows the last data as stale, with its age", func(t *testing.T) {
				assert.Equal(t, home.StateStale, h.Deals.State)
				assert.Equal(t, start, h.Deals.FetchedAt)
				assert.Empty(t, h.Deals.Error)
				assert.Equal(t, []string{"Echo Valley"}, titles(h.Featured))
			})
		})
	})
}

func TestRefresh_deals(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a collection and the deals of the day", func(t *testing.T) {
		games := catalog{
			newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary),
			newGame(t, "Echo Valley", nil, gogLibrary),        // owned on PC, not on Steam
			newGame(t, "Harbor Lights", nil, ps4Disc),         // only on a console
			newGame(t, "Copper Coast", nil, soldDisc),         // sold: not owned any more
			newGame(t, "星の灯台", nil, steamLibrary),             // a title in another script
			newGame(t, "Driftwood", game.Links{}, gogLibrary), // owned, no Steam link
		}
		deals := &fakeDeals{
			featured: []game.Deal{
				deal("Lantern Keep: Definitive", "4410"), // the same Steam app under another name
				deal("ECHO VALLEY™", ""),
				deal("Harbor Lights", "4430"),
				deal("Copper Coast", ""),
				deal("星の港", ""),
				deal("星の灯台", ""),
				deal("Driftwood Tales", ""), // a deal without a Steam id, for another game
			},
			lows: []game.Deal{deal("Harbor Lights", "4430")},
		}
		cfg := &configs{}
		cfg.enable(deals)
		svc := home.NewService(cfg, games, ownerOf(games), snapshots{}, prefs{Currency: "EUR"}, home.WithDeals(deals), home.WithClock((&clock{start}).now))

		t.Run("WHEN the deals block is refreshed", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN both lists are asked for, in the country of the preferred currency", func(t *testing.T) {
				assert.Equal(t, []home.DealQuery{
					{
						List:    home.DealsFeatured,
						Country: "ES",
						Limit:   40,
					},
					{
						List:    home.DealsHistoricalLows,
						Country: "ES",
						Limit:   40,
					},
				}, deals.calls)
			})

			t.Run("AND the block is fresh, and the answer reported to the registry", func(t *testing.T) {
				assert.Equal(t, home.StateFresh, h.Deals.State)
				assert.Equal(t, start, h.Deals.FetchedAt)
				assert.Equal(t, start.Add(15*time.Minute), h.Deals.RefreshableAt)
				assert.Equal(t, []error{nil}, cfg.results)
			})

			t.Run("AND games owned on PC are left out, by Steam id or by exact title", func(t *testing.T) {
				assert.Equal(t, []string{"Harbor Lights", "Copper Coast", "星の港", "Driftwood Tales"}, titles(h.Featured))
			})

			t.Run("AND a game owned only on a console stays, with where the user has it", func(t *testing.T) {
				assert.Equal(t, []string{game.SystemPS4}, h.Featured[0].OwnedOn)
				assert.Empty(t, h.Featured[1].OwnedOn, "a sold copy is not owned")
			})

			t.Run("AND titles in another script match only the same title", func(t *testing.T) {
				assert.Contains(t, titles(h.Featured), "星の港")
				assert.NotContains(t, titles(h.Featured), "星の灯台")
			})

			t.Run("AND a deal without a Steam id is never matched through an empty id", func(t *testing.T) {
				assert.Contains(t, titles(h.Featured), "Driftwood Tales")
			})
		})

		t.Run("WHEN the user then buys a game on sale and the page is read again", func(t *testing.T) {
			// The service reads the same slice: replacing the sold copy's game is what a purchase
			// imported by a scan looks like to it.
			games[3] = newGame(t, "Copper Coast", nil, steamLibrary)

			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN its deal is gone without waiting for the next refresh", func(t *testing.T) {
				assert.NotContains(t, titles(h.Featured), "Copper Coast")
				assert.Len(t, deals.calls, 2, "no provider was asked again")
			})
		})
	})

	t.Run("GIVEN more unowned deals than the page shows", func(t *testing.T) {
		many := &fakeDeals{}
		for i := range 20 {
			many.featured = append(many.featured, deal(fmt.Sprintf("Example %d", i), ""))
		}

		cfg := &configs{}
		cfg.enable(many)
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{}, home.WithDeals(many), home.WithClock((&clock{start}).now))

		t.Run("WHEN the block is refreshed without a preferred currency", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN twelve are shown, in the provider's order, and no country asked for", func(t *testing.T) {
				require.Len(t, h.Featured, 12)
				assert.Equal(t, "Example 0", h.Featured[0].Title)
				assert.Empty(t, many.calls[0].Country, "no country: the provider decides")
			})
		})
	})

	t.Run("GIVEN an empty collection", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		releases := &fakeReleases{anticipated: []game.Release{{
			Title: "Harbor Lights",
			Date:  "2026-12-01",
		}}}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN both blocks are refreshed", func(t *testing.T) {
			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)
			h, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN the deals are all shown, and the series not asked for", func(t *testing.T) {
				assert.Equal(t, []string{"Echo Valley"}, titles(h.Featured))
				assert.Empty(t, releases.owned)
				assert.Empty(t, h.FromSeries)
				assert.Len(t, h.Anticipated, 1)
			})
		})
	})
}

func TestHome_ownershipBatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN more stored deals than one ownership check may hold", func(t *testing.T) {
		many := &fakeDeals{}
		for i := range 150 {
			many.featured = append(many.featured, deal(fmt.Sprintf("Example %d", i), ""))
			many.lows = append(many.lows, deal(fmt.Sprintf("Sample %d", i), ""))
		}

		games := catalog{newGame(t, "Example 149", nil, steamLibrary)}
		owned := ownerOf(games)
		cfg := &configs{}
		cfg.enable(many)
		svc := home.NewService(cfg, games, owned, snapshots{}, prefs{}, home.WithDeals(many), home.WithClock((&clock{start}).now))

		t.Run("WHEN the deals are refreshed and read", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the lookup is asked in batches it accepts, and every deal was checked", func(t *testing.T) {
				require.NotEmpty(t, owned.batches)

				total := 0

				for _, n := range owned.batches {
					assert.LessOrEqual(t, n, ownership.MaxItems)
					total += n
				}

				assert.Equal(t, 300, total)
				assert.Len(t, h.Featured, 12)
				assert.Len(t, h.HistoricalLows, 12)
			})
		})
	})
}

func TestRefresh_failureKeepsLastGoodData(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals gathered six hours ago", func(t *testing.T) {
		deals := &fakeDeals{
			featured: []game.Deal{deal("Echo Valley", "")},
			lows:     []game.Deal{deal("Harbor Lights", "4430")},
		}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		stored := snapshots{}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), stored, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		clk.advance(6 * time.Hour)

		t.Run("WHEN the service is down at the next refresh", func(t *testing.T) {
			down := fmt.Errorf("not answering (HTTP 503): %w", provider.ErrUnavailable)
			deals.err = down

			h, err := svc.Refresh(ctx, home.BlockDeals)

			t.Run("THEN the refresh itself does not fail: the block says what happened", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, home.StateStale, h.Deals.State)
				assert.Equal(t, home.ErrorUnavailable, h.Deals.ErrorKind)
				assert.Contains(t, h.Deals.Error, "HTTP 503")
				assert.Equal(t, start.Add(6*time.Hour), h.Deals.FailedAt)
			})

			t.Run("AND the last good deals stay, with their age", func(t *testing.T) {
				assert.Equal(t, start, h.Deals.FetchedAt)
				assert.Equal(t, []string{"Echo Valley"}, titles(h.Featured))
			})

			t.Run("AND the stored snapshot keeps both previous lists next to the failure, for a restart", func(t *testing.T) {
				snap := stored[home.BlockDeals]
				assert.Equal(t, []string{"Echo Valley"}, titles(snap.Featured))
				assert.Equal(t, []string{"Harbor Lights"}, titles(snap.HistoricalLows))
				assert.Equal(t, start, snap.FetchedAt)
				assert.Equal(t, start.Add(6*time.Hour), snap.FailedAt)
				assert.Equal(t, home.ErrorUnavailable, snap.ErrorKind)
			})

			t.Run("AND the problem reaches the provider's status (System → Health)", func(t *testing.T) {
				assert.ErrorIs(t, cfg.results[len(cfg.results)-1], provider.ErrUnavailable)
			})
		})

		t.Run("WHEN it answers again later", func(t *testing.T) {
			deals.err = nil

			clk.advance(time.Hour)

			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the error is gone", func(t *testing.T) {
				assert.Equal(t, home.StateFresh, h.Deals.State)
				assert.Empty(t, h.Deals.Error)
			})
		})
	})
}

func TestRefresh_guard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals refreshed a moment ago", func(t *testing.T) {
		deals := &fakeDeals{}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)

		t.Run("WHEN the user asks again ten minutes later", func(t *testing.T) {
			clk.advance(10 * time.Minute)

			_, err := svc.Refresh(ctx, home.BlockDeals)

			t.Run("THEN it is refused without a request", func(t *testing.T) {
				assert.ErrorIs(t, err, home.ErrTooSoon)
				assert.Len(t, deals.calls, 2, "only the first refresh's two lists")
			})
		})

		t.Run("WHEN the user asks again fifteen minutes after the first, and it fails", func(t *testing.T) {
			clk.advance(5 * time.Minute)

			deals.err = provider.ErrUnavailable

			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the failed attempt counts too: the next one waits fifteen minutes", func(t *testing.T) {
				clk.advance(time.Minute)

				_, err := svc.Refresh(ctx, home.BlockDeals)
				assert.ErrorIs(t, err, home.ErrTooSoon)
			})
		})
	})

	t.Run("GIVEN no deals provider enabled", func(t *testing.T) {
		svc := home.NewService(&configs{}, catalog{}, ownerOf(catalog{}), snapshots{}, prefs{}, home.WithDeals(&fakeDeals{}))

		t.Run("WHEN the user refreshes deals, or a block that does not exist", func(t *testing.T) {
			_, notConfigured := svc.Refresh(ctx, home.BlockDeals)
			_, unknown := svc.Refresh(ctx, "weather")

			t.Run("THEN they are told to enable a provider, or that there is no such block", func(t *testing.T) {
				assert.ErrorIs(t, notConfigured, home.ErrNotConfigured)
				assert.ErrorIs(t, unknown, home.ErrUnknownBlock)
			})
		})
	})
}

func TestRefresh_releases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an owned game, a sold one, and the releases a database knows", func(t *testing.T) {
		keep := newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary)
		games := catalog{keep, newGame(t, "Copper Coast", game.Links{game.LinkSteam: "4420"}, soldDisc)}
		releases := &fakeReleases{
			series: []game.Release{
				{
					Title:        "Lantern Keep: Origins",
					Date:         "2026-10-01",
					Because:      keep.ID(),
					BecauseTitle: "Lantern Keep",
				}, // out already
				{
					Title:        "Lantern Keep II",
					Date:         "2026-11-20",
					Because:      keep.ID(),
					BecauseTitle: "Lantern Keep",
				},
				{
					Title:        "Copper Coast 2",
					Date:         "2026-11-01",
					Because:      "deleted-game",
					BecauseTitle: "Copper Coast",
				},
			},
			anticipated: []game.Release{
				{
					Title: "Harbor Lights",
					Date:  "2026-12-01",
				},
				{
					Title: "Echo Valley",
					Date:  "2026-10-14",
				},
			},
		}
		cfg := &configs{}
		cfg.enable(releases)
		svc := home.NewService(cfg, games, ownerOf(games), snapshots{}, prefs{}, home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN the releases block is refreshed", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN the next 60 days are asked for, and the series of the games still owned", func(t *testing.T) {
				assert.Equal(t, []time.Duration{60 * 24 * time.Hour}, releases.windows)
				require.Len(t, releases.owned, 1)
				assert.Equal(t, []home.OwnedRef{{
					Game:  keep.ID(),
					Title: "Lantern Keep",
					Links: game.Links{game.LinkSteam: "4410"},
				}}, releases.owned[0])
			})

			t.Run("AND releases already out, or explained by a game no longer in the catalog, are left out", func(t *testing.T) {
				require.Len(t, h.FromSeries, 1)
				assert.Equal(t, "Lantern Keep II", h.FromSeries[0].Title)
				require.Len(t, h.Anticipated, 1)
				assert.Equal(t, "Harbor Lights", h.Anticipated[0].Title)
			})
		})
	})
}

func TestRefresh_canceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals gathered six hours ago", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		stored := snapshots{}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), stored, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		clk.advance(6 * time.Hour)

		t.Run("WHEN the caller goes away after the provider was asked", func(t *testing.T) {
			gone, leave := context.WithCancel(ctx)
			defer leave()

			deals.asked = leave
			_, err := svc.Refresh(gone, home.BlockDeals)
			deals.asked = nil

			t.Run("THEN the refresh reports the cancellation", func(t *testing.T) {
				assert.ErrorIs(t, err, context.Canceled)
			})

			t.Run("AND the attempt is recorded, but not as a failure of the block, and the data stays", func(t *testing.T) {
				snap := stored[home.BlockDeals]
				assert.Equal(t, start.Add(6*time.Hour), snap.AttemptedAt)
				assert.True(t, snap.FailedAt.IsZero())
				assert.Empty(t, snap.Error)
				assert.Equal(t, start, snap.FetchedAt)
				assert.Equal(t, []string{"Echo Valley"}, titles(snap.Featured))
			})

			t.Run("AND the provider's status is not touched", func(t *testing.T) {
				assert.Len(t, cfg.results, 1, "only the first refresh's answer")
			})
		})

		t.Run("WHEN the user asks again five minutes later", func(t *testing.T) {
			clk.advance(5 * time.Minute)

			before := len(deals.calls)
			_, err := svc.Refresh(ctx, home.BlockDeals)

			t.Run("THEN the interrupted attempt counts for the guard", func(t *testing.T) {
				assert.ErrorIs(t, err, home.ErrTooSoon)
				assert.Len(t, deals.calls, before)
			})

			t.Run("AND the page says when it can be refreshed, and the block is still fresh", func(t *testing.T) {
				h, err := svc.Home(ctx)
				require.NoError(t, err)
				assert.Equal(t, start.Add(6*time.Hour+15*time.Minute), h.Deals.RefreshableAt)
				assert.Equal(t, home.StateFresh, h.Deals.State)
			})
		})
	})
}

func TestRefresh_catalogError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a releases provider and a catalog whose storage fails", func(t *testing.T) {
		broken := errors.New("database is locked")
		releases := &fakeReleases{anticipated: []game.Release{{
			Title: "Harbor Lights",
			Date:  "2026-12-01",
		}}}
		cfg := &configs{}
		cfg.enable(releases)

		stored := snapshots{}
		svc := home.NewService(cfg, brokenCatalog{broken}, ownerOf(catalog{}), stored, prefs{},
			home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN the releases block is refreshed", func(t *testing.T) {
			_, err := svc.Refresh(ctx, home.BlockReleases)

			t.Run("THEN the storage error is returned as it is", func(t *testing.T) {
				assert.ErrorIs(t, err, broken)
			})

			t.Run("AND the provider was not asked, nor blamed in its status", func(t *testing.T) {
				assert.Zero(t, releases.calls())
				assert.Empty(t, cfg.results)
			})

			t.Run("AND nothing is stored as the block's failure", func(t *testing.T) {
				assert.NotContains(t, stored, home.BlockReleases)
			})
		})
	})
}

func TestHome_expiredDeals(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals gathered with their ends, then a day passes", func(t *testing.T) {
		ending := deal("Echo Valley", "")
		ending.Expires = start.Add(12 * time.Hour)
		lasting := deal("Harbor Lights", "")
		lasting.Expires = start.Add(48 * time.Hour)
		open := deal("Copper Coast", "")
		low := deal("Lantern Keep", "")
		low.Expires = start.Add(time.Hour)

		deals := &fakeDeals{
			featured: []game.Deal{ending, lasting, open},
			lows:     []game.Deal{low},
		}
		cfg := &configs{}
		cfg.enable(deals)

		clk := &clock{start}
		stored := snapshots{}
		svc := home.NewService(cfg, catalog{}, ownerOf(catalog{}), stored, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		clk.advance(24 * time.Hour)

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN the deals that ended are left out, and those without an end stay", func(t *testing.T) {
				assert.Equal(t, []string{"Harbor Lights", "Copper Coast"}, titles(h.Featured))
				assert.Empty(t, h.HistoricalLows)
			})

			t.Run("AND the stored snapshot is not changed", func(t *testing.T) {
				assert.Len(t, stored[home.BlockDeals].Featured, 3)
				assert.Len(t, stored[home.BlockDeals].HistoricalLows, 1)
			})
		})
	})
}
