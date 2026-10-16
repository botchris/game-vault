package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/health"
)

func TestTexts_sameKeysInEveryLanguage(t *testing.T) {
	t.Run("GIVEN the English and Spanish texts", func(t *testing.T) {
		en, es := texts["en"], texts["es"]

		t.Run("THEN every key exists in both, never empty", func(t *testing.T) {
			for k, v := range en {
				assert.NotEmpty(t, v, "en %s", k)
				assert.NotEmpty(t, es[k], "es is missing %s", k)
			}

			for k, v := range es {
				assert.NotEmpty(t, v, "es %s", k)
				assert.NotEmpty(t, en[k], "en is missing %s", k)
			}
		})

		t.Run("AND every Health reason has a text, the connector's pause included", func(t *testing.T) {
			for _, r := range health.Reasons() {
				assert.Contains(t, en, "reason."+string(r))
			}

			assert.Contains(t, en, "reason.connector_paused")
		})
	})
}

func TestText_fallsBack(t *testing.T) {
	assert.Equal(t, "in 3 days", text("en", "days.other", 3))
	assert.Equal(t, "en 3 días", text("es", "days.other", 3))
	assert.Equal(t, "in 3 days", text("fr", "days.other", 3), "an unknown language is English")
	assert.Equal(t, "no.such.key", text("es", "no.such.key"), "an unknown key shows itself")
}

func TestFillParams(t *testing.T) {
	assert.Equal(t, "Only 2 GB free (5%)", fillParams("Only {{free}} free ({{percent}}%)", map[string]string{
		"free":    "2 GB",
		"percent": "5",
	}))
	assert.Equal(t, "No backup in  days", fillParams("No backup in {{days}} days", nil), "a missing param leaves nothing behind")
}
