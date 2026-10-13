package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripYearSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"Example Quest Remastered (2017)": "Example Quest Remastered",
		"Example Quest (2016)  ":          "Example Quest",
		"Example Quest(1999)":             "Example Quest",
		"Example Quest (2016) Deluxe":     "Example Quest (2016) Deluxe", // only a trailing year
		"Example Quest (16)":              "Example Quest (16)",          // four digits only
		"Example Quest (Remastered)":      "Example Quest (Remastered)",
		"Example 2049":                    "Example 2049",
		" Example Quest ":                 " Example Quest ", // nothing else is trimmed
		"(2016)":                          "",
	} {
		assert.Equal(t, want, StripYearSuffix(in), in)
	}
}
