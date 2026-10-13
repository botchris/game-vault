package rpc

import (
	"encoding/json"
	"time"

	"gamevault/internal/application/providers"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
	pb "gamevault/internal/gen/gamevault/v1"
)

func reportToPB(r *source.SyncReport) *pb.SyncReport {
	if r == nil {
		return nil
	}

	return &pb.SyncReport{
		StartedAt:       ts(r.StartedAt),
		FinishedAt:      ts(r.FinishedAt),
		Success:         r.Success(),
		Error:           r.Err,
		Fetched:         int32(r.Fetched),
		CopiesAdded:     int32(r.CopiesAdded),
		CopiesUpdated:   int32(r.CopiesUpdated),
		CopiesUnchanged: int32(r.CopiesUnchanged),
		GamesCreated:    int32(r.GamesCreated),
		Excluded:        int32(r.Excluded),
		Warnings:        r.Warnings,
	}
}

func fieldsToPB(fields schema.Fields) []*pb.SettingField {
	public := fields.Public()
	out := make([]*pb.SettingField, 0, len(public))

	for _, f := range public {
		kind := pb.SettingField_KIND_TEXT

		switch f.Kind {
		case schema.FieldSecret:
			kind = pb.SettingField_KIND_SECRET
		case schema.FieldConsent:
			kind = pb.SettingField_KIND_CONSENT
		}

		var signIn string

		if f.SignIn != nil {
			if b, err := json.Marshal(f.SignIn.WithOpen(f.HelpURL)); err == nil {
				signIn = string(b)
			}
		}

		out = append(out, &pb.SettingField{
			SignIn:   signIn,
			Key:      f.Key,
			LabelKey: f.LabelKey,
			Kind:     kind,
			Required: f.Required,
			HelpKey:  f.HelpKey,
			HelpUrl:  f.HelpURL,
		})
	}

	return out
}

func descriptorToPB(d source.TypeDescriptor) *pb.SourceType {
	return &pb.SourceType{
		Id:             string(d.Type),
		Name:           d.Name,
		DescriptionKey: d.DescriptionKey,
		Fields:         fieldsToPB(d.Fields),
		ManualScans:    d.ManualScans,
	}
}

func providerToPB(v providers.View) *pb.Provider {
	return &pb.Provider{
		Id:             string(v.ID()),
		Kind:           string(v.Kind()),
		Name:           v.Descriptor.Name,
		DescriptionKey: v.Descriptor.DescriptionKey,
		Fields:         fieldsToPB(v.Descriptor.Fields),
		Enabled:        v.Enabled(),
		Priority:       int32(v.Priority()),
		Settings:       v.Descriptor.Fields.Masked(v.Settings()),
	}
}

// sourceToPB maps a source, masking secrets with the type descriptor.
func sourceToPB(v sync.SourceView, d source.TypeDescriptor) *pb.Source {
	return &pb.Source{
		Id:                string(v.ID()),
		Type:              string(v.Type()),
		Name:              v.Name(),
		Enabled:           v.Enabled(),
		SyncIntervalHours: uint32(v.SyncInterval() / time.Hour),
		Settings:          d.Masked(v.Settings()),
		LastSync:          reportToPB(v.LastSync()),
		CopyCount:         int32(v.CopyCount),
		Exclusions:        exclusionsToPB(v.Exclusions()),
		CreatedAt:         ts(v.CreatedAt()),
		UpdatedAt:         ts(v.UpdatedAt()),
	}
}

func exclusionsToPB(list []source.Exclusion) []*pb.Exclusion {
	out := make([]*pb.Exclusion, 0, len(list))
	for _, e := range list {
		out = append(out, &pb.Exclusion{
			ExternalId: e.ExternalID,
			Title:      e.Title,
			At:         ts(e.At),
		})
	}

	return out
}

func configFromPB(in *pb.SourceInput) source.Config {
	if in == nil {
		return source.Config{}
	}

	return source.Config{
		Name:         in.Name,
		Enabled:      in.Enabled,
		SyncInterval: time.Duration(in.SyncIntervalHours) * time.Hour,
		Settings:     schema.Settings(in.Settings),
	}
}
