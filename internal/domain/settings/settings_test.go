package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevels_areReadOnly(t *testing.T) {
	t.Run("GIVEN a caller that changes the levels it was given", func(t *testing.T) {
		levels := LogLevels()
		levels[0] = "trace"

		t.Run("THEN the accepted levels are unchanged", func(t *testing.T) {
			assert.Equal(t, []string{"debug", "info", "warn", "error"}, LogLevels())

			_, err := Logging{
				Level:         "trace",
				MaxFileSizeMB: 10,
				MaxFiles:      5,
			}.Validate()
			require.Error(t, err)
		})
	})
}
