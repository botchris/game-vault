package source_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/source"
)

func TestSkipped(t *testing.T) {
	t.Run("GIVEN a scan that skipped nothing", func(t *testing.T) {
		s := source.Skipped{Store: "Example"}

		t.Run("WHEN it is logged", func(t *testing.T) {
			var logs bytes.Buffer

			s.Log(slog.New(slog.NewTextHandler(&logs, nil)))

			t.Run("THEN nothing is written, and there is no warning", func(t *testing.T) {
				assert.Empty(t, logs.String())
				assert.Empty(t, s.Warnings())
			})
		})
	})

	t.Run("GIVEN a scan that skipped items for two reasons", func(t *testing.T) {
		s := source.Skipped{Store: "Example"}
		s.Add("DLC and add-ons")
		s.Add("subscription games")
		s.Add("DLC and add-ons")

		t.Run("WHEN it is logged", func(t *testing.T) {
			var logs bytes.Buffer

			s.Log(slog.New(slog.NewTextHandler(&logs, nil)))

			t.Run("THEN each reason is one line with its count, in the order first met", func(t *testing.T) {
				lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
				assert.Len(t, lines, 2)
				assert.Contains(t, lines[0], `store=Example reason="DLC and add-ons" count=2`)
				assert.Contains(t, lines[1], `store=Example reason="subscription games" count=1`)
			})
		})

		t.Run("WHEN it is logged without a logger", func(t *testing.T) {
			t.Run("THEN nothing happens", func(t *testing.T) {
				assert.NotPanics(t, func() { s.Log(nil) })
			})
		})

		t.Run("WHEN its warnings are asked, for a skip the user must hear about", func(t *testing.T) {
			t.Run("THEN each reason is one warning with its count", func(t *testing.T) {
				assert.Equal(t, []string{
					"2 Example items were not imported: DLC and add-ons",
					"1 Example item was not imported: subscription games",
				}, s.Warnings())
			})
		})
	})
}
