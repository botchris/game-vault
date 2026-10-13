package catalog_test

import (
	"context"
	"path/filepath"
	gosync "sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/game"
)

// editDuringRead is a game repository that, the first time armed is set and a game is read, starts
// another edit (onRead) and waits for it before returning: without a transaction the edit finishes
// at once and lands between the automatic-links use case's read and its save. With one, the edit
// is held back by the single database connection and can never finish here, so the wait ends at a
// bound instead: a passing run costs that bound, a failing run fails deterministically.
type editDuringRead struct {
	game.Repository

	mu     gosync.Mutex
	armed  bool
	onRead func()
	done   chan struct{}
}

func (r *editDuringRead) Get(ctx context.Context, id game.ID) (*game.Game, error) {
	g, err := r.Repository.Get(ctx, id)

	r.mu.Lock()
	fire := r.armed
	r.armed = false
	r.mu.Unlock()

	if fire {
		go func() {
			r.onRead()
			close(r.done)
		}()

		select {
		case <-r.done:
		case <-time.After(100 * time.Millisecond):
		}
	}

	return g, err
}

func TestAddAutoLinks_keepsAConcurrentEdit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	t.Run("GIVEN a PS4 disc's game, and a disc added while automatic links are being saved", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		repo := &editDuringRead{
			Repository: sqlite.NewGameRepository(db),
			done:       make(chan struct{}),
		}
		svc := catalog.NewService(repo, db)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Console Quest"}, []game.CopyDetails{{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		}})
		require.NoError(t, err)

		repo.onRead = func() {
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS5",
			}, nil)
			assert.NoError(t, err)
		}
		repo.armed = true

		t.Run("WHEN the links are added", func(t *testing.T) {
			added, err := svc.AddAutoLinks(ctx, g.ID(), game.Links{game.LinkSteam: "100"}, time.Now())
			require.NoError(t, err)
			<-repo.done

			got, err := svc.GetGame(ctx, g.ID())
			require.NoError(t, err)

			t.Run("THEN both the link and the other edit are kept: the transaction made the edit wait", func(t *testing.T) {
				assert.Equal(t, game.Links{game.LinkSteam: "100"}, added)
				assert.Equal(t, "100", got.Links()[game.LinkSteam])
				assert.Equal(t, []string{game.LinkSteam}, got.AutoLinks())
				assert.Len(t, got.Copies(), 2)
				assert.False(t, got.AutoLinkTriedAt().IsZero())
			})
		})
	})
}

func TestUpdateGame_linksOnlyWhenSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game that got an automatic Steam link after a page loaded it", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		svc := catalog.NewService(sqlite.NewGameRepository(db), db)
		g, err := svc.CreateGame(ctx, game.Info{
			Title: "Console Quest",
			Links: game.Links{"psn": "PPSA0001"},
		}, []game.CopyDetails{{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		}})
		require.NoError(t, err)

		outOfDate := g.Info() // what the page holds: no Steam link
		_, err = svc.AddAutoLinks(ctx, g.ID(), game.Links{game.LinkSteam: "100"}, time.Now())
		require.NoError(t, err)

		t.Run("WHEN the page saves a rating without setting the links", func(t *testing.T) {
			outOfDate.Rating = 4
			got, err := svc.UpdateGameKeepLinks(ctx, g.ID(), outOfDate)
			require.NoError(t, err)

			t.Run("THEN the links stay as they are, and nothing is rejected", func(t *testing.T) {
				assert.Equal(t, game.Rating(4), got.Rating())
				assert.Equal(t, game.Links{"psn": "PPSA0001", game.LinkSteam: "100"}, got.Links())
				assert.Equal(t, []string{game.LinkSteam}, got.AutoLinks())
				assert.Empty(t, got.RejectedLinks())
			})
		})

		t.Run("WHEN the Edit tab saves the links without the Steam one", func(t *testing.T) {
			info := outOfDate
			info.Links = game.Links{"psn": "PPSA0001"}
			got, err := svc.UpdateGame(ctx, g.ID(), info)
			require.NoError(t, err)

			t.Run("THEN the automatic link is removed and rejected, as the user asked", func(t *testing.T) {
				assert.Equal(t, game.Links{"psn": "PPSA0001"}, got.Links())
				assert.Equal(t, []game.LinkRef{{
					Key: game.LinkSteam,
					ID:  "100",
				}}, got.RejectedLinks())
			})
		})
	})
}
