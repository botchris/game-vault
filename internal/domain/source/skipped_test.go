package source_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/source"
)

func TestSkipped(t *testing.T) {
	t.Run("GIVEN a scan that skipped nothing", func(t *testing.T) {
		s := source.Skipped{Store: "Example"}

		t.Run("THEN it has no warning", func(t *testing.T) {
			assert.Empty(t, s.Warnings())
		})
	})

	t.Run("GIVEN a scan that skipped items for two reasons", func(t *testing.T) {
		s := source.Skipped{Store: "Example"}
		s.Add("DLC and add-ons")
		s.Add("subscription games")
		s.Add("DLC and add-ons")

		t.Run("THEN each reason is one warning with its count, in the order first met", func(t *testing.T) {
			assert.Equal(t, []string{
				"2 Example items were not imported: DLC and add-ons",
				"1 Example item was not imported: subscription games",
			}, s.Warnings())
		})
	})
}
