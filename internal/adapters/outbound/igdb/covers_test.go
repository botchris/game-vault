package igdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

func TestCovers_applies(t *testing.T) {
	c := NewCovers(NewClient())

	for _, tc := range []struct {
		name string
		q    media.CoverQuery
		want bool
	}{
		{"a PC edition, first pass", media.CoverQuery{System: game.SystemPC}, false},
		{"a game without copies, first pass", media.CoverQuery{}, false},
		{"a PS4 edition, first pass", media.CoverQuery{System: "PS4"}, false},
		{"a PC edition no provider had art for", media.CoverQuery{
			System:   game.SystemPC,
			Fallback: true,
		}, true},
		{"a PS4 edition no provider had art for", media.CoverQuery{
			System:   "PS4",
			Fallback: true,
		}, true},
	} {
		assert.Equal(t, tc.want, c.Applies(tc.q), tc.name)
	}
}

func TestCovers_startsDisabled(t *testing.T) {
	t.Run("GIVEN the IGDB cover provider, which needs a Twitch application", func(t *testing.T) {
		t.Run("THEN it starts disabled, like every provider that needs credentials", func(t *testing.T) {
			assert.False(t, NewCovers(NewClient()).Descriptor().EnabledByDefault)
		})
	})
}

func TestCovers_outages(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.CoverQuery{
		Title:    "Example Quest",
		Fallback: true,
	}

	for name, setup := range map[string]func(f *fakeIGDB){
		"Twitch is down (HTTP 503)": func(f *fakeIGDB) { f.tokenDown = true },
		"Twitch hangs up":           func(f *fakeIGDB) { f.hangUp = true },
		"IGDB is down (HTTP 502)":   func(f *fakeIGDB) { f.apiDown = true },
	} {
		t.Run("GIVEN "+name, func(t *testing.T) {
			f := newFakeIGDB(t)
			f.set(setup)
			c := NewCovers(f.client(time.Now))

			t.Run("WHEN covers are asked", func(t *testing.T) {
				_, err := c.Covers(ctx, q, goodSettings)

				t.Run("THEN it counts as limited, so the edition is not marked missing for days", func(t *testing.T) {
					require.ErrorIs(t, err, provider.ErrRateLimited)
					assert.Contains(t, err.Error(), "try again later")
				})
			})
		})
	}

	t.Run("GIVEN Twitch answers HTTP 503", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.set(func(f *fakeIGDB) { f.tokenDown = true })

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := f.client(time.Now).Test(ctx, goodSettings)

			t.Run("THEN it says Twitch is not answering, not to check the connection", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "Twitch is not answering: try again later")
				assert.NotContains(t, err.Error(), "check the connection")
			})
		})
	})
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
				Title:    "Example Quest",
				System:   game.SystemPC,
				Links:    game.Links{game.LinkSteam: "620"},
				Fallback: true,
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

	t.Run("GIVEN IGDB has no credentials yet", func(t *testing.T) {
		f := newFakeIGDB(t)
		c := NewCovers(f.client(time.Now))

		t.Run("WHEN covers are asked", func(t *testing.T) {
			got, err := c.Covers(ctx, media.CoverQuery{
				Title:    "Example Quest",
				Fallback: true,
			}, nil)

			t.Run("THEN it says so as a settings problem, without a request: an empty answer would clear the status", func(t *testing.T) {
				require.ErrorIs(t, err, ErrNotConfigured)
				require.ErrorIs(t, err, provider.ErrBadSettings)
				assert.Empty(t, got)
				assert.Zero(t, f.tokens())
				assert.Empty(t, f.sent("games"))
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

func TestPick_punctuation(t *testing.T) {
	t.Run("GIVEN search results named with ': ' for a title written with ' - '", func(t *testing.T) {
		games := []gameJSON{
			{
				ID:   1,
				Name: "Example: A Wild Adventure",
			},
			{
				ID:   2,
				Name: "Example: A Wild Adventure Remastered",
			},
		}

		t.Run("WHEN the game is picked", func(t *testing.T) {
			got := pick(games, "Example - A Wild Adventure")

			t.Run("THEN it is the game with every word, whatever the punctuation", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, int64(1), got.ID)
			})
		})
	})
}
