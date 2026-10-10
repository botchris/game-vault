package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/sync"
	"gamevault/internal/domain/source"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// SourceHandler implements gamevaultv1connect.SourceServiceHandler on top of the sync use cases.
type SourceHandler struct {
	sync *sync.Service
}

var _ gamevaultv1connect.SourceServiceHandler = (*SourceHandler)(nil)

// NewSourceHandler returns the SourceService handler backed by the sync service.
func NewSourceHandler(s *sync.Service) *SourceHandler { return &SourceHandler{sync: s} }

func (h *SourceHandler) toPB(v sync.SourceView) *pb.Source {
	d, _ := h.sync.Descriptor(v.Type()) // an unknown type masks everything, which is safe
	return sourceToPB(v, d)
}

// ListSourceTypes returns the kinds of source that can be created and their fields.
func (h *SourceHandler) ListSourceTypes(context.Context, *connect.Request[pb.ListSourceTypesRequest]) (*connect.Response[pb.ListSourceTypesResponse], error) {
	out := &pb.ListSourceTypesResponse{SecretPlaceholder: source.SecretPlaceholder}
	for _, d := range h.sync.Types() {
		out.Types = append(out.Types, descriptorToPB(d))
	}

	return connect.NewResponse(out), nil
}

// ListSources returns the configured sources with their last scan status.
func (h *SourceHandler) ListSources(ctx context.Context, _ *connect.Request[pb.ListSourcesRequest]) (*connect.Response[pb.ListSourcesResponse], error) {
	views, err := h.sync.List(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ListSourcesResponse{}
	for _, v := range views {
		out.Sources = append(out.Sources, h.toPB(v))
	}

	return connect.NewResponse(out), nil
}

// CreateSource adds a source.
func (h *SourceHandler) CreateSource(ctx context.Context, req *connect.Request[pb.CreateSourceRequest]) (*connect.Response[pb.CreateSourceResponse], error) {
	in := req.Msg.Source
	if in == nil {
		in = &pb.SourceInput{}
	}

	v, err := h.sync.Create(ctx, source.Type(in.Type), configFromPB(in))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.CreateSourceResponse{Source: h.toPB(v)}), nil
}

// UpdateSource changes the name, settings or credentials of a source.
func (h *SourceHandler) UpdateSource(ctx context.Context, req *connect.Request[pb.UpdateSourceRequest]) (*connect.Response[pb.UpdateSourceResponse], error) {
	v, err := h.sync.Update(ctx, source.ID(req.Msg.Id), configFromPB(req.Msg.Source))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.UpdateSourceResponse{Source: h.toPB(v)}), nil
}

// DeleteSource removes a source.
func (h *SourceHandler) DeleteSource(ctx context.Context, req *connect.Request[pb.DeleteSourceRequest]) (*connect.Response[pb.DeleteSourceResponse], error) {
	if err := h.sync.Delete(ctx, source.ID(req.Msg.Id), req.Msg.DeleteCopies); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.DeleteSourceResponse{}), nil
}

// IncludeCopy takes an item off a source's removed list.
func (h *SourceHandler) IncludeCopy(ctx context.Context, req *connect.Request[pb.IncludeCopyRequest]) (*connect.Response[pb.IncludeCopyResponse], error) {
	if err := h.sync.IncludeCopy(ctx, source.ID(req.Msg.SourceId), req.Msg.ExternalId); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.IncludeCopyResponse{}), nil
}

// TestSource checks that a source's credentials work without scanning it.
func (h *SourceHandler) TestSource(ctx context.Context, req *connect.Request[pb.TestSourceRequest]) (*connect.Response[pb.TestSourceResponse], error) {
	in := req.Msg.Source
	if in == nil {
		in = &pb.SourceInput{}
	}

	if err := h.sync.Test(ctx, source.ID(req.Msg.Id), source.Type(in.Type), configFromPB(in)); err != nil {
		// A failed connection is a valid test result, not an RPC failure.
		return connect.NewResponse(&pb.TestSourceResponse{
			Success: false,
			Message: err.Error(),
		}), nil
	}

	return connect.NewResponse(&pb.TestSourceResponse{Success: true}), nil
}

// SyncSource starts a scan of one source.
func (h *SourceHandler) SyncSource(ctx context.Context, req *connect.Request[pb.SyncSourceRequest]) (*connect.Response[pb.SyncSourceResponse], error) {
	v, err := h.sync.Sync(ctx, source.ID(req.Msg.Id))
	if v.Source == nil {
		return nil, toConnectError(err)
	}
	// Fetch errors are recorded in last_sync; the source itself is still returned.
	return connect.NewResponse(&pb.SyncSourceResponse{Source: h.toPB(v)}), nil
}

// SyncAllSources starts a scan of every enabled source.
func (h *SourceHandler) SyncAllSources(ctx context.Context, _ *connect.Request[pb.SyncAllSourcesRequest]) (*connect.Response[pb.SyncAllSourcesResponse], error) {
	views, err := h.sync.SyncAll(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.SyncAllSourcesResponse{}
	for _, v := range views {
		out.Sources = append(out.Sources, h.toPB(v))
	}

	return connect.NewResponse(out), nil
}
