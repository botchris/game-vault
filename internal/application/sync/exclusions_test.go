package sync_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// listing is a library source with a fixed list of items; during runs inside Fetch, as if the
// user acted while the store was answering.
type listing struct {
	items  []game.ImportedCopy
	during func()
}

func (l *listing) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "ls",
		Name: "Listing",
	}
}

func (l *listing) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	if l.during != nil {
		l.during()
	}

	return l.items, nil, nil
}

func (l *listing) Test(context.Context, source.Settings) error { return nil }

func item(id, title string) game.ImportedCopy {
	return game.ImportedCopy{
		ExternalID: id,
		Title:      title,
		Details: game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		},
	}
}

func TestExclusions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*sync.Service, *listing, game.Repository, source.ID) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		games := sqlite.NewGameRepository(db)
		p := &listing{items: []game.ImportedCopy{item("ls:netflix", "Netflix"), item("ls:journey", "Journey")}}
		svc := sync.NewService(sqlite.NewSourceRepository(db), games, db, nil, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

		v, err := svc.Create(ctx, "ls", source.Config{Enabled: true})
		require.NoError(t, err)
		_, err = svc.Sync(ctx, v.ID())
		require.NoError(t, err)

		return svc, p, games, v.ID()
	}

	copyOf := func(t *testing.T, games game.Repository, title string) (game.ID, game.ID) {
		t.Helper()

		list, err := games.List(ctx)
		require.NoError(t, err)

		for _, g := range list {
			if g.Title() == title {
				return g.ID(), g.Copies()[0].ID
			}
		}

		t.Fatalf("no game %q", title)

		return "", ""
	}

	titles := func(t *testing.T, games game.Repository) []string {
		t.Helper()

		list, err := games.List(ctx)
		require.NoError(t, err)

		out := make([]string, 0, len(list))
		for _, g := range list {
			out = append(out, g.Title())
		}

		return out
	}

	t.Run("GIVEN a synced source WHEN the user removes an item", func(t *testing.T) {
		svc, _, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")

		g, err := svc.ExcludeCopy(ctx, gameID, copyID)
		require.NoError(t, err)

		t.Run("THEN its game, left empty, is deleted and the source lists the item", func(t *testing.T) {
			assert.Nil(t, g)
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))

			v, err := svc.Get(ctx, id)
			require.NoError(t, err)
			require.Len(t, v.Exclusions(), 1)
			assert.Equal(t, "ls:netflix", v.Exclusions()[0].ExternalID)
			assert.Equal(t, "Netflix", v.Exclusions()[0].Title)
		})

		t.Run("AND the next sync skips it and counts it", func(t *testing.T) {
			v, err := svc.Sync(ctx, id)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))
			assert.Equal(t, 1, v.LastSync().Excluded)
		})

		t.Run("AND bringing it back imports it on the next sync", func(t *testing.T) {
			require.NoError(t, svc.IncludeCopy(ctx, id, "ls:netflix"))
			_, err := svc.Sync(ctx, id)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"Journey", "Netflix"}, titles(t, games))
			assert.ErrorIs(t, svc.IncludeCopy(ctx, id, "ls:netflix"), sync.ErrNotExcluded)
		})
	})

	t.Run("GIVEN a removed item WHEN the store reports it under a new id format", func(t *testing.T) {
		svc, p, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")
		_, err := svc.ExcludeCopy(ctx, gameID, copyID)
		require.NoError(t, err)

		renamed := item("ls:v2:netflix", "Netflix")
		renamed.PreviousExternalID = "ls:netflix"
		p.items = []game.ImportedCopy{renamed, item("ls:journey", "Journey")}

		_, err = svc.Sync(ctx, id)
		require.NoError(t, err)

		t.Run("THEN it stays out", func(t *testing.T) {
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))
		})
	})

	t.Run("GIVEN a removed item whose old id the store now gives to several items", func(t *testing.T) {
		svc, p, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")
		_, err := svc.ExcludeCopy(ctx, gameID, copyID)
		require.NoError(t, err)

		// Humble's old ids named an order, not a key: every key of the order reports the same one.
		netflix := item("ls:v2:netflix", "Netflix")
		netflix.PreviousExternalID = "ls:netflix"
		spotify := item("ls:v2:spotify", "Spotify")
		spotify.PreviousExternalID = "ls:netflix"
		p.items = []game.ImportedCopy{netflix, spotify, item("ls:journey", "Journey")}

		v, err := svc.Sync(ctx, id)
		require.NoError(t, err)

		t.Run("THEN only the item with the removed title stays out", func(t *testing.T) {
			assert.ElementsMatch(t, []string{"Journey", "Spotify"}, titles(t, games))
			assert.Equal(t, 1, v.LastSync().Excluded)
		})
	})

	t.Run("GIVEN the user removes an item while a sync is fetching", func(t *testing.T) {
		svc, p, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")
		p.during = func() {
			_, err := svc.ExcludeCopy(ctx, gameID, copyID)
			require.NoError(t, err)

			p.during = nil
		}

		_, err := svc.Sync(ctx, id)
		require.NoError(t, err)

		t.Run("THEN the sync keeps the exclusion and does not bring the item back", func(t *testing.T) {
			v, err := svc.Get(ctx, id)
			require.NoError(t, err)
			assert.Len(t, v.Exclusions(), 1)
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))
		})
	})

	t.Run("GIVEN a copy added by hand", func(t *testing.T) {
		svc, _, games, _ := setup(t)
		gameID, _ := copyOf(t, games, "Journey")
		g, err := games.Get(ctx, gameID)
		require.NoError(t, err)
		manual, err := g.AddCopy(game.CopyDetails{Kind: game.KindPhysical}, time.Now())
		require.NoError(t, err)
		require.NoError(t, games.Save(ctx, g))

		_, err = svc.ExcludeCopy(ctx, gameID, manual.ID)

		t.Run("THEN it cannot be excluded", func(t *testing.T) {
			assert.ErrorIs(t, err, sync.ErrNotImported)
		})
	})
}

