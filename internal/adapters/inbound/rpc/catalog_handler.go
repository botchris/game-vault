package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.CatalogServiceHandler = (*CatalogHandler)(nil)

// CatalogHandler serves the catalog of game systems.
type CatalogHandler struct{}

// NewCatalogHandler builds the handler.
func NewCatalogHandler() *CatalogHandler { return &CatalogHandler{} }

// ListSystems returns every system of the catalog, in display order.
func (h *CatalogHandler) ListSystems(context.Context, *connect.Request[pb.ListSystemsRequest]) (*connect.Response[pb.ListSystemsResponse], error) {
	out := &pb.ListSystemsResponse{}

	for _, s := range game.Systems() {
		out.Systems = append(out.Systems, &pb.SystemInfo{
			Name:    s.Name,
			Maker:   s.Maker,
			Kind:    string(s.Kind),
			Year:    int32(s.Year),
			Aliases: s.Aliases,
			Short:   s.Short,
			Modern:  s.Modern,
		})
	}

	return connect.NewResponse(out), nil
}
