package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func TestGameCoverURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	const bad = "javascript:alert(1)"

	t.Run("WHEN a game is created with a cover URL that is not http(s)", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		_, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:    "Halo 3",
			CoverUrl: bad,
		}))

		t.Run("THEN it is refused and no game is created, so a retry makes no duplicate", func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

			list, err := c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
			require.NoError(t, err)
			assert.Empty(t, list.Msg.Games)
		})
	})

	t.Run("GIVEN a game without copies created with a cover URL", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:    "Halo 3",
			CoverUrl: "https://example.test/halo.jpg",
		}))
		require.NoError(t, err)

		t.Run("THEN it keeps the cover", func(t *testing.T) {
			assert.Equal(t, "https://example.test/halo.jpg", created.Msg.Game.CoverUrl)
		})

		t.Run("WHEN it is updated with another title and a cover URL that is not http(s)", func(t *testing.T) {
			_, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
				Id:       created.Msg.Game.Id,
				Title:    "Halo 3 (2007)",
				CoverUrl: bad,
			}))

			t.Run("THEN it is refused and nothing changed", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

				got, err := c.games.GetGame(ctx, connect.NewRequest(&pb.GetGameRequest{Id: created.Msg.Game.Id}))
				require.NoError(t, err)
				assert.Equal(t, "Halo 3", got.Msg.Game.Title)
				assert.Equal(t, "https://example.test/halo.jpg", got.Msg.Game.CoverUrl)
			})
		})
	})
}
