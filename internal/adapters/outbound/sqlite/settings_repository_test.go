package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/settings"
)

func TestSettingsRepository_preferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	repo := NewSettingsRepository(openTest(t))

	t.Run("GIVEN no saved preferences", func(t *testing.T) {
		p, err := repo.Preferences(ctx)

		t.Run("THEN they are empty", func(t *testing.T) {
			require.NoError(t, err)
			assert.Empty(t, p.Currency)
		})
	})

	t.Run("WHEN a default currency is saved", func(t *testing.T) {
		require.NoError(t, repo.SavePreferences(ctx, settings.Preferences{Currency: "GBP"}))

		t.Run("THEN it is read back", func(t *testing.T) {
			p, err := repo.Preferences(ctx)
			require.NoError(t, err)
			assert.Equal(t, "GBP", p.Currency)
		})
	})
}
