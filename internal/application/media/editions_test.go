package media_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// pcStore has art for PC editions of games linked to Steam, like the Steam store.
type pcStore struct{}

func (pcStore) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "pc-store",
		Kind:             provider.KindCover,
		Name:             "PC store",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (pcStore) Test(context.Context, schema.Settings) error { return nil }

func (pcStore) Applies(q media.CoverQuery) bool { return q.ForPC() && q.Links[game.LinkSteam] != "" }

func (pcStore) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	return []media.CoverCandidate{{
		URL:      "https://img.test/steam-" + q.Links[game.LinkSteam],
		Provider: "pc-store",
	}}, nil
}

// boxArt has box art for every console except the Wii, and none for add-ons; it records the
// systems it is asked for.
type boxArt struct{ asked []string }

func (*boxArt) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "boxart",
		Kind:             provider.KindCover,
		Name:             "Box art",
		EnabledByDefault: true,
		DefaultOrder:     20,
	}
}

func (*boxArt) Test(context.Context, schema.Settings) error { return nil }

func (*boxArt) Applies(q media.CoverQuery) bool { return q.System != "" && q.System != game.SystemPC }

func (b *boxArt) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	b.asked = append(b.asked, q.System)
	if q.System == "Wii" || media.IsAddOn(q.Title) {
		return nil, nil
	}

	return []media.CoverCandidate{{
		URL:      "https://img.test/box-" + q.System,
		Provider: "boxart",
	}}, nil
}

// echo "downloads" an image whose bytes are its URL, so a test sees which image a cover is.
type echo struct{}

func (echo) Fetch(_ context.Context, url string) (media.Image, error) {
	return media.Image{
		Data:        []byte(url),
		ContentType: "image/png",
	}, nil
}

type mediaEnv struct {
	svc    *media.Service
	games  game.Repository
	assets *gamedata.Store
	box    *boxArt
}

func newMediaEnv(t *testing.T, ctx context.Context) mediaEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	games, box := sqlite.NewGameRepository(db), &boxArt{}
	svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, nil, echo{}, time.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{Covers: []media.CoverProvider{pcStore{}, box}})

	return mediaEnv{
		svc:    svc,
		games:  games,
		assets: assets,
		box:    box,
	}
}

// saveGame stores a game with one copy per details.
func saveGame(t *testing.T, ctx context.Context, games game.Repository, title string, links game.Links, copies ...game.CopyDetails) *game.Game {
	t.Helper()

	g, err := game.New(title, time.Now())
	require.NoError(t, err)

	_, err = g.UpdateInfo(game.Info{
		Title: title,
		Links: links,
	}, time.Now())
	require.NoError(t, err)

	for _, d := range copies {
		_, err := g.AddCopy(d, time.Now())
		require.NoError(t, err)
	}

	require.NoError(t, games.Save(ctx, g))

	return g
}

func cover(t *testing.T, img media.Image, err error) string {
	t.Helper()
	require.NoError(t, err)

	return string(img.Data)
}

func TestEditionCovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game linked to Steam with a Steam copy, a PS3 disc and a Wii disc", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", game.Links{game.LinkSteam: "7"},
			game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "Steam",
			},
			game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			},
			game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Wii",
			})

		t.Run("WHEN each edition's cover is asked", func(t *testing.T) {
			pc, pcErr := env.svc.EditionCover(ctx, g.ID(), "PC")
			ps3, ps3Err := env.svc.EditionCover(ctx, g.ID(), "PS3")
			_, wiiErr := env.svc.EditionCover(ctx, g.ID(), "Wii")

			t.Run("THEN each edition has its own box, and box art was never asked for the PC edition", func(t *testing.T) {
				assert.Equal(t, "https://img.test/steam-7", cover(t, pc, pcErr))
				assert.Equal(t, "https://img.test/box-PS3", cover(t, ps3, ps3Err))
				assert.ErrorIs(t, wiiErr, media.ErrNoCover)
				assert.Equal(t, []string{"PS3", "Wii"}, env.box.asked)
			})

			t.Run("AND the main edition's cover is the PS3 one (a disc, first by system)", func(t *testing.T) {
				img, err := env.svc.Cover(ctx, g.ID())
				assert.Equal(t, "https://img.test/box-PS3", cover(t, img, err))
			})

			t.Run("AND asking again is served from the cache, and the Wii miss is remembered", func(t *testing.T) {
				_, _ = env.svc.EditionCover(ctx, g.ID(), "PS3")
				_, err := env.svc.EditionCover(ctx, g.ID(), "Wii")
				assert.ErrorIs(t, err, media.ErrNoCover)
				assert.Len(t, env.box.asked, 2)
			})

			t.Run("AND a system the game has no copy on has no cover, and nobody is asked", func(t *testing.T) {
				_, err := env.svc.EditionCover(ctx, g.ID(), "Switch")
				assert.ErrorIs(t, err, media.ErrNoCover)
				assert.Len(t, env.box.asked, 2)
			})

			t.Run("AND refreshing missing covers forgets only the Wii miss", func(t *testing.T) {
				n, err := env.svc.RefreshCovers(ctx, true)
				require.NoError(t, err)
				assert.Equal(t, 1, n)

				_, _ = env.svc.EditionCover(ctx, g.ID(), "Wii")
				_, _ = env.svc.EditionCover(ctx, g.ID(), "PS3")
				assert.Equal(t, []string{"PS3", "Wii", "Wii"}, env.box.asked)
			})
		})

		t.Run("WHEN the Wii edition gets a chosen cover", func(t *testing.T) {
			got, err := env.games.Get(ctx, g.ID())
			require.NoError(t, err)
			require.NoError(t, got.SetEditionCover("Wii", game.EditionCover{URL: "https://img.test/chosen"}, time.Now()))
			require.NoError(t, env.games.Save(ctx, got))
			require.NoError(t, env.svc.InvalidateEdition(ctx, g.ID(), "Wii"))

			t.Run("THEN that edition shows it and the others keep their cached box", func(t *testing.T) {
				wii, err := env.svc.EditionCover(ctx, g.ID(), "Wii")
				assert.Equal(t, "https://img.test/chosen", cover(t, wii, err))

				ps3, err := env.svc.EditionCover(ctx, g.ID(), "PS3")
				assert.Equal(t, "https://img.test/box-PS3", cover(t, ps3, err))
				assert.Len(t, env.box.asked, 3)
			})
		})

		t.Run("WHEN the candidates of the PC edition are listed THEN only the PC store proposes", func(t *testing.T) {
			candidates, _, err := env.svc.CoverCandidates(ctx, g.ID(), "PC")
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			assert.Equal(t, provider.ID("pc-store"), candidates[0].Provider)
		})

		t.Run("WHEN the candidates of a system the game has no copy on are listed THEN it is an error", func(t *testing.T) {
			_, _, err := env.svc.CoverCandidates(ctx, g.ID(), "Switch")
			assert.ErrorIs(t, err, media.ErrNoEdition)
		})
	})

	t.Run("GIVEN an add-on on PS3 nobody has art for, and its base game on PS3", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		saveGame(t, ctx, env.games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})
		dlc := saveGame(t, ctx, env.games, "Halo 3 Map Pack DLC", nil, game.CopyDetails{
			Kind:     game.KindKey,
			Platform: "PS3",
		})

		t.Run("THEN the add-on borrows the base game's PS3 box", func(t *testing.T) {
			img, err := env.svc.EditionCover(ctx, dlc.ID(), "PS3")
			assert.Equal(t, "https://img.test/box-PS3", cover(t, img, err))
		})
	})

	// cachedBeforeEditions stores a cover as versions before editions did (one per game).
	cachedBeforeEditions := func(t *testing.T, env mediaEnv, g *game.Game) {
		t.Helper()

		legacy := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(legacy, string(g.ID())+".jpg"), []byte("legacy"), 0o600))
		_, err := env.assets.MigrateLegacyCovers(legacy, map[game.ID]string{g.ID(): g.Title()})
		require.NoError(t, err)
	}

	t.Run("GIVEN a cover cached before editions for a game with a PS3 disc and a Steam copy, and no chosen cover", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", game.Links{game.LinkSteam: "7"},
			game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "Steam",
			},
			game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			})
		cachedBeforeEditions(t, env, g)

		t.Run("THEN the main (PS3) edition does not adopt it, which may be PC art, and resolves its own box", func(t *testing.T) {
			main, err := env.svc.Cover(ctx, g.ID())
			assert.Equal(t, "https://img.test/box-PS3", cover(t, main, err))
			assert.Equal(t, []string{"PS3"}, env.box.asked)

			pc, err := env.svc.EditionCover(ctx, g.ID(), "PC")
			assert.Equal(t, "https://img.test/steam-7", cover(t, pc, err))
		})

		t.Run("AND the cover from before editions is deleted", func(t *testing.T) {
			_, ok, err := env.assets.AdoptLegacyCover(media.GameRef{
				ID:    g.ID(),
				Title: g.Title(),
			}, "PS3")
			require.NoError(t, err)
			assert.False(t, ok)
		})
	})

	t.Run("GIVEN a cover cached before editions for a game with only PS3 discs", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})
		cachedBeforeEditions(t, env, g)

		t.Run("THEN its only edition adopts it without asking anyone", func(t *testing.T) {
			main, err := env.svc.Cover(ctx, g.ID())
			assert.Equal(t, "legacy", cover(t, main, err))
			assert.Empty(t, env.box.asked)
		})
	})

	t.Run("GIVEN a cover cached before editions for a game with a PS3 disc whose cover was chosen, and a Wii disc", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", nil,
			game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			},
			game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Wii",
			})
		require.NoError(t, g.SetEditionCover("PS3", game.EditionCover{URL: "https://img.test/chosen-ps3"}, time.Now()))
		require.NoError(t, env.games.Save(ctx, g))
		cachedBeforeEditions(t, env, g)

		t.Run("WHEN the Wii edition finds no cover and the missing covers are refreshed", func(t *testing.T) {
			_, err := env.svc.EditionCover(ctx, g.ID(), "Wii")
			require.ErrorIs(t, err, media.ErrNoCover)

			n, err := env.svc.RefreshCovers(ctx, true)
			require.NoError(t, err)
			assert.Equal(t, 1, n)

			t.Run("THEN the main edition, whose cover was chosen, still adopts the cover from before editions", func(t *testing.T) {
				main, err := env.svc.Cover(ctx, g.ID())
				assert.Equal(t, "legacy", cover(t, main, err))
				assert.Equal(t, []string{"Wii"}, env.box.asked)
			})
		})
	})

	t.Run("GIVEN a game without copies whose cover was chosen", func(t *testing.T) {
		env := newMediaEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Halo 3", nil)
		require.NoError(t, g.SetEditionCover("", game.EditionCover{URL: "https://img.test/chosen"}, time.Now()))
		require.NoError(t, env.games.Save(ctx, g))

		t.Run("THEN its cover is the chosen one, and nobody is asked", func(t *testing.T) {
			img, err := env.svc.Cover(ctx, g.ID())
			assert.Equal(t, "https://img.test/chosen", cover(t, img, err))
			assert.Empty(t, env.box.asked)
		})
	})

	for _, tc := range []struct {
		system string
		want   string
	}{
		{"Wii", "legacy"},                      // another edition leaves it for the main one to adopt
		{"PS3", "https://img.test/chosen-ps3"}, // the main edition resolves its chosen cover again
	} {
		t.Run("GIVEN a cover cached before editions for a game whose main edition is PS3, with a chosen cover, and a Wii disc", func(t *testing.T) {
			env := newMediaEnv(t, ctx)
			g := saveGame(t, ctx, env.games, "Halo 3", nil,
				game.CopyDetails{
					Kind:     game.KindPhysical,
					Platform: "PS3",
				},
				game.CopyDetails{
					Kind:     game.KindPhysical,
					Platform: "Wii",
				})
			require.NoError(t, g.SetEditionCover("PS3", game.EditionCover{URL: "https://img.test/chosen-ps3"}, time.Now()))
			require.NoError(t, env.games.Save(ctx, g))
			cachedBeforeEditions(t, env, g)

			t.Run("WHEN the "+tc.system+" edition is invalidated THEN the main cover is "+tc.want, func(t *testing.T) {
				require.NoError(t, env.svc.InvalidateEdition(ctx, g.ID(), tc.system))

				main, err := env.svc.Cover(ctx, g.ID())
				assert.Equal(t, tc.want, cover(t, main, err))
			})
		})
	}
}

// countingGames counts the games read, to see which work is skipped.
type countingGames struct {
	game.Repository

	gets int
}

func (c *countingGames) Get(ctx context.Context, id game.ID) (*game.Game, error) {
	c.gets++
	return c.Repository.Get(ctx, id)
}

func TestInvalidateEditionWithoutCoverFromBeforeEditions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a new game with nothing stored for it", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		games := &countingGames{Repository: sqlite.NewGameRepository(db)}
		svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, nil, echo{}, time.Now,
			slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{})
		g := saveGame(t, ctx, games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})

		t.Run("WHEN its edition is invalidated, as on its first scan, THEN the game is not read", func(t *testing.T) {
			require.NoError(t, svc.InvalidateEdition(ctx, g.ID(), "PS3"))
			assert.Zero(t, games.gets)
		})
	})
}