// expired is a source whose stored session no longer works: every scan fails without rotating it.
// during runs inside Fetch, as if the user signed in again while the store was answering.
type expired struct{ during func() }

func (e *expired) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "ex",
		Name: "Expired",
		Fields: []source.Field{{
			Key:  "session",
			Kind: source.FieldState,
		}},
	}
}

func (e *expired) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	if e.during != nil {
		e.during()
	}

	return nil, nil, errors.New("signed out")
}

func (e *expired) Test(context.Context, source.Settings) error { return nil }

func TestSyncKeepsASessionTheUserRenewedMeanwhile(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	sources := sqlite.NewSourceRepository(db)
	p := &expired{}
	svc := sync.NewService(sources, sqlite.NewGameRepository(db), db, nil, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

	v, err := svc.Create(ctx, "ex", source.Config{Enabled: true})
	require.NoError(t, err)

	src, err := sources.Get(ctx, v.ID())
	require.NoError(t, err)
	src.ReplaceSettings(p.Descriptor(), source.Settings{"session": "old"}, time.Now())
	require.NoError(t, sources.Save(ctx, src))

	t.Run("GIVEN a scan of an expired session WHEN the user signs in again while it runs", func(t *testing.T) {
		p.during = func() {
			fresh, err := sources.Get(ctx, v.ID())
			require.NoError(t, err)
			fresh.ReplaceSettings(p.Descriptor(), source.Settings{"session": "new"}, time.Now())
			require.NoError(t, sources.Save(ctx, fresh))
		}

		_, err := svc.Sync(ctx, v.ID())
		require.Error(t, err)

		t.Run("THEN the scan, which rotated nothing, leaves the new session alone", func(t *testing.T) {
			got, err := sources.Get(ctx, v.ID())
			require.NoError(t, err)
			assert.Equal(t, "new", got.Settings()["session"])
		})
	})
}
