package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// TestSearchTitle_webAgrees pins the web UI's searchTitle (web/src/lib/coverSearch.ts, a port of
// game.SearchTitle that builds the cover picker's search links) to the server's: both are checked
// against the same cases, web/tests/searchTitle.cases.json, which the Node tests run through the
// port. A case added there is checked on both sides.
func TestSearchTitle_webAgrees(t *testing.T) {
	t.Run("GIVEN the shared search-title cases", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join("..", "..", "web", "tests", "searchTitle.cases.json"))
		require.NoError(t, err)

		var cases [][2]string

		require.NoError(t, json.Unmarshal(raw, &cases))
		require.NotEmpty(t, cases)

		t.Run("THEN game.SearchTitle gives the expected title for each", func(t *testing.T) {
			for _, c := range cases {
				assert.Equal(t, c[1], game.SearchTitle(c[0]), "title %q", c[0])
			}
		})
	})
}
