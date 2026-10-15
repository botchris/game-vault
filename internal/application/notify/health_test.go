package notify_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/health"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/event"
)

var _ health.Check = (*notify.Service)(nil)

func TestService_Issues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a paused connection and a working one", func(t *testing.T) {
		w := newWorld(t)
		paused := w.connect(t, ctx, "Phone", true, event.KindIssueOpened)
		w.connect(t, ctx, "Desk", true, event.KindIssueOpened)

		pausedAt := w.clock.Now().Add(-time.Hour)
		paused.Pause("the service rejected the token", pausedAt)
		require.NoError(t, w.conns.Save(ctx, paused))

		t.Run("WHEN Health asks THEN only the paused one is an issue, linking to it", func(t *testing.T) {
			issues := w.svc.Issues(ctx)
			require.Len(t, issues, 1)

			got := issues[0]
			assert.Equal(t, health.Issue{
				Key:      "connector:" + string(paused.ID()),
				Level:    health.LevelError,
				Kind:     health.KindConnector,
				Source:   "Phone",
				Reason:   health.ReasonConnectorPaused,
				Params:   map[string]string{"reason": "the service rejected the token"},
				Message:  got.Message,
				Since:    pausedAt,
				Target:   health.TargetNotifications,
				TargetID: string(paused.ID()),
			}, got)
			assert.Equal(t, notify.ConnectionIssueKey(paused.ID()), got.Key)
			assert.Contains(t, got.Message, "the service rejected the token")
		})
	})

	t.Run("GIVEN only working connections WHEN Health asks THEN there is no issue", func(t *testing.T) {
		w := newWorld(t)
		w.connect(t, ctx, "Desk", true, event.KindIssueOpened)

		assert.Empty(t, w.svc.Issues(ctx))
	})
}
