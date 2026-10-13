package health_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/health"
)

// TestHealthReasons_webAgrees pins the web UI's health reasons (Reason in web/src/lib/health.ts)
// and their translations (health.reasons in every locale) to the server's (health.Reasons): a
// reason the web does not list shows the server's English message, and one without a translation
// shows its key.
func TestHealthReasons_webAgrees(t *testing.T) {
	server := make([]string, 0, len(health.Reasons()))
	for _, r := range health.Reasons() {
		server = append(server, string(r))
	}

	slices.Sort(server)

	t.Run("GIVEN the reasons of the web UI", func(t *testing.T) {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "lib", "health.ts"))
		require.NoError(t, err)

		block := regexp.MustCompile(`(?s)export const Reason = \{(.*?)\} as const;`).FindSubmatch(src)
		require.NotNil(t, block, "Reason not found in health.ts")

		web := make([]string, 0, len(server))
		for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(block[1], -1) {
			web = append(web, string(m[1]))
		}

		slices.Sort(web)

		t.Run("THEN they are the server's", func(t *testing.T) {
			assert.Equal(t, server, web)
		})
	})

	locales, err := filepath.Glob(filepath.Join("..", "..", "..", "web", "src", "i18n", "locales", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, locales)

	for _, path := range locales {
		t.Run("GIVEN the translations in "+filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)

			var locale struct {
				Health struct {
					Reasons map[string]string `json:"reasons"`
				} `json:"health"`
			}
			require.NoError(t, json.Unmarshal(src, &locale))

			t.Run("THEN every reason of the server has one, and no other", func(t *testing.T) {
				assert.Equal(t, server, slices.Sorted(maps.Keys(locale.Health.Reasons)))
			})
		})
	}
}
