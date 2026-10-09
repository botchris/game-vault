package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/media"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.LookupServiceHandler = (*MediaHandler)(nil)

func suggestionsToPB(in []media.Suggestion) []*pb.GameSuggestion {
	out := make([]*pb.GameSuggestion, 0, len(in))
	for _, s := range in {
		out = append(out, &pb.GameSuggestion{
			Title:      s.Title,
			Platform:   s.Platform,
			CoverUrl:   s.CoverURL,
			ThumbUrl:   s.ThumbURL,
			Label:      s.Label,
			ProviderId: string(s.Provider),
		})
	}

	return out
}

func gameRefsToPB(in []media.GameRef) []*pb.GameRef {
	out := make([]*pb.GameRef, 0, len(in))
	for _, g := range in {
		out = append(out, &pb.GameRef{
			Id:    string(g.ID),
			Title: g.Title,
		})
	}

	return out
}

// IdentifyBarcode identifies a physical game from its barcode.
func (h *MediaHandler) IdentifyBarcode(ctx context.Context, req *connect.Request[pb.IdentifyBarcodeRequest]) (*connect.Response[pb.IdentifyBarcodeResponse], error) {
	res, err := h.media.IdentifyBarcode(ctx, req.Msg.Barcode)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.IdentifyBarcodeResponse{
		Barcode:     string(res.Code),
		Suggestions: suggestionsToPB(res.Suggestions),
		Existing:    gameRefsToPB(res.Existing),
		Warnings:    res.Warnings,
	}
	for _, o := range res.Owned {
		out.Owned = append(out.Owned, &pb.OwnedCopy{
			Game: &pb.GameRef{
				Id:    string(o.Game.ID),
				Title: o.Game.Title,
			},
			CopyId:   string(o.CopyID),
			Platform: o.Platform,
		})
	}

	if m := res.Match; m != nil {
		out.Match = &pb.BarcodeMatch{
			Raw:        m.Raw,
			Title:      m.Title,
			Platform:   m.Platform,
			Edition:    m.Edition,
			ImageUrl:   m.ImageURL,
			ProviderId: string(m.Provider),
		}
	}

	return connect.NewResponse(out), nil
}

// SuggestGames proposes games matching a partial title, for quick registration.
func (h *MediaHandler) SuggestGames(ctx context.Context, req *connect.Request[pb.SuggestGamesRequest]) (*connect.Response[pb.SuggestGamesResponse], error) {
	sugg, existing, warnings, err := h.media.SuggestGames(ctx, req.Msg.Title, req.Msg.Platform)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.SuggestGamesResponse{
		Suggestions: suggestionsToPB(sugg),
		Existing:    gameRefsToPB(existing),
		Warnings:    warnings,
	}), nil
}
