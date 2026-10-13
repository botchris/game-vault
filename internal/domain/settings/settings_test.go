package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
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

func TestLogging_Validate(t *testing.T) {
	logging := func(level string, sizeMB, files int) Logging {
		return Logging{
			Level:         level,
			MaxFileSizeMB: sizeMB,
			MaxFiles:      files,
		}
	}

	cases := []struct {
		name string
		in   Logging
		ok   bool
	}{
		{"the defaults", DefaultLogging(), true},
		{"the smallest limits", logging("debug", 1, 1), true},
		{"the largest limits", logging("error", 100, 50), true},
		{"an unknown level", logging("trace", 10, 5), false},
		{"files of 0 MB", logging("info", 0, 5), false},
		{"files over 100 MB", logging("info", 101, 5), false},
		{"no files", logging("info", 10, 0), false},
		{"more than 50 files", logging("info", 10, 51), false},
	}
	for _, c := range cases {
		_, err := c.in.Validate()
		if c.ok {
			assert.NoError(t, err, c.name)
			continue
		}

		var v *errkind.ValidationError
		assert.ErrorAs(t, err, &v, c.name)
	}

	t.Run("GIVEN a level with spaces and capitals THEN it is normalized", func(t *testing.T) {
		got, err := logging(" WARN ", 10, 5).Validate()
		require.NoError(t, err)
		assert.Equal(t, "warn", got.Level)
	})
}

func TestPreferences_Validate(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"EUR", "EUR", true},
		{" usd ", "USD", true},
		{"EURO", "", false},
		{"E1R", "", false},
	}
	for _, c := range cases {
		got, err := Preferences{Currency: c.in}.Validate()
		if !c.ok {
			var v *errkind.ValidationError
			assert.ErrorAs(t, err, &v, "%q", c.in)

			continue
		}

		require.NoError(t, err, "%q", c.in)
		assert.Equal(t, c.want, got.Currency, "%q", c.in)
	}
}
