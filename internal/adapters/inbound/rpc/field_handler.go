package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/fields"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// FieldHandler implements gamevaultv1connect.FieldServiceHandler on top of the custom fields
// use cases.
type FieldHandler struct {
	fields *fields.Service
}

var _ gamevaultv1connect.FieldServiceHandler = (*FieldHandler)(nil)

// NewFieldHandler returns the FieldService handler.
func NewFieldHandler(f *fields.Service) *FieldHandler {
	return &FieldHandler{fields: f}
}

// ListFields returns the custom fields in order.
func (h *FieldHandler) ListFields(ctx context.Context, _ *connect.Request[pb.ListFieldsRequest]) (*connect.Response[pb.ListFieldsResponse], error) {
	defs, err := h.fields.List(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := make([]*pb.FieldDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, definitionToPB(d))
	}

	return connect.NewResponse(&pb.ListFieldsResponse{Fields: out}), nil
}

// CreateField adds a custom field.
func (h *FieldHandler) CreateField(ctx context.Context, req *connect.Request[pb.CreateFieldRequest]) (*connect.Response[pb.CreateFieldResponse], error) {
	d, err := h.fields.Create(ctx, definitionFromPB(req.Msg.Field))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.CreateFieldResponse{Field: definitionToPB(d)}), nil
}

// UpdateField changes a custom field's editable attributes.
func (h *FieldHandler) UpdateField(ctx context.Context, req *connect.Request[pb.UpdateFieldRequest]) (*connect.Response[pb.UpdateFieldResponse], error) {
	d, err := h.fields.Update(ctx, definitionFromPB(req.Msg.Field))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.UpdateFieldResponse{Field: definitionToPB(d)}), nil
}

// MoveField puts a custom field at a position in the order.
func (h *FieldHandler) MoveField(ctx context.Context, req *connect.Request[pb.MoveFieldRequest]) (*connect.Response[pb.MoveFieldResponse], error) {
	if err := h.fields.Move(ctx, req.Msg.Id, int(req.Msg.Index)); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.MoveFieldResponse{}), nil
}

// FieldUsage counts the games and copies that hold a value of a custom field.
func (h *FieldHandler) FieldUsage(ctx context.Context, req *connect.Request[pb.FieldUsageRequest]) (*connect.Response[pb.FieldUsageResponse], error) {
	games, copies, err := h.fields.Usage(ctx, req.Msg.Id)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.FieldUsageResponse{
		Games:  int32(games),
		Copies: int32(copies),
	}), nil
}

// DeleteField removes a custom field and its values from every game and copy.
func (h *FieldHandler) DeleteField(ctx context.Context, req *connect.Request[pb.DeleteFieldRequest]) (*connect.Response[pb.DeleteFieldResponse], error) {
	games, copies, err := h.fields.Delete(ctx, req.Msg.Id)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.DeleteFieldResponse{
		Games:  int32(games),
		Copies: int32(copies),
	}), nil
}

// RemoveChoice removes a list value, clearing it from games or merging it into another value.
func (h *FieldHandler) RemoveChoice(ctx context.Context, req *connect.Request[pb.RemoveChoiceRequest]) (*connect.Response[pb.RemoveChoiceResponse], error) {
	if err := h.fields.RemoveChoice(ctx, req.Msg.FieldId, req.Msg.ChoiceId, req.Msg.MergeInto); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.RemoveChoiceResponse{}), nil
}

// AddChoice adds a list value, or returns the existing one with that name.
func (h *FieldHandler) AddChoice(ctx context.Context, req *connect.Request[pb.AddChoiceRequest]) (*connect.Response[pb.AddChoiceResponse], error) {
	c, err := h.fields.AddChoice(ctx, req.Msg.FieldId, req.Msg.Name)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.AddChoiceResponse{Choice: choiceToPB(c)}), nil
}
