package source

import (
	"testing"
	"time"
)

func TestExclusions(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	src := Rehydrate("s1", "psn", "PlayStation", true, 0, Settings{}, nil, nil, now, now)

	if err := src.Exclude(Exclusion{
		Title: "Netflix",
		At:    now,
	}); err == nil {
		t.Fatal("an exclusion needs the item's external id")
	}

	netflix := Exclusion{
		ExternalID: "psn:NETFLIX",
		Title:      "Netflix",
		At:         now,
	}
	spotify := Exclusion{
		ExternalID: "psn:SPOTIFY",
		Title:      "Spotify",
		At:         now.Add(time.Hour),
	}

	for _, e := range []Exclusion{netflix, spotify, {
		ExternalID: "psn:NETFLIX",
		Title:      "Renamed",
		At:         now.Add(2 * time.Hour),
	}} {
		if err := src.Exclude(e); err != nil {
			t.Fatal(err)
		}
	}

	got := src.Exclusions()
	if len(got) != 2 || got[0] != spotify || got[1] != netflix {
		t.Fatalf("newest first, the first entry kept for a repeat: %+v", got)
	}

	if !src.Excludes("psn:NETFLIX") || src.Excludes("psn:YOUTUBE") {
		t.Fatal("Excludes must answer for listed ids only")
	}

	got[0].Title = "changed"
	if src.Exclusions()[0].Title != "Spotify" {
		t.Fatal("Exclusions must return a copy")
	}

	if !src.Include("psn:NETFLIX") || src.Include("psn:NETFLIX") || src.Excludes("psn:NETFLIX") {
		t.Fatal("Include takes an id off the list once")
	}
}
