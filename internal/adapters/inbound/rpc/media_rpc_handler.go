package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// MediaHandler implements the provider and cover services on top of the media use cases.
type MediaHandler struct {
	media *media.Service
}

var (
	_ gamevaultv1connect.ProviderServiceHandler = (*MediaHandler)(nil)
	_ gamevaultv1connect.CoverServiceHandler    = (*MediaHandler)(nil)
)

// NewMediaHandler returns the handler for the provider, cover and metadata services.
func NewMediaHandler(m *media.Service) *MediaHandler { return &MediaHandler{media: m} }

// ListProviders returns the providers of each chain in their configured order.
func (h *MediaHandler) ListProviders(ctx context.Context, req *connect.Request[pb.ListProvidersRequest]) (*connect.Response[pb.ListProvidersResponse], error) {
	views, err := h.media.Providers(ctx, provider.Kind(req.Msg.Kind))
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ListProvidersResponse{SecretPlaceholder: schema.SecretPlaceholder}
	for _, v := range views {
		out.Providers = append(out.Providers, providerToPB(v))
	}

	return connect.NewResponse(out), nil
}

// UpdateProvider enables or disables a provider and saves its settings.
func (h *MediaHandler) UpdateProvider(ctx context.Context, req *connect.Request[pb.UpdateProviderRequest]) (*connect.Response[pb.UpdateProviderResponse], error) {
	v, err := h.media.ConfigureProvider(ctx, provider.ID(req.Msg.Id), req.Msg.Enabled, schema.Settings(req.Msg.Settings))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.UpdateProviderResponse{Provider: providerToPB(v)}), nil
}

// ReorderProviders sets the order of the providers in a chain.
func (h *MediaHandler) ReorderProviders(ctx context.Context, req *connect.Request[pb.ReorderProvidersRequest]) (*connect.Response[pb.ReorderProvidersResponse], error) {
	ids := make([]provider.ID, len(req.Msg.Ids))
	for i, id := range req.Msg.Ids {
		ids[i] = provider.ID(id)
	}

	views, err := h.media.ReorderProviders(ctx, provider.Kind(req.Msg.Kind), ids)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ReorderProvidersResponse{}
	for _, v := range views {
		out.Providers = append(out.Providers, providerToPB(v))
	}

	return connect.NewResponse(out), nil
}

// TestProvider checks that a provider works with its current settings.
func (h *MediaHandler) TestProvider(ctx context.Context, req *connect.Request[pb.TestProviderRequest]) (*connect.Response[pb.TestProviderResponse], error) {
	if err := h.media.TestProvider(ctx, provider.ID(req.Msg.Id), schema.Settings(req.Msg.Settings)); err != nil {
		// A failed check is a valid test result, not an RPC failure.
		return connect.NewResponse(&pb.TestProviderResponse{
			Success: false,
			Message: err.Error(),
		}), nil
	}

	return connect.NewResponse(&pb.TestProviderResponse{Success: true}), nil
}

// ListCoverCandidates returns the covers the providers propose for a game.
func (h *MediaHandler) ListCoverCandidates(ctx context.Context, req *connect.Request[pb.ListCoverCandidatesRequest]) (*connect.Response[pb.ListCoverCandidatesResponse], error) {
	candidates, warnings, err := h.media.CoverCandidates(ctx, game.ID(req.Msg.GameId))
	if err != nil {
		return nil, toConnectError(err)
	}

	names := map[provider.ID]string{}

	if views, err := h.media.Providers(ctx, provider.KindCover); err == nil {
		for _, v := range views {
			names[v.ID()] = v.Descriptor.Name
		}
	}

	out := &pb.ListCoverCandidatesResponse{Warnings: warnings}
	for _, c := range candidates {
		out.Candidates = append(out.Candidates, &pb.CoverCandidate{
			Url:          c.URL,
			ThumbUrl:     c.ThumbURL,
			Label:        c.Label,
			ProviderId:   string(c.Provider),
			ProviderName: names[c.Provider],
		})
	}

	return connect.NewResponse(out), nil
}

// RefreshCovers clears the cached covers so they are fetched again.
func (h *MediaHandler) RefreshCovers(ctx context.Context, req *connect.Request[pb.RefreshCoversRequest]) (*connect.Response[pb.RefreshCoversResponse], error) {
	n, err := h.media.RefreshCovers(ctx, req.Msg.MissingOnly)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.RefreshCoversResponse{Games: int32(n)}), nil
}
