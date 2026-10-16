package game

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckProviderCoverage(t *testing.T) {
	full := map[string][]int{}
	for _, s := range Systems() {
		full[s.Name] = []int{1}
	}

	t.Run("GIVEN a table mapping every system", func(t *testing.T) {
		assert.NoError(t, CheckProviderCoverage(full, nil))
	})

	t.Run("GIVEN a table missing one system", func(t *testing.T) {
		missing := maps.Clone(full)
		delete(missing, "SNES")

		t.Run("THEN it fails naming it", func(t *testing.T) {
			assert.ErrorContains(t, CheckProviderCoverage(missing, nil), `"SNES"`)
		})

		t.Run("OR it passes when the system is excluded", func(t *testing.T) {
			assert.NoError(t, CheckProviderCoverage(missing, []string{"SNES"}))
		})
	})

	t.Run("GIVEN a key that is not a catalog name, an empty entry, or a system mapped and excluded", func(t *testing.T) {
		bad := maps.Clone(full)
		bad["Super Nintendo"] = []int{4}
		bad["NES"] = nil

		err := CheckProviderCoverage(bad, []string{"PS4"})
		assert.ErrorContains(t, err, `"Super Nintendo"`)
		assert.ErrorContains(t, err, `"NES"`)
		assert.ErrorContains(t, err, `"PS4"`)
	})
}
