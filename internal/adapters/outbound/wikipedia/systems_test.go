package wikipedia

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/game"
)

// TestSystems_mapEveryConsole checks the table of Wikidata's platforms against the domain's systems: a
// console missing from it, or a key spelled differently, silently drops the platform filter.
func TestSystems_mapEveryConsole(t *testing.T) {
	for _, c := range append(game.Consoles(), game.SystemPC) {
		assert.NotEmpty(t, platformItems[c], "system %q has no Wikidata platform", c)
	}

	for s := range platformItems {
		assert.True(t, game.IsKnownPlatform(s), "%q is not a system Game Vault knows", s)
		assert.Equal(t, s, game.SystemOf(s), "%q is not a system Game Vault knows", s)
	}
}
