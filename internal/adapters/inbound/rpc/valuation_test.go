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

func TestValuation_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a physical copy with a known barcode and euros as the default currency", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{Preferences: &pb.Preferences{Currency: "EUR"}}))
		require.NoError(t, err)

		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Dead Space 3",
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
				Platform: "Xbox 360",
				Barcode:  "5030934110075",
			}},
		}))
		require.NoError(t, err)

		g := created.Msg.Game

		t.Run("WHEN its price is estimated", func(t *testing.T) {
			res, err := c.valuation.EstimateCopy(ctx, connect.NewRequest(&pb.EstimateCopyRequest{
				GameId: g.Id,
				CopyId: g.Copies[0].Id,
			}))
			require.NoError(t, err)

			t.Run("THEN the copy shows the estimate and its next date", func(t *testing.T) {
				cp := res.Msg.Game.Copies[0]
				require.Len(t, cp.Estimates, 1)
				assert.Equal(t, "fake-prices", cp.Estimates[0].Provider)
				assert.Equal(t, int64(2000), cp.Estimates[0].Sell.AmountMinor)
				assert.NotNil(t, cp.NextValuation)
				assert.NotNil(t, cp.ValuedAt)
			})

			t.Run("AND the collection is worth it", func(t *testing.T) {
				v, err := c.valuation.GetCollectionValue(ctx, connect.NewRequest(&pb.GetCollectionValueRequest{}))
				require.NoError(t, err)
				assert.Equal(t, "EUR", v.Msg.Currency)
				require.Len(t, v.Msg.Totals, 1)
				assert.Equal(t, int64(2000), v.Msg.Totals[0].SellMinor)
				assert.Equal(t, int64(600), v.Msg.Totals[0].BuyCashMinor)
				assert.Equal(t, int32(1), v.Msg.Totals[0].Copies)
			})
		})

		t.Run("WHEN a copy without a barcode is estimated", func(t *testing.T) {
			added, err := c.games.AddCopy(ctx, connect.NewRequest(&pb.AddCopyRequest{
				GameId:  g.Id,
				Details: &pb.CopyDetails{Kind: pb.CopyKind_COPY_KIND_PHYSICAL},
			}))
			require.NoError(t, err)

			_, err = c.valuation.EstimateCopy(ctx, connect.NewRequest(&pb.EstimateCopyRequest{
				GameId: g.Id,
				CopyId: added.Msg.Game.Copies[1].Id,
			}))

			t.Run("THEN it is refused, saying to add the barcode", func(t *testing.T) {
				assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
			})
		})
	})
}
