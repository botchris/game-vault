package thegamesdb

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/game"
)

// TestSystems_mapEveryConsole checks the table of TheGamesDB's platforms against the domain's systems: a
// console missing from it, or a key spelled differently, silently drops the platform filter.
func TestSystems_mapEveryConsole(t *testing.T) {
	// Until Task 5 extends the table to the whole catalog, only the systems that have a game constant are checked.
	for _, c := range []string{
		game.SystemPC, game.SystemPS5, game.SystemPS4, game.SystemPS3, game.SystemPS2, game.SystemPS1, game.SystemPSP,
		game.SystemPSVita, game.SystemXboxSeries, game.SystemXboxOne, game.SystemXbox360, game.SystemXbox, game.SystemSwitch,
		game.SystemWiiU, game.SystemWii, game.SystemGameCube, game.SystemN64, game.System3DS, game.SystemDS, game.SystemGameBoy,
	} {
		assert.NotEmpty(t, platformNames[c], "system %q has no TheGamesDB platform", c)
	}

	for s := range platformNames {
		assert.True(t, game.IsKnownPlatform(s), "%q is not a system Game Vault knows", s)
		assert.Equal(t, s, game.SystemOf(s), "%q is not a system Game Vault knows", s)
	}
}
