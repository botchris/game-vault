package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseStoreDate(t *testing.T) {
	for in, want := range map[string]Date{
		"2023-05-06T07:08:09.123456":  "2023-05-06", // Humble's order dates
		"2023-05-06T07:08:09":         "2023-05-06",
		"2023-05-06T07:08:09Z":        "2023-05-06", // RFC 3339 (itch.io, Fanatical)
		"2023-05-06T23:30:00-05:00":   "2023-05-06", // the day where it was written
		"2023-05-06T07:08:09.000Z":    "2023-05-06", // Fanatical's milliseconds
		"2023-05-06":                  "2023-05-06",
		"2023-05-06 07:08:09":         "2023-05-06", // itch.io's older endpoints
		"May 6, 2023":                 "2023-05-06", // Humble's release dates in text
		"May 6 2023":                  "2023-05-06",
		"Sep 6, 2023":                 "2023-09-06",
		"Sep 6 2023":                  "2023-09-06",
		"September 6, 2023":           "2023-09-06",
		"  2023-05-06  ":              "2023-05-06",
		"":                            "",
		"soon":                        "",
		"06/05/2023":                  "",
		"2023-13-45":                  "",
		"2023-05-06T07:08:09+0200 xx": "",
	} {
		assert.Equal(t, want, ParseStoreDate(in), in)
	}
}
