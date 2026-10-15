package igdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// TestSystems_coverTheCatalog checks the table of IGDB's platforms against the catalog: a
// system missing from it, or a key spelled differently, silently drops the platform filter.
func TestSystems_coverTheCatalog(t *testing.T) {
	require.NoError(t, game.CheckProviderCoverage(platformIDs, unmappedSystems))
}
