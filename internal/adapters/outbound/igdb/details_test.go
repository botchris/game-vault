package igdb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

func TestDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game linked to Steam that IGDB knows by its AppID", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[{"id": 5, "external_game_source": 1, "uid": "620", "game": ` + exampleQuest + `}]`
		d := NewDetails(f.client(time.Now))
		q := media.CoverQuery{
			Title:     "Example Quest",
			Links:     game.Links{game.LinkSteam: "620", "gog": "1207658924"},
			Platforms: []string{"Steam"},
		}

		t.Run("WHEN its sheet is asked in any language", func(t *testing.T) {
			got, err := d.Details(ctx, q, "es", goodSettings)
			require.NoError(t, err)
			require.NotNil(t, got)

			t.Run("THEN the game is found by its store links, without a title search", func(t *testing.T) {
				sent := f.sent("external_games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], `external_game_source = 1 & uid = "620"`)
				assert.Contains(t, sent[0], `external_game_source = 5 & uid = "1207658924"`)
				assert.Empty(t, f.sent("games"))
			})

			t.Run("THEN the sheet holds IGDB's English summary and facts", func(t *testing.T) {
				assert.Equal(t, "Example Quest is an invented adventure.", got.Summary)
				assert.Equal(t, []string{"Adventure", "Puzzle"}, got.Genres)
				assert.Equal(t, []string{"Invented Studio"}, got.Developers)
				assert.Equal(t, []string{"Invented Publishing"}, got.Publishers)
				assert.Equal(t, "2011-04-19", got.ReleaseDate)
				assert.Equal(t, "PEGI 12", got.AgeRating)
				assert.Equal(t, "https://www.igdb.com/games/example-quest", got.StoreURL)
				assert.Empty(t, got.SummarySource, "IGDB's summaries need no credit")

				t.Run("AND its screenshots and YouTube trailer", func(t *testing.T) {
					require.Len(t, got.Screenshots, 2)
					assert.Equal(t, "https://images.igdb.com/igdb/image/upload/t_screenshot_big/sc-one.jpg", got.Screenshots[0].ThumbURL)
					assert.Equal(t, "https://images.igdb.com/igdb/image/upload/t_1080p/sc-one.jpg", got.Screenshots[0].FullURL)
					require.Len(t, got.Videos, 1)
					assert.Equal(t, "yt-example", got.Videos[0].YouTubeID)
					assert.Equal(t, "https://i.ytimg.com/vi/yt-example/hqdefault.jpg", got.Videos[0].Thumbnail)
				})
			})
		})
	})

	t.Run("GIVEN a PS4 disc of a game no store links", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.search = `[` + exampleQuest + `, {"id": 1002, "name": "Example Quest: Extra Chapter", "summary": "An expansion."}]`
		d := NewDetails(f.client(time.Now))
		q := media.CoverQuery{
			Title:             "Example Quest",
			PhysicalPlatforms: []string{"PS4"},
			Platforms:         []string{"PS4"},
		}

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, q, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the title is searched on the PS4 and the exact title wins", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], `search "Example Quest";`)
				assert.Equal(t, []string{"48"}, f.searchedPlatforms())
				require.NotNil(t, got)
				assert.Equal(t, "Example Quest is an invented adventure.", got.Summary)
				assert.Empty(t, f.sent("external_games"), "a game without store links skips the link lookup")
			})
		})
	})

	t.Run("GIVEN a Steam game IGDB does not know by its AppID", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.search = `[` + exampleQuest + `]`
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{
				Title:     "Example Quest",
				Links:     game.Links{game.LinkSteam: "999"},
				Platforms: []string{"Steam"},
			}, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the title is searched on PC instead", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Equal(t, []string{"6,14,3"}, f.searchedPlatforms())
				require.NotNil(t, got)
				assert.Equal(t, "Example Quest is an invented adventure.", got.Summary)
			})
		})
	})

	t.Run("GIVEN a title IGDB has for two different games", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.search = `[{"id": 1, "name": "Remake Quest", "summary": "The 1993 original."},
			{"id": 2, "name": "Remake Quest", "summary": "The 2016 reboot."},
			{"id": 3, "name": "Remake Quest", "summary": "A special edition.", "version_parent": 2}]`
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{Title: "Remake Quest"}, "en", goodSettings)

			t.Run("THEN nothing is returned rather than the wrong game", func(t *testing.T) {
				require.NoError(t, err)
				assert.Nil(t, got)
				assert.Equal(t, []string{""}, f.searchedPlatforms(), "no system known: no platform filter")
			})
		})
	})

	t.Run("GIVEN a title IGDB has as a game and as its edition", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.search = `[{"id": 3, "name": "Edition Quest", "summary": "A special edition.", "version_parent": 2},
			{"id": 2, "name": "Edition Quest", "summary": "The game."}]`
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{Title: "Edition Quest"}, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the game itself is used", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "The game.", got.Summary)
			})
		})
	})

	t.Run("GIVEN a title IGDB only has as other games", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.search = `[{"id": 1002, "name": "Example Quest: Extra Chapter", "summary": "An expansion."}]`
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{Title: "Example Quest"}, "en", goodSettings)

			t.Run("THEN nothing is returned", func(t *testing.T) {
				require.NoError(t, err)
				assert.Nil(t, got)
			})
		})
	})

	t.Run("GIVEN a title with quotes and backslashes", func(t *testing.T) {
		f := newFakeIGDB(t)
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			_, err := d.Details(ctx, media.CoverQuery{Title: `Say "Hi" \ Bye`}, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN they cannot break out of the query's string", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], `search "Say Hi Bye";`)
				assert.Equal(t, 2, strings.Count(sent[0], `"`))
			})
		})
	})

	t.Run("GIVEN IGDB is busy", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.setBusy(true)
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN a sheet is asked", func(t *testing.T) {
			_, err := d.Details(ctx, media.CoverQuery{Title: "Example Quest"}, "en", goodSettings)

			t.Run("THEN it is a rate limit, so the sheet is not cached", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}

func TestDetails_editionTitles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	disc := media.CoverQuery{
		Title:             "Example West GOTY",
		PhysicalPlatforms: []string{"Xbox 360"},
		Platforms:         []string{"Xbox 360"},
	}

	t.Run("GIVEN an IGDB that only finds the game by its name, without the edition tag", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.searchFor = map[string]string{"Example West": `[
			{"id": 1, "name": "Example West", "summary": "The game."},
			{"id": 2, "name": "Example West: Game of the Year Edition", "summary": "Its edition.", "version_parent": 1},
			{"id": 3, "name": "Example West 2", "summary": "The sequel."}]`}
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN the sheet of a GOTY disc is asked", func(t *testing.T) {
			got, err := d.Details(ctx, disc, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the name is searched and the main game wins over its edition and its sequel", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "The game.", got.Summary)
				assert.Equal(t, []string{"Example West"}, f.searchTerms(), "the raw title is not needed")
			})
		})
	})

	t.Run("GIVEN an IGDB that only finds the game by the raw title", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.searchFor = map[string]string{"Example West GOTY": `[{"id": 1, "name": "Example West GOTY", "summary": "Odd entry."}]`}
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, disc, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the raw title is searched after the name found nothing", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "Odd entry.", got.Summary)
				assert.Equal(t, []string{"Example West", "Example West GOTY"}, f.searchTerms())
			})
		})
	})

	t.Run("GIVEN a title without edition tags IGDB does not know", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.searchFor = map[string]string{}
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			_, err := d.Details(ctx, media.CoverQuery{Title: "Plain Quest"}, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN it is searched once", func(t *testing.T) {
				assert.Equal(t, []string{"Plain Quest"}, f.searchTerms())
			})
		})
	})
}

