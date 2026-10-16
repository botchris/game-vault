package notify

import (
	"context"

	"gamevault/internal/application/health"
)

var _ health.Check = (*Service)(nil)

// Issues implements health.Check: each paused connection is an error until the user fixes it. It
// reads the stored connections only, never the network.
func (s *Service) Issues(ctx context.Context) []health.Issue {
	conns, err := s.conns.List(ctx)
	if err != nil {
		s.log.Warn("health: reading the connections", "error", err)

		return nil
	}

	var out []health.Issue

	for _, c := range conns {
		if !c.Paused() {
			continue
		}

		out = append(out, health.Issue{
			Key:      ConnectionIssueKey(c.ID()),
			Level:    health.LevelError,
			Kind:     health.KindConnector,
			Source:   c.Name(),
			Reason:   health.ReasonConnectorPaused,
			Params:   map[string]string{"reason": c.PausedWhy()},
			Message:  "Notifications are paused: " + c.PausedWhy() + ". Fix the connection's settings, then test or save it.",
			Since:    c.PausedAt(),
			Target:   health.TargetNotifications,
			TargetID: string(c.ID()),
		})
	}

	return out
}
