package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/ownership"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.OwnershipServiceHandler = (*OwnershipHandler)(nil)

// ownershipMaxBytes bounds a lookup's request: 200 items with long titles fit easily, and a token
// holder cannot send huge bodies.
const ownershipMaxBytes = 256 << 10

// OwnershipHandler serves the ownership lookup: the web's bundle checker and the browser
// extension's store labels. It is the only service an access token may call (see authMiddleware).
type OwnershipHandler struct{ ownership *ownership.Service }

// NewOwnershipHandler builds the handler.
func NewOwnershipHandler(o *ownership.Service) *OwnershipHandler {
	return &OwnershipHandler{ownership: o}
}

// CheckOwnership answers each item with owned, possible or not owned, in order.
func (h *OwnershipHandler) CheckOwnership(ctx context.Context, req *connect.Request[pb.CheckOwnershipRequest]) (*connect.Response[pb.CheckOwnershipResponse], error) {
	items := make([]ownership.Item, 0, len(req.Msg.Items))
	for _, it := range req.Msg.Items {
		items = append(items, ownership.Item{
			Store: it.Store,
			ID:    it.Id,
			Title: it.Title,
		})
	}

	results, err := h.ownership.Check(ctx, items)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.CheckOwnershipResponse{Results: make([]*pb.OwnershipResult, 0, len(results))}

	for _, r := range results {
		out.Results = append(out.Results, ownershipResultToPB(r))
	}

	return connect.NewResponse(out), nil
}

func ownershipResultToPB(r ownership.Result) *pb.OwnershipResult {
	out := &pb.OwnershipResult{Status: string(r.Status)}
	if r.Game != nil {
		out.Game = ownedGameToPB(*r.Game)
	}

	for _, c := range r.Candidates {
		out.Candidates = append(out.Candidates, ownedGameToPB(c))
	}

	return out
}

func ownedGameToPB(m ownership.Match) *pb.OwnedGame {
	out := &pb.OwnedGame{
		Id:    string(m.ID),
		Title: m.Title,
		Path:  "#/game/" + string(m.ID),
	}

	for _, c := range m.Copies {
		out.Copies = append(out.Copies, &pb.OwnershipCopy{
			Platform:   c.Platform,
			System:     c.System,
			Kind:       string(c.Kind),
			PendingKey: c.PendingKey,
		})
	}

	return out
}
