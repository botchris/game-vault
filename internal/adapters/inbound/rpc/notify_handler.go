package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// deliveriesShown is how many deliveries a connection's list holds.
const deliveriesShown = 20

var _ gamevaultv1connect.NotificationServiceHandler = (*NotificationHandler)(nil)

// NotificationHandler implements gamevaultv1connect.NotificationServiceHandler on top of the notify
// use cases.
type NotificationHandler struct {
	notify *notify.Service
}

// NewNotificationHandler returns the NotificationService handler backed by the notify service.
func NewNotificationHandler(s *notify.Service) *NotificationHandler {
	return &NotificationHandler{notify: s}
}

func (h *NotificationHandler) toPB(c *connection.Connection) *pb.Connection {
	d, _ := h.notify.Descriptor(c.Type()) // an unknown type masks everything, which is safe

	return connectionToPB(c, d)
}

// ListConnectorTypes returns the kinds of connection, the events and the languages.
func (h *NotificationHandler) ListConnectorTypes(context.Context, *connect.Request[pb.ListConnectorTypesRequest]) (*connect.Response[pb.ListConnectorTypesResponse], error) {
	out := &pb.ListConnectorTypesResponse{
		Events:            eventKindsToPB(),
		Languages:         connection.Languages(),
		SecretPlaceholder: schema.SecretPlaceholder,
	}

	for _, d := range h.notify.Types() {
		out.Types = append(out.Types, connectorTypeToPB(d))
	}

	return connect.NewResponse(out), nil
}

// ListConnections returns the configured connections, secrets masked.
func (h *NotificationHandler) ListConnections(ctx context.Context, _ *connect.Request[pb.ListConnectionsRequest]) (*connect.Response[pb.ListConnectionsResponse], error) {
	list, err := h.notify.List(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ListConnectionsResponse{}
	for _, c := range list {
		out.Connections = append(out.Connections, h.toPB(c))
	}

	return connect.NewResponse(out), nil
}

// SaveConnection creates or reconfigures a connection.
func (h *NotificationHandler) SaveConnection(ctx context.Context, req *connect.Request[pb.SaveConnectionRequest]) (*connect.Response[pb.SaveConnectionResponse], error) {
	in := req.Msg.Connection
	if in == nil {
		in = &pb.ConnectionInput{}
	}

	c, err := h.notify.Save(ctx, connection.ID(req.Msg.Id), connection.Type(in.Type), configFromConnectionPB(in))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.SaveConnectionResponse{Connection: h.toPB(c)}), nil
}

// DeleteConnection removes a connection and its deliveries.
func (h *NotificationHandler) DeleteConnection(ctx context.Context, req *connect.Request[pb.DeleteConnectionRequest]) (*connect.Response[pb.DeleteConnectionResponse], error) {
	if err := h.notify.Delete(ctx, connection.ID(req.Msg.Id)); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.DeleteConnectionResponse{}), nil
}

// TestConnection sends a sample message with the settings given.
func (h *NotificationHandler) TestConnection(ctx context.Context, req *connect.Request[pb.TestConnectionRequest]) (*connect.Response[pb.TestConnectionResponse], error) {
	in := req.Msg.Connection
	if in == nil {
		in = &pb.ConnectionInput{}
	}

	found, err := h.notify.Test(ctx, connection.ID(req.Msg.Id), connection.Type(in.Type), configFromConnectionPB(in))
	if err != nil {
		// A failed test is a valid result, not an RPC failure; what was found still helps the form.
		return connect.NewResponse(&pb.TestConnectionResponse{
			Success: false,
			Message: err.Error(),
			Found:   found,
		}), nil
	}

	return connect.NewResponse(&pb.TestConnectionResponse{
		Success: true,
		Found:   found,
	}), nil
}

// ListDeliveries returns a connection's latest deliveries, newest first.
func (h *NotificationHandler) ListDeliveries(ctx context.Context, req *connect.Request[pb.ListDeliveriesRequest]) (*connect.Response[pb.ListDeliveriesResponse], error) {
	id := connection.ID(req.Msg.ConnectionId)

	c, err := h.notify.Get(ctx, id)
	if err != nil {
		return nil, toConnectError(err)
	}

	list, err := h.notify.Deliveries(ctx, id, deliveriesShown)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ListDeliveriesResponse{}
	for _, d := range list {
		out.Deliveries = append(out.Deliveries, deliveryToPB(d, h.notify.DeliveryTitle(ctx, d, c.Language())))
	}

	return connect.NewResponse(out), nil
}

// GetNotificationSettings returns the address messages link to.
func (h *NotificationHandler) GetNotificationSettings(ctx context.Context, _ *connect.Request[pb.GetNotificationSettingsRequest]) (*connect.Response[pb.GetNotificationSettingsResponse], error) {
	address, err := h.notify.Address(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.GetNotificationSettingsResponse{Address: address}), nil
}

// SaveNotificationSettings stores the address messages link to.
func (h *NotificationHandler) SaveNotificationSettings(ctx context.Context, req *connect.Request[pb.SaveNotificationSettingsRequest]) (*connect.Response[pb.SaveNotificationSettingsResponse], error) {
	if err := h.notify.SaveAddress(ctx, req.Msg.Address); err != nil {
		return nil, toConnectError(err)
	}

	address, err := h.notify.Address(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.SaveNotificationSettingsResponse{Address: address}), nil
}
