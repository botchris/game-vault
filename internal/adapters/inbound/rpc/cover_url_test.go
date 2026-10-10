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

func TestEditionCoverURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	const bad = "javascript:alert(1)"

	t.Run("GIVEN a game with a PS3 disc whose edition has a cover URL", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Halo 3",
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
				Platform: "PS3",
			}},
		}))
		require.NoError(t, err)

		id := created.Msg.Game.Id
		_, err = c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
			GameId: id,
			System: "PS3",
			Cover:  &pb.SetEditionCoverRequest_Url{Url: "https://example.test/halo.jpg"},
		}))
		require.NoError(t, err)

		t.Run("WHEN a cover URL that is not http(s) is chosen", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: id,
				System: "PS3",
				Cover:  &pb.SetEditionCoverRequest_Url{Url: bad},
			}))

			t.Run("THEN it is refused and the edition keeps its cover", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

				got, err := c.games.GetGame(ctx, connect.NewRequest(&pb.GetGameRequest{Id: id}))
				require.NoError(t, err)
				assert.Equal(t, "https://example.test/halo.jpg", got.Msg.Game.Editions[0].CoverUrl)
			})
		})
	})
}