func TestDetails_severalGamesForOneLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.CoverQuery{
		Title: "Example Quest",
		Links: game.Links{game.LinkSteam: "620"},
	}

	t.Run("GIVEN IGDB lists a Steam AppID under the game and its edition", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[{"id": 5, "external_game_source": 1, "uid": "620", "game": {"id": 2, "name": "Example Quest: Deluxe", "summary": "An edition.", "version_parent": 1}},
			{"id": 6, "external_game_source": 1, "uid": "620", "game": {"id": 1, "name": "Example Quest", "summary": "The game."}}]`
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, q, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the game itself is described", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "The game.", got.Summary)
				assert.Contains(t, f.sent("external_games")[0], "game.version_parent")
			})
		})
	})

	t.Run("GIVEN IGDB lists a Steam AppID under two different games", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[{"id": 5, "external_game_source": 1, "uid": "620", "game": {"id": 1, "name": "Example Quest", "summary": "One."}},
			{"id": 6, "external_game_source": 1, "uid": "620", "game": {"id": 3, "name": "Example Quest Online", "summary": "Another."}}]`
		d := NewDetails(f.client(time.Now))

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, q, "en", goodSettings)
			require.NoError(t, err)

			t.Run("THEN the link is not trusted, and the title search decides", func(t *testing.T) {
				assert.Nil(t, got)
				assert.Len(t, f.sent("games"), 1)
			})
		})
	})
}

func TestAgeRating(t *testing.T) {
	rating := func(org, value string) ageRatingJSON {
		var r ageRatingJSON

		r.Organization.Name = org
		r.Category.Rating = value

		return r
	}

	for _, tc := range []struct {
		name    string
		ratings []ageRatingJSON
		want    string
	}{
		{"PEGI in words", []ageRatingJSON{rating("PEGI", "Eighteen")}, "PEGI 18"},
		{"PEGI in digits", []ageRatingJSON{rating("PEGI", "16")}, "PEGI 16"},
		{"PEGI with its own prefix", []ageRatingJSON{rating("PEGI", "PEGI 7")}, "PEGI 7"},
		{"ESRB as a letter", []ageRatingJSON{rating("ESRB", "M")}, "ESRB M"},
		{"ESRB E10", []ageRatingJSON{rating("ESRB", "E10")}, "ESRB E10+"},
		{"PEGI before ESRB", []ageRatingJSON{rating("ESRB", "T"), rating("PEGI", "Twelve")}, "PEGI 12"},
		{"another board alone", []ageRatingJSON{rating("USK", "USK_12")}, "USK 12"},
		{"none", nil, ""},
	} {
		assert.Equal(t, tc.want, ageRating(tc.ratings), tc.name)
	}
}
