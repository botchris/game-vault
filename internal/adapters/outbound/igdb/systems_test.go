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

// TestPlatformIDs_noneSharedBetweenSystems checks that no IGDB platform id belongs to two
// systems, which would make the platform filter and the reverse lookup ambiguous.
func TestPlatformIDs_noneSharedBetweenSystems(t *testing.T) {
	owner := map[int]string{}

	for system, ids := range platformIDs {
		for _, id := range ids {
			if other, ok := owner[id]; ok {
				t.Errorf("IGDB platform %d is under both %q and %q", id, other, system)
			}

			owner[id] = system
		}
	}
}
