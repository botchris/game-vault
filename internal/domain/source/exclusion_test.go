package source

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

func TestExclusions(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	src := Rehydrate(Snapshot{
		ID:        "s1",
		Type:      "psn",
		Name:      "PlayStation",
		Enabled:   true,
		Settings:  schema.Settings{},
		CreatedAt: now,
		UpdatedAt: now,
	})

	require.Error(t, src.Exclude(Exclusion{
		Title: "Netflix",
		At:    now,
	}), "an exclusion needs the item's external id")

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
		require.NoError(t, src.Exclude(e))
	}

	got := src.Exclusions()
	assert.Equal(t, []Exclusion{spotify, netflix}, got, "newest first, the first entry kept for a repeat")

	assert.True(t, src.Excludes("psn:NETFLIX"), "a listed id")
	assert.False(t, src.Excludes("psn:YOUTUBE"), "an id not listed")

	got[0].Title = "changed"
	assert.Equal(t, "Spotify", src.Exclusions()[0].Title, "Exclusions must return a copy")

	assert.True(t, src.Include("psn:NETFLIX"), "Include takes a listed id off the list")
	assert.False(t, src.Include("psn:NETFLIX"), "only once")
	assert.False(t, src.Excludes("psn:NETFLIX"))
}
