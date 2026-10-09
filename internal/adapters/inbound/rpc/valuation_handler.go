package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// ValuationHandler implements gamevaultv1connect.ValuationServiceHandler on top of the valuation
// use cases.
type ValuationHandler struct {
	valuation *valuation.Service
}

var _ gamevaultv1connect.ValuationServiceHandler = (*ValuationHandler)(nil)

// NewValuationHandler returns the ValuationService handler.
func NewValuationHandler(v *valuation.Service) *ValuationHandler {
	return &ValuationHandler{valuation: v}
}

// EstimateCopy asks every enabled price provider for a copy's price now.
func (h *ValuationHandler) EstimateCopy(ctx context.Context, req *connect.Request[pb.EstimateCopyRequest]) (*connect.Response[pb.EstimateCopyResponse], error) {
	g, warnings, err := h.valuation.EstimateCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.EstimateCopyResponse{
		Game:     gameToPB(g),
		Warnings: warnings,
	}), nil
}

// GetCollectionValue returns the collection's value per source.
func (h *ValuationHandler) GetCollectionValue(ctx context.Context, _ *connect.Request[pb.GetCollectionValueRequest]) (*connect.Response[pb.GetCollectionValueResponse], error) {
	v, err := h.valuation.CollectionValue(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.GetCollectionValueResponse{Currency: v.Currency}
	for _, t := range v.Totals {
		out.Totals = append(out.Totals, &pb.ProviderTotal{
			Provider:       string(t.Provider),
			Name:           t.Name,
			SellMinor:      t.Sell,
			BuyCashMinor:   t.BuyCash,
			BuyCreditMinor: t.BuyCredit,
			Copies:         int32(t.Copies),
			OtherCurrency:  int32(t.OtherCurrency),
		})
	}

	return connect.NewResponse(out), nil
}
