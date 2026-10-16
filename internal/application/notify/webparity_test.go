package notify_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/notify"
)

// TestExpiringDays_webAgrees keeps the server's window of expiring keys equal to the library's: a
// message saying a key expires within 30 days must name the keys the "Expiring" filter shows.
func TestExpiringDays_webAgrees(t *testing.T) {
	t.Run("GIVEN the web's deadlines module", func(t *testing.T) {
		src, err := os.ReadFile("../../../web/src/lib/deadlines.ts")
		require.NoError(t, err)

		t.Run("WHEN its EXPIRING_DAYS is read THEN it equals ExpiringDays", func(t *testing.T) {
			m := regexp.MustCompile(`export const EXPIRING_DAYS = (\d+);`).FindSubmatch(src)
			require.NotNil(t, m, "EXPIRING_DAYS is no longer declared as expected in deadlines.ts")

			days, err := strconv.Atoi(string(m[1]))
			require.NoError(t, err)
			assert.Equal(t, notify.ExpiringDays, days)
		})
	})
}
