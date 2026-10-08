package media

import (
	"slices"
	"testing"
	"time"

	"gamevault/internal/domain/game"
)

func TestIsAddOn(t *testing.T) {
	for title, want := range map[string]bool{
		"GRIP: Combat Racing Artifex DLC":  true,
		"Celeste - Original Soundtrack":    true,
		"Borderlands 2 Season Pass":        true,
		"Dead Space 3":                     false,
		"Pack-Man":                         false,
		"Expansion: The Card Game Of Doom": true, // rare false positive: only used when no cover was found
	} {
		if got := IsAddOn(title); got != want {
			t.Errorf("IsAddOn(%q) = %v", title, got)
		}
	}
}

func TestBaseGameOf(t *testing.T) {
	now := time.Now()
	mk := func(title string) *game.Game {
		g, err := game.New(title, now)
		if err != nil {
			t.Fatal(err)
		}

		return g
	}
	dlc := mk("GRIP: Combat Racing Artifex DLC")
	grip, other := mk("GRIP: Combat Racing"), mk("GRIP")

	catalog := []*game.Game{other, dlc, grip, mk("Gripper"), mk("GRIP: Combat Racing Artifex DLC Bundle")}
	if got := baseGameOf(dlc, catalog); got != grip {
		t.Fatalf("base game = %v, want the longest title prefix", got.Title())
	}

	if got := baseGameOf(mk("Dead Space 3 Awakened DLC"), []*game.Game{mk("Dead Space")}); got == nil {
		t.Fatal("a shorter prefix should still match")
	}

	if got := baseGameOf(mk("Gripper DLC"), []*game.Game{grip}); got != nil {
		t.Fatalf("matched a non word prefix: %v", got.Title())
	}
}

func TestBaseQueries(t *testing.T) {
	got := baseQueries("GRIP: Combat Racing Artifex DLC")
	for _, want := range []string{"GRIP", "GRIP: Combat Racing Artifex", "GRIP: Combat Racing"} {
		if !slices.Contains(got, want) {
			t.Errorf("baseQueries = %q, missing %q", got, want)
		}
	}
}
