package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.HomeServiceHandler = (*HomeHandler)(nil)

// HomeHandler serves the home page's external blocks.
type HomeHandler struct{ home *home.Service }

// NewHomeHandler builds the handler.
func NewHomeHandler(h *home.Service) *HomeHandler { return &HomeHandler{home: h} }

// GetHome returns the stored blocks; it never calls a service.
func (h *HomeHandler) GetHome(ctx context.Context, _ *connect.Request[pb.GetHomeRequest]) (*connect.Response[pb.GetHomeResponse], error) {
	out, err := h.home.Home(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(homeToPB(out)), nil
}

// RefreshHome gathers one block now and returns the whole home.
func (h *HomeHandler) RefreshHome(ctx context.Context, req *connect.Request[pb.RefreshHomeRequest]) (*connect.Response[pb.RefreshHomeResponse], error) {
	out, err := h.home.Refresh(ctx, home.Block(req.Msg.Block))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.RefreshHomeResponse{Home: homeToPB(out)}), nil
}

func homeToPB(h home.Home) *pb.GetHomeResponse {
	return &pb.GetHomeResponse{
		Deals:          blockToPB(h.Deals),
		Featured:       dealsToPB(h.Featured),
		HistoricalLows: dealsToPB(h.HistoricalLows),
		Releases:       blockToPB(h.Releases),
		FromSeries:     releasesToPB(h.FromSeries),
		Anticipated:    releasesToPB(h.Anticipated),
	}
}

func blockToPB(b home.BlockView) *pb.HomeBlock {
	return &pb.HomeBlock{
		State:         string(b.State),
		Provider:      b.Provider,
		FetchedAt:     ts(b.FetchedAt),
		FailedAt:      ts(b.FailedAt),
		ErrorKind:     string(b.ErrorKind),
		Error:         b.Error,
		RefreshableAt: ts(b.RefreshableAt),
	}
}

func dealsToPB(in []game.Deal) []*pb.HomeDeal {
	out := make([]*pb.HomeDeal, 0, len(in))
	for _, d := range in {
		out = append(out, &pb.HomeDeal{
			Title:         d.Title,
			Shop:          d.Shop,
			Price:         moneyToPB(d.Price),
			Regular:       moneyToPB(d.Regular),
			Cut:           int32(d.Cut),
			HistoricalLow: d.HistoricalLow,
			ExpiresAt:     ts(d.Expires),
			Url:           d.URL,
			ImageUrl:      d.ImageURL,
			OwnedOn:       d.OwnedOn,
		})
	}

	return out
}

func releasesToPB(in []game.Release) []*pb.HomeRelease {
	out := make([]*pb.HomeRelease, 0, len(in))
	for _, r := range in {
		out = append(out, &pb.HomeRelease{
			Title:         r.Title,
			ReleaseDate:   string(r.Date),
			Platforms:     r.Platforms,
			CoverUrl:      r.CoverURL,
			ArtUrl:        r.ArtURL,
			Url:           r.URL,
			BecauseGameId: string(r.Because),
			BecauseTitle:  r.BecauseTitle,
		})
	}

	return out
}
