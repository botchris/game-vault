package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAddOnTitle(t *testing.T) {
	for title, want := range map[string]bool{
		"GRIP: Combat Racing Artifex DLC":  true,
		"Celeste - Original Soundtrack":    true,
		"Borderlands 2 Season Pass":        true,
		"Dead Space 3":                     false,
		"Pack-Man":                         false,
		"Expansion: The Card Game Of Doom": true, // rare false positive: only used when no cover was found
	} {
		assert.Equal(t, want, IsAddOnTitle(title), title)
	}
}

func TestStripAddOnMarker(t *testing.T) {
	for title, want := range map[string]string{
		"GRIP: Combat Racing Artifex DLC": "GRIP: Combat Racing Artifex ",
		"Celeste - Original Soundtrack":   "Celeste - Original ",
		"Dead Space 3":                    "Dead Space 3",
	} {
		assert.Equal(t, want, StripAddOnMarker(title), title)
	}
}
