package event_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/event"
)

// TestKinds_catalog pins the stored kind names and their groups: subscriptions and the outbox keep
// these strings, so a rename would silently drop what users chose.
func TestKinds_catalog(t *testing.T) {
	want := map[event.Kind]event.Group{
		"collection.added":    event.GroupActivity,
		"release.announced":   event.GroupActivity,
		"release.out":         event.GroupActivity,
		"deal.historical_low": event.GroupActivity,
		"issue.opened":        event.GroupAttention,
		"issue.resolved":      event.GroupAttention,
		"keys.expiring":       event.GroupAttention,
		"auth.failed_logins":  event.GroupSecurity,
	}

	got := map[event.Kind]event.Group{}
	for _, k := range event.Kinds() {
		got[k] = event.GroupOf(k)
		assert.True(t, event.Known(k), k)
	}

	assert.Equal(t, want, got)
	assert.False(t, event.Known("collection.removed"))
}
