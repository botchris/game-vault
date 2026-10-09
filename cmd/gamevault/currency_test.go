package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// TestCurrencyDigits_webAgrees pins the web UI's currency decimals (web/src/lib/money.ts) to the
// server's (game.CurrencyDigits): prices are stored in minor units, so a currency with different
// decimals on each side would be stored 10, 100 or 1000 times off.
func TestCurrencyDigits_webAgrees(t *testing.T) {
	t.Run("GIVEN the decimals table of the web UI", func(t *testing.T) {
		src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "money.ts"))
		require.NoError(t, err)

		table := regexp.MustCompile(`(?s)const CURRENCY_DIGITS[^{]*\{(.*?)\};`).FindSubmatch(src)
		require.NotNil(t, table, "CURRENCY_DIGITS not found in money.ts")

		web := map[string]int{}
		for _, m := range regexp.MustCompile(`([A-Z]{3}):\s*(\d)`).FindAllSubmatch(table[1], -1) {
			web[string(m[1])], _ = strconv.Atoi(string(m[2]))
		}

		require.NotEmpty(t, web)

		t.Run("THEN every three-letter code has the same decimals on both sides", func(t *testing.T) {
			for a := 'A'; a <= 'Z'; a++ {
				for b := 'A'; b <= 'Z'; b++ {
					for c := 'A'; c <= 'Z'; c++ {
						code := string([]rune{a, b, c})

						want, ok := web[code]
						if !ok {
							want = 2
						}

						assert.Equal(t, want, game.CurrencyDigits(code), "currency %s", code)
					}
				}
			}
		})
	})
}
