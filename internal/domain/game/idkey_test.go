package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTitleIDKey_neverChanges pins the ids derived from titles. Battle.net and EA copies without a
// store id were imported under these ids: a different output would import them again as new copies.
// Never edit an expected value; MatchKey may change, this may not.
func TestTitleIDKey_neverChanges(t *testing.T) {
	for title, want := range map[string]string{
		"Example Quest":                          "example quest",
		"Celeste™ (Steam Key)":                   "celeste",
		"Hades GOG Key":                          "hades",
		"Example West: Game of the Year Edition": "example west",
		"Pokémon Example & Friends":              "pokemon example and friends",
		"Example Racer [PS4] - Deluxe Edition":   "example racer",
		"Example Arena: Director's Cut":          "example arena",
		"Example Saga Ubisoft Connect CD Key":    "example saga",
		"Example Tactics - Xbox Series X|S Key":  "example tactics",
		"Example Blitz 2":                        "example blitz 2",
		"Øresund Example":                        "resund example", // Ø is no ASCII letter: dropped
		"Epic Mickey":                            "epic mickey",
		"  ":                                     "",
		// Known collision: a title with no ASCII letter or digit gives an empty key, so two such
		// games without a store id share one id ("ea:", "battlenet:") and import as one copy. It
		// stays: changing it would re-key the copies already imported.
		"黑神话": "",
	} {
		assert.Equal(t, want, TitleIDKey(title), title)
	}
}
