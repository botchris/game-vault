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

func TestAddScannedCopies_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	disc := func(platform, barcode string) *pb.CopyDetails {
		return &pb.CopyDetails{
			Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
			Status:   pb.CopyStatus_COPY_STATUS_OWNED,
			Platform: platform,
			Barcode:  barcode,
		}
	}

	t.Run("GIVEN two discs of a new game and one with a broken barcode", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		res, err := c.games.AddScannedCopies(ctx, connect.NewRequest(&pb.AddScannedCopiesRequest{Items: []*pb.ScannedCopy{
			{
				ClientId: "r1:0",
				Title:    "Dead Space 3",
				Details:  disc("Xbox 360", "5030934110075"),
			},
			{
				ClientId: "r2:0",
				Title:    "Dead Space 3",
				Details:  disc("PS3", "5030941110075"),
			},
			{
				ClientId: "r3:0",
				Title:    "Halo 3",
				Details:  disc("Xbox 360", "123"),
			},
		}}))
		require.NoError(t, err)

		t.Run("THEN the game is created once with both discs", func(t *testing.T) {
			require.Len(t, res.Msg.Games, 1)
			assert.Len(t, res.Msg.Games[0].Copies, 2)
		})

		t.Run("AND every item has a result, the broken one with its error", func(t *testing.T) {
			byID := map[string]*pb.ScannedCopyResult{}
			for _, r := range res.Msg.Results {
				byID[r.ClientId] = r
			}

			require.Len(t, byID, 3)
			assert.Empty(t, byID["r1:0"].Error)
			assert.Equal(t, res.Msg.Games[0].Id, byID["r2:0"].GameId)
			assert.NotEmpty(t, byID["r2:0"].CopyId)
			assert.NotEmpty(t, byID["r3:0"].Error)
		})
	})

	t.Run("GIVEN more items than the limit", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		items := make([]*pb.ScannedCopy, 201)

		for i := range items {
			items[i] = &pb.ScannedCopy{
				Title:   "X",
				Details: disc("PS3", ""),
			}
		}

		_, err := c.games.AddScannedCopies(ctx, connect.NewRequest(&pb.AddScannedCopiesRequest{Items: items}))

		t.Run("THEN the request is refused as invalid", func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	})
}
