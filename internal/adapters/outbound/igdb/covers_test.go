package igdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

func TestCovers_applies(t *testing.T) {
	c := NewCovers(NewClient())

	for _, tc := range []struct {
		name string
		q    media.CoverQuery
		want bool
	}{
		{"a PC edition", media.CoverQuery{System: game.SystemPC}, true},
		{"a game without copies", media.CoverQuery{}, true},
		{"a PS4 edition", media.CoverQuery{System: "PS4"}, false},
		{"a PS4 edition no store had art for", media.CoverQuery{
			System:   "PS4",
			Fallback: true,
		}, true},
	} {
		assert.Equal(t, tc.want, c.Applies(tc.q), tc.name)
	}
}

func TestCovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a PC game IGDB has a cover for", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[{"id": 5, "external_game_source": 1, "uid": "620", "game": ` + exampleQuest + `}]`
		c := NewCovers(f.client(time.Now))

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := c.Covers(ctx, media.CoverQuery{
				Title:  "Example Quest",
				System: game.SystemPC,
				Links:  game.Links{game.LinkSteam: "620"},
			}, goodSettings)
			require.NoError(t, err)

			t.Run("THEN the large cover is proposed, with a smaller thumbnail", func(t *testing.T) {
				require.Len(t, got, 1)
				assert.Equal(t, "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/co-example.jpg", got[0].URL)
				assert.Equal(t, "https://images.igdb.com/igdb/image/upload/t_cover_big/co-example.jpg", got[0].ThumbURL)
				assert.Equal(t, "Example Quest · 2011", got[0].Label)
				assert.Equal(t, "Example Quest", got[0].Title)
				assert.Equal(t, CoversID, got[0].Provider)
			})
		})
	})

	t.Run("GIVEN a game IGDB knows without a cover", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.search = `[{"id": 7, "name": "Bare Quest"}]`
		c := NewCovers(f.client(time.Now))

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := c.Covers(ctx, media.CoverQuery{Title: "Bare Quest"}, goodSettings)

			t.Run("THEN there are no candidates and no error", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, got)
			})
		})
	})
}
