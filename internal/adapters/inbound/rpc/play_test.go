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

func TestUpdateGame_playStatusAndRating(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game without play status or rating", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: "Outer Wilds"}))
		require.NoError(t, err)

		id := created.Msg.Game.Id
		assert.Equal(t, pb.PlayStatus_PLAY_STATUS_UNSPECIFIED, created.Msg.Game.PlayStatus)

		t.Run("WHEN it is marked as finished with five stars", func(t *testing.T) {
			_, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
				Id:         id,
				Title:      "Outer Wilds",
				PlayStatus: pb.PlayStatus_PLAY_STATUS_FINISHED,
				Rating:     5,
			}))
			require.NoError(t, err)

			t.Run("THEN reading it back returns both", func(t *testing.T) {
				got, err := c.games.GetGame(ctx, connect.NewRequest(&pb.GetGameRequest{Id: id}))
				require.NoError(t, err)
				assert.Equal(t, pb.PlayStatus_PLAY_STATUS_FINISHED, got.Msg.Game.PlayStatus)
				assert.Equal(t, int32(5), got.Msg.Game.Rating)
			})
		})

		t.Run("WHEN the rating or the status is out of range", func(t *testing.T) {
			_, badRating := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
				Id:     id,
				Title:  "Outer Wilds",
				Rating: 6,
			}))
			_, badStatus := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
				Id:         id,
				Title:      "Outer Wilds",
				PlayStatus: pb.PlayStatus(99),
			}))

			t.Run("THEN both are refused as invalid arguments", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(badRating))
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(badStatus))
			})
		})
	})
}

func TestUpdateGame_linksOnlyWhenSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game linked to Steam", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Console Quest",
			Links: map[string]string{"steam": "100"},
		}))
		require.NoError(t, err)

		t.Run("WHEN a rating is saved without links_set, from a page that holds no links", func(t *testing.T) {
			res, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
				Id:     created.Msg.Game.Id,
				Title:  "Console Quest",
				Rating: 4,
			}))
			require.NoError(t, err)

			t.Run("THEN the links are untouched", func(t *testing.T) {
				assert.Equal(t, int32(4), res.Msg.Game.Rating)
				assert.Equal(t, map[string]string{"steam": "100"}, res.Msg.Game.Links)
			})
		})
	})
}
