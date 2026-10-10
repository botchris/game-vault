package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// TestSystems_webAgrees pins the web UI's system tables (SYSTEMS, the PC platforms and the store
// systems in web/src/lib/editions.ts) to the server's (game.SystemOf): the library groups and
// filters by the system the web computes, so a platform that each side maps differently would be
// listed under a system the server never stored.
func TestSystems_webAgrees(t *testing.T) {
	t.Run("GIVEN the system tables of the web UI", func(t *testing.T) {
		src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "editions.ts"))
		require.NoError(t, err)

		systems := webStrings(t, src, `export const SYSTEMS = \[(.*?)\];`)
		pc := webStrings(t, src, `const PC_PLATFORMS = \[(.*?)\];`)

		stores := map[string]string{}

		block := regexp.MustCompile(`(?s)const STORE_SYSTEMS[^{]*\{(.*?)\};`).FindSubmatch(src)
		require.NotNil(t, block, "STORE_SYSTEMS not found in editions.ts")

		for _, m := range regexp.MustCompile(`'([^']+)':\s*'([^']+)'`).FindAllSubmatch(block[1], -1) {
			stores[string(m[1])] = string(m[2])
		}

		require.NotEmpty(t, stores)

		// webSystemOf is the web's systemOf, written out from its tables.
		webSystemOf := func(platform string) string {
			k := strings.ToLower(strings.TrimSpace(platform))
			if k == "" {
				return "Other"
			}

			for _, p := range pc {
				if strings.ToLower(p) == k {
					return "PC"
				}
			}

			if s, ok := stores[k]; ok {
				return s
			}

			for _, s := range systems {
				if strings.ToLower(s) == k {
					return s
				}
			}

			return strings.TrimSpace(platform)
		}

		t.Run("THEN every platform the web knows maps to the same system on both sides", func(t *testing.T) {
			platforms := append(append([]string{}, systems...), pc...)

			for k := range stores {
				platforms = append(platforms, k)
			}

			// The platforms the web offers in its forms (web/src/lib/model.ts) must agree too.
			model, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "model.ts"))
			require.NoError(t, err)

			platforms = append(platforms, webStrings(t, model, `export const STORE_PLATFORMS = \[(.*?)\];`)...)
			platforms = append(platforms, webStrings(t, model, `export const PHYSICAL_PLATFORMS = \[(.*?)\];`)...)
			platforms = append(platforms, "", " Amiga ", "xbox 360", "STEAM", "Dreamcast")

			for _, p := range platforms {
				assert.Equal(t, game.SystemOf(p), webSystemOf(p), "platform %q", p)
			}
		})

		t.Run("THEN every system the server derives from a console name is offered by the web", func(t *testing.T) {
			for _, s := range systems {
				assert.Equal(t, s, game.SystemOf(s), "system %q", s)
			}

			assert.Contains(t, systems, game.SystemPC)
		})
	})
}

// webStrings returns the quoted strings of the first capture group of pattern in src.
func webStrings(t *testing.T, src []byte, pattern string) []string {
	t.Helper()

	block := regexp.MustCompile(`(?s)` + pattern).FindSubmatch(src)
	require.NotNil(t, block, "%s not found", pattern)

	matches := regexp.MustCompile(`'([^']*)'`).FindAllSubmatch(block[1], -1)
	out := make([]string, 0, len(matches))

	for _, m := range matches {
		out = append(out, string(m[1]))
	}

	require.NotEmpty(t, out, pattern)

	return out
}
