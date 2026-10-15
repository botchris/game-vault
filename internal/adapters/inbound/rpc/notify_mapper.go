package rpc

import (
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
)

func connectorTypeToPB(d connection.TypeDescriptor) *pb.ConnectorType {
	return &pb.ConnectorType{
		Id:             string(d.Type),
		Name:           d.Name,
		DescriptionKey: d.DescriptionKey,
		Fields:         fieldsToPB(d.Fields),
	}
}

func eventKindsToPB() []*pb.EventKind {
	kinds := event.Kinds()
	out := make([]*pb.EventKind, 0, len(kinds))

	for _, k := range kinds {
		out = append(out, &pb.EventKind{
			Kind:  string(k),
			Group: string(event.GroupOf(k)),
		})
	}

	return out
}

// connectionToPB maps a connection, masking secrets with its type's descriptor. The zero
// descriptor of a type no longer compiled in has no fields, so Masked drops every setting: nothing
// secret can leak.
func connectionToPB(c *connection.Connection, d connection.TypeDescriptor) *pb.Connection {
	events := make([]string, 0, len(c.Events()))
	for _, k := range c.Events() {
		events = append(events, string(k))
	}

	return &pb.Connection{
		Id:           string(c.ID()),
		Type:         string(c.Type()),
		Name:         c.Name(),
		Enabled:      c.Enabled(),
		Events:       events,
		Language:     c.Language(),
		Settings:     d.Masked(c.Settings()),
		PausedReason: c.PausedWhy(),
		PausedAt:     ts(c.PausedAt()),
	}
}

func configFromConnectionPB(in *pb.ConnectionInput) connection.Config {
	if in == nil {
		return connection.Config{}
	}

	events := make([]event.Kind, 0, len(in.Events))
	for _, k := range in.Events {
		events = append(events, event.Kind(k))
	}

	return connection.Config{
		Name:     in.Name,
		Enabled:  in.Enabled,
		Events:   events,
		Language: in.Language,
		Settings: schema.Settings(in.Settings),
	}
}

func deliveryToPB(d notify.Delivery, summary string) *pb.Delivery {
	return &pb.Delivery{
		Id:        d.ID,
		Event:     string(d.Event.Kind),
		State:     string(d.State),
		Attempts:  int32(d.Attempts),
		LastError: d.LastError,
		CreatedAt: ts(d.CreatedAt),
		UpdatedAt: ts(d.UpdatedAt),
		Summary:   summary,
	}
}
