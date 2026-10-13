package source

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

func TestStateFields(t *testing.T) {
	d := TypeDescriptor{
		Type: "x",
		Name: "X",
		Fields: []schema.Field{
			{
				Key:  "code",
				Kind: schema.FieldSecret,
			},
			{
				Key:  "session",
				Kind: schema.FieldState,
			},
		},
	}
	now := time.Now()

	src, err := New(d, Config{Settings: schema.Settings{"code": "abc", "session": "from-client"}}, now)
	require.NoError(t, err)
	assert.NotContains(t, src.Settings(), "session", "clients must not set state fields")

	src.ReplaceSettings(d, schema.Settings{"session": "s1"}, now)
	assert.Empty(t, d.Masked(src.Settings()), "state must never be shown")

	// Reconfiguring keeps the state; the provider rotates it later.
	require.NoError(t, src.Reconfigure(d, Config{Settings: schema.Settings{"session": "evil"}}, now))
	assert.Equal(t, "s1", src.Settings()["session"], "reconfigure must not change the state")

	assert.False(t, src.UpdateState(d, schema.Settings{"session": "s1"}, now), "unchanged state reported as changed")
	assert.True(t, src.UpdateState(d, schema.Settings{"session": "s2", "code": "ignored"}, now))
	assert.Equal(t, "s2", src.Settings()["session"])
	assert.Empty(t, src.Settings()["code"], "only state fields are updated")
}
