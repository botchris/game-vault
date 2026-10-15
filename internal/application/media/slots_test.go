package media_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// queued is a console box art provider whose lookups wait until released, like lookups queued
// behind a throttled service; it signals each lookup it starts.
type queued struct {
	started chan struct{}
	release chan struct{}
}

func (*queued) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "queued",
		Kind:             provider.KindCover,
		Name:             "Queued",
		EnabledByDefault: true,
		DefaultOrder:     20,
	}
}

func (*queued) Test(context.Context, schema.Settings) error { return nil }

func (*queued) Applies(q media.GameQuery) bool { return q.System != "" && q.System != game.SystemPC }

func (p *queued) Covers(context.Context, media.GameQuery, schema.Settings) ([]media.CoverCandidate, error) {
	p.started <- struct{}{}

	<-p.release

	return nil, nil
}

func TestCoverLookups_doNotHoldDownloadSlots(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN eight console editions whose box art lookups are queued behind a slow service", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		slow := &queued{
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		games := sqlite.NewGameRepository(db)
		svc := newServices(t, db, games, assets, echo{}, []providers.Impl{pcStore{}, slow})

		const waiting = 8

		for i := range waiting {
			g := saveGame(t, ctx, games, "Console game "+strconv.Itoa(i), nil, game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			})

			go func() { _, _ = svc.Covers.EditionCover(ctx, g.ID(), "PS3") }()
		}

		t.Cleanup(func() { close(slow.release) })

		for range waiting {
			select {
			case <-slow.started:
			case <-ctx.Done():
				t.Fatal("the console lookups did not all start: they wait for a download slot")
			}
		}

		pc := saveGame(t, ctx, games, "Hades", game.Links{game.LinkSteam: "1145360"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})

		t.Run("WHEN a PC edition's cover is asked meanwhile", func(t *testing.T) {
			done := make(chan string, 1)

			go func() {
				img, err := svc.Covers.EditionCover(ctx, pc.ID(), "PC")
				if err != nil {
					done <- err.Error()
					return
				}

				done <- string(img.Data)
			}()

			t.Run("THEN it is served without waiting for them", func(t *testing.T) {
				select {
				case got := <-done:
					assert.Equal(t, "https://img.test/steam-1145360", got)
				case <-time.After(3 * time.Second):
					t.Fatal("the PC cover waited behind the queued console lookups")
				}
			})
		})
	})
}
