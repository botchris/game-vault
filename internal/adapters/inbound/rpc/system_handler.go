package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/system"
	"gamevault/internal/application/transfer"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// SystemHandler implements gamevaultv1connect.SystemServiceHandler.
type SystemHandler struct {
	system   *system.Service
	transfer *transfer.Service
}

var _ gamevaultv1connect.SystemServiceHandler = (*SystemHandler)(nil)

func NewSystemHandler(s *system.Service, t *transfer.Service) *SystemHandler {
	return &SystemHandler{system: s, transfer: t}
}

func backupToPB(b system.Backup) *pb.Backup {
	return &pb.Backup{Name: b.Name, SizeBytes: b.SizeBytes, CreatedAt: ts(b.CreatedAt)}
}

func (h *SystemHandler) GetStatus(ctx context.Context, _ *connect.Request[pb.GetStatusRequest]) (*connect.Response[pb.GetStatusResponse], error) {
	st, err := h.system.Status(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.GetStatusResponse{
		Version: st.Version, ConfigDir: st.ConfigDir, DatabasePath: st.DatabasePath, StartedAt: ts(st.StartedAt),
		GameCount: int32(st.GameCount), CopyCount: int32(st.CopyCount),
	}), nil
}

func (h *SystemHandler) ListBackups(ctx context.Context, _ *connect.Request[pb.ListBackupsRequest]) (*connect.Response[pb.ListBackupsResponse], error) {
	backups, err := h.system.ListBackups(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	out := &pb.ListBackupsResponse{}
	for _, b := range backups {
		out.Backups = append(out.Backups, backupToPB(b))
	}
	return connect.NewResponse(out), nil
}

func (h *SystemHandler) CreateBackup(ctx context.Context, _ *connect.Request[pb.CreateBackupRequest]) (*connect.Response[pb.CreateBackupResponse], error) {
	b, err := h.system.CreateBackup(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.CreateBackupResponse{Backup: backupToPB(b)}), nil
}

func (h *SystemHandler) ImportCsv(ctx context.Context, req *connect.Request[pb.ImportCsvRequest]) (*connect.Response[pb.ImportCsvResponse], error) {
	rep, err := h.transfer.Import(ctx, req.Msg.Content)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&pb.ImportCsvResponse{Report: reportToPB(&rep)}), nil
}

func (h *SystemHandler) ExportCsv(ctx context.Context, _ *connect.Request[pb.ExportCsvRequest]) (*connect.Response[pb.ExportCsvResponse], error) {
	name, content, err := h.transfer.Export(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.ExportCsvResponse{Filename: name, Content: content}), nil
}
