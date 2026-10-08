package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"gamevault/internal/application/logs"
	"gamevault/internal/domain/settings"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// LogHandler implements gamevaultv1connect.LogServiceHandler.
type LogHandler struct {
	logs *logs.Service
}

var _ gamevaultv1connect.LogServiceHandler = (*LogHandler)(nil)

func NewLogHandler(l *logs.Service) *LogHandler { return &LogHandler{logs: l} }

func logSettingsToPB(l settings.Logging) *pb.LogSettings {
	return &pb.LogSettings{Level: l.Level, MaxFileSizeMb: uint32(l.MaxFileSizeMB), MaxFiles: uint32(l.MaxFiles)}
}

func (h *LogHandler) GetLogSettings(ctx context.Context, _ *connect.Request[pb.GetLogSettingsRequest]) (*connect.Response[pb.GetLogSettingsResponse], error) {
	l, err := h.logs.Settings(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.GetLogSettingsResponse{Settings: logSettingsToPB(l)}), nil
}

func (h *LogHandler) UpdateLogSettings(ctx context.Context, req *connect.Request[pb.UpdateLogSettingsRequest]) (*connect.Response[pb.UpdateLogSettingsResponse], error) {
	in := req.Msg.Settings
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("settings are required"))
	}
	l, err := h.logs.UpdateSettings(ctx, settings.Logging{Level: in.Level, MaxFileSizeMB: int(in.MaxFileSizeMb), MaxFiles: int(in.MaxFiles)})
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.UpdateLogSettingsResponse{Settings: logSettingsToPB(l)}), nil
}

func (h *LogHandler) ListLogFiles(ctx context.Context, _ *connect.Request[pb.ListLogFilesRequest]) (*connect.Response[pb.ListLogFilesResponse], error) {
	files, err := h.logs.ListFiles(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	out := &pb.ListLogFilesResponse{}
	for _, f := range files {
		out.Files = append(out.Files, &pb.LogFile{Name: f.Name, SizeBytes: f.SizeBytes, ModifiedAt: ts(f.ModifiedAt), Current: f.Current})
	}
	return connect.NewResponse(out), nil
}

func (h *LogHandler) GetLogFile(ctx context.Context, req *connect.Request[pb.GetLogFileRequest]) (*connect.Response[pb.GetLogFileResponse], error) {
	content, truncated, err := h.logs.ReadFile(ctx, req.Msg.Name, int(req.Msg.TailLines))
	switch {
	case errors.Is(err, logs.ErrInvalidName):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, logs.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.GetLogFileResponse{Name: req.Msg.Name, Content: content, Truncated: truncated}), nil
}

func (h *LogHandler) ClearLogFiles(ctx context.Context, _ *connect.Request[pb.ClearLogFilesRequest]) (*connect.Response[pb.ClearLogFilesResponse], error) {
	n, err := h.logs.ClearArchived(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.ClearLogFilesResponse{Deleted: int32(n)}), nil
}
