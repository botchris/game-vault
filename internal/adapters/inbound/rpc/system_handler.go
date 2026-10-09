package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/system"
	"gamevault/internal/application/transfer"
	"gamevault/internal/domain/settings"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// SystemHandler implements gamevaultv1connect.SystemServiceHandler.
type SystemHandler struct {
	system   *system.Service
	transfer *transfer.Service
}

var _ gamevaultv1connect.SystemServiceHandler = (*SystemHandler)(nil)

// NewSystemHandler returns the SystemService handler backed by the system and transfer services.
func NewSystemHandler(s *system.Service, t *transfer.Service) *SystemHandler {
	return &SystemHandler{
		system:   s,
		transfer: t,
	}
}

func backupToPB(b system.Backup) *pb.Backup {
	return &pb.Backup{
		Name:       b.Name,
		SizeBytes:  b.SizeBytes,
		CreatedAt:  ts(b.CreatedAt),
		PhotoCount: int32(b.Photos),
	}
}

// GetStatus returns the server version and catalog totals.
func (h *SystemHandler) GetStatus(ctx context.Context, _ *connect.Request[pb.GetStatusRequest]) (*connect.Response[pb.GetStatusResponse], error) {
	st, err := h.system.Status(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.GetStatusResponse{
		Version:      st.Version,
		ConfigDir:    st.ConfigDir,
		DatabasePath: st.DatabasePath,
		StartedAt:    ts(st.StartedAt),
		GameCount:    int32(st.GameCount),
		CopyCount:    int32(st.CopyCount),
	}), nil
}

// ListBackups lists the database backups and the size of the photo store they share.
func (h *SystemHandler) ListBackups(ctx context.Context, _ *connect.Request[pb.ListBackupsRequest]) (*connect.Response[pb.ListBackupsResponse], error) {
	backups, err := h.system.ListBackups(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	size, err := h.system.PhotoStoreSize(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ListBackupsResponse{PhotoStoreBytes: size}
	for _, b := range backups {
		out.Backups = append(out.Backups, backupToPB(b))
	}

	return connect.NewResponse(out), nil
}

// CreateBackup makes a database backup now.
func (h *SystemHandler) CreateBackup(ctx context.Context, _ *connect.Request[pb.CreateBackupRequest]) (*connect.Response[pb.CreateBackupResponse], error) {
	b, err := h.system.CreateBackup(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.CreateBackupResponse{Backup: backupToPB(b)}), nil
}

// ImportCsv adds the games of a CSV file to the catalog.
func (h *SystemHandler) ImportCsv(ctx context.Context, req *connect.Request[pb.ImportCsvRequest]) (*connect.Response[pb.ImportCsvResponse], error) {
	rep, err := h.transfer.Import(ctx, req.Msg.Content)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&pb.ImportCsvResponse{Report: reportToPB(&rep)}), nil
}

// ExportCsv returns the catalog as a CSV file.
func (h *SystemHandler) ExportCsv(ctx context.Context, _ *connect.Request[pb.ExportCsvRequest]) (*connect.Response[pb.ExportCsvResponse], error) {
	name, content, err := h.transfer.Export(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.ExportCsvResponse{
		Filename: name,
		Content:  content,
	}), nil
}

// GetPreferences returns the user's preferences.
func (h *SystemHandler) GetPreferences(ctx context.Context, _ *connect.Request[pb.GetPreferencesRequest]) (*connect.Response[pb.GetPreferencesResponse], error) {
	p, err := h.system.Preferences(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.GetPreferencesResponse{Preferences: &pb.Preferences{Currency: p.Currency}}), nil
}

// UpdatePreferences stores the user's preferences.
func (h *SystemHandler) UpdatePreferences(ctx context.Context, req *connect.Request[pb.UpdatePreferencesRequest]) (*connect.Response[pb.UpdatePreferencesResponse], error) {
	p, err := h.system.UpdatePreferences(ctx, settings.Preferences{Currency: req.Msg.GetPreferences().GetCurrency()})
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.UpdatePreferencesResponse{Preferences: &pb.Preferences{Currency: p.Currency}}), nil
}
