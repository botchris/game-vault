package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/health"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.HealthServiceHandler = (*HealthHandler)(nil)

// HealthHandler serves what needs the user's attention.
type HealthHandler struct{ health *health.Service }

// NewHealthHandler builds the handler.
func NewHealthHandler(h *health.Service) *HealthHandler { return &HealthHandler{health: h} }

// GetHealth returns the current issues of every check.
func (h *HealthHandler) GetHealth(ctx context.Context, _ *connect.Request[pb.GetHealthRequest]) (*connect.Response[pb.GetHealthResponse], error) {
	out := &pb.GetHealthResponse{}

	for _, i := range h.health.Issues(ctx) {
		out.Issues = append(out.Issues, &pb.HealthIssue{
			Key:      i.Key,
			Level:    string(i.Level),
			Kind:     string(i.Kind),
			Source:   i.Source,
			Message:  i.Message,
			Since:    optionalTS(i.Since),
			Target:   string(i.Target),
			TargetId: i.TargetID,
			Reason:   string(i.Reason),
			Params:   i.Params,
		})
	}

	return connect.NewResponse(out), nil
}
