package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
)

func TestExclusions_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a synced source with one item", func(t *testing.T) {
		c := newServer(t, &fakeProvider{copies: []game.ImportedCopy{{
			ExternalID: "fake:netflix",
			Title:      "Netflix",
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "PS4",
			},
		}}})

		src, err := c.sources.CreateSource(ctx, connect.NewRequest(&pb.CreateSourceRequest{Source: &pb.SourceInput{
			Type:     "fake",
			Enabled:  true,
			Settings: map[string]string{"token": "t"},
		}}))
		require.NoError(t, err)

		_, err = c.sources.SyncSource(ctx, connect.NewRequest(&pb.SyncSourceRequest{Id: src.Msg.Source.Id}))
		require.NoError(t, err)

		list, err := c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
		require.NoError(t, err)
		require.Len(t, list.Msg.Games, 1)
		g := list.Msg.Games[0]

		t.Run("WHEN its copy is excluded", func(t *testing.T) {
			res, err := c.games.ExcludeCopy(ctx, connect.NewRequest(&pb.ExcludeCopyRequest{
				GameId: g.Id,
				CopyId: g.Copies[0].Id,
			}))
			require.NoError(t, err)

			t.Run("THEN the emptied game is gone and the source lists the item", func(t *testing.T) {
				assert.Nil(t, res.Msg.Game)

				sources, err := c.sources.ListSources(ctx, connect.NewRequest(&pb.ListSourcesRequest{}))
				require.NoError(t, err)
				require.Len(t, sources.Msg.Sources[0].Exclusions, 1)
				assert.Equal(t, "fake:netflix", sources.Msg.Sources[0].Exclusions[0].ExternalId)
				assert.Equal(t, "Netflix", sources.Msg.Sources[0].Exclusions[0].Title)
			})

			t.Run("AND a sync reports it skipped", func(t *testing.T) {
				synced, err := c.sources.SyncSource(ctx, connect.NewRequest(&pb.SyncSourceRequest{Id: src.Msg.Source.Id}))
				require.NoError(t, err)
				assert.Equal(t, int32(1), synced.Msg.Source.LastSync.Excluded)
			})

			t.Run("AND including it twice answers not found the second time", func(t *testing.T) {
				req := &pb.IncludeCopyRequest{
					SourceId:   src.Msg.Source.Id,
					ExternalId: "fake:netflix",
				}
				_, err := c.sources.IncludeCopy(ctx, connect.NewRequest(req))
				require.NoError(t, err)

				_, err = c.sources.IncludeCopy(ctx, connect.NewRequest(req))
				assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			})
		})
	})

	t.Run("GIVEN a game added by hand", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:  "Halo 3",
			Copies: []*pb.CopyDetails{{Kind: pb.CopyKind_COPY_KIND_PHYSICAL}},
		}))
		require.NoError(t, err)

		_, err = c.games.ExcludeCopy(ctx, connect.NewRequest(&pb.ExcludeCopyRequest{
			GameId: created.Msg.Game.Id,
			CopyId: created.Msg.Game.Copies[0].Id,
		}))

		t.Run("THEN excluding its copy is refused as a precondition", func(t *testing.T) {
			assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		})
	})
}
