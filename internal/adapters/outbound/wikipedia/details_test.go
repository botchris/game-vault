package wikipedia

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/ratelimit"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

const (
	windows = "Q1406"
	ps4     = "Q5014725"
	album   = "Q482994"
)

// exampleWorld is Wikidata and Wikipedia knowing one game, its soundtrack, and its companies.
func exampleWorld(f *fakeWiki) {
	f.entities["Q100"] = entity{
		labels: map[string]string{"mul": "Example Quest"},
		claims: map[string][]string{
			"P31":  {videoGame},
			"P400": {windows, "Q10683"},
			"P178": {"Q200"},
			"P123": {"Q201"},
			"P136": {"Q300"},
		},
		dates: []string{"+2011-04-19T00:00:00Z", "+2011-04-18T00:00:00Z", "+2014-02-26T00:00:00Z"},
		sitelinks: map[string]string{
			"enwiki": "Example Quest",
			"eswiki": "Example Quest (videojuego)",
		},
	}
	f.entities["Q101"] = entity{
		labels: map[string]string{"en": "Example Quest"},
		claims: map[string][]string{"P31": {album}},
	}
	f.entities["Q200"] = entity{labels: map[string]string{"mul": "Invented Studio"}}
	f.entities["Q201"] = entity{labels: map[string]string{
		"en": "Invented Publishing",
		"es": "Editorial Inventada",
	}}
	f.entities["Q300"] = entity{labels: map[string]string{
		"en": "puzzle video game",
		"es": "videojuego de lógica",
	}}
	f.searches["en:Example Quest"] = []string{"Q100", "Q101"}
	f.searches["es:Example Quest"] = []string{"Q100"}
	f.articles["en:Example Quest"] = article{extract: "Example Quest is an invented puzzle game.\nIt was released in 2011."}
	f.articles["es:Example Quest (videojuego)"] = article{extract: "Example Quest es un videojuego de lógica inventado.\nSe lanzó en 2011."}
}

func TestDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	pcGame := media.CoverQuery{
		Title:     "Example Quest",
		Platforms: []string{"Steam"},
	}

	t.Run("GIVEN a PC game Wikidata knows, with an article in the UI language", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		d := f.details()

		t.Run("WHEN its sheet is asked in Spanish", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "es", nil)
			require.NoError(t, err)
			require.NotNil(t, got)

			t.Run("THEN the summary is that article's introduction, with paragraphs", func(t *testing.T) {
				assert.Equal(t, "Example Quest es un videojuego de lógica inventado.\n\nSe lanzó en 2011.", got.Summary)

				t.Run("AND it credits the article and its license", func(t *testing.T) {
					assert.Equal(t, media.Attribution{
						Name:       "Wikipedia",
						URL:        "https://es.wikipedia.org/wiki/Example_Quest_(videojuego)",
						License:    "CC BY-SA 4.0",
						LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
					}, got.SummarySource)
				})
			})

			t.Run("THEN the facts come from Wikidata, labeled in the UI language or else English", func(t *testing.T) {
				assert.Equal(t, []string{"Invented Studio"}, got.Developers)
				assert.Equal(t, []string{"Editorial Inventada"}, got.Publishers)
				assert.Equal(t, []string{"Videojuego de lógica"}, got.Genres)
				assert.Equal(t, "2011-04-18", got.ReleaseDate, "the earliest publication date")
			})

			t.Run("THEN labels are asked in the UI language, then English, then the neutral mul", func(t *testing.T) {
				for _, langs := range f.languagesAsked() {
					assert.Equal(t, "es|en|mul", langs)
				}
			})

			t.Run("THEN every request names Game Vault and how to reach it", func(t *testing.T) {
				for _, ua := range f.userAgents() {
					assert.Equal(t, "GameVault/1.2.3 (https://github.com/botchris/game-vault)", ua)
				}
			})
		})

		t.Run("WHEN its sheet is asked in a language without an article", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "fr", nil)
			require.NoError(t, err)
			require.NotNil(t, got)

			t.Run("THEN the English article and labels are used", func(t *testing.T) {
				assert.Equal(t, "Example Quest is an invented puzzle game.\n\nIt was released in 2011.", got.Summary)
				assert.Equal(t, "https://en.wikipedia.org/wiki/Example_Quest", got.SummarySource.URL)
				assert.Equal(t, []string{"Invented Publishing"}, got.Publishers)
				assert.Equal(t, []string{"Puzzle video game"}, got.Genres)
			})
		})

		t.Run("WHEN the UI language is regional", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "es-MX", nil)
			require.NoError(t, err)

			t.Run("THEN its primary language's Wikipedia is used", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "https://es.wikipedia.org/wiki/Example_Quest_(videojuego)", got.SummarySource.URL)
				assert.Equal(t, []string{"Editorial Inventada"}, got.Publishers)
			})
		})

		t.Run("WHEN the UI language has a script subtag and no article", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "zh-Hans", nil)
			require.NoError(t, err)

			t.Run("THEN it falls back to English, asking labels in zh first", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "https://en.wikipedia.org/wiki/Example_Quest", got.SummarySource.URL)

				langs := f.languagesAsked()
				assert.Equal(t, "zh|en|mul", langs[len(langs)-1])
			})
		})

		t.Run("WHEN the language is not a language code", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "../evil", nil)
			require.NoError(t, err)

			t.Run("THEN English is used", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "https://en.wikipedia.org/wiki/Example_Quest", got.SummarySource.URL)
			})
		})
	})

	t.Run("GIVEN two video games with the title, both on PC", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		f.entities["Q102"] = entity{
			labels: map[string]string{"mul": "Example Quest"},
			claims: map[string][]string{
				"P31":  {videoGame},
				"P400": {windows},
			},
			sitelinks: map[string]string{"enwiki": "Example Quest (2016 video game)"},
		}
		f.searches["en:Example Quest"] = []string{"Q100", "Q102"}
		d := f.details()

		t.Run("WHEN the PC game's sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "en", nil)

			t.Run("THEN nothing is returned rather than the wrong game", func(t *testing.T) {
				require.NoError(t, err)
				assert.Nil(t, got)
			})
		})

		t.Run("WHEN a PS4 disc's sheet is asked, and only one of them is on the PS4", func(t *testing.T) {
			e := f.entities["Q102"]
			e.claims["P400"] = []string{ps4}
			f.entities["Q102"] = e
			f.articles["en:Example Quest (2016 video game)"] = article{extract: "The 2016 reboot."}

			got, err := d.Details(ctx, media.CoverQuery{
				Title:             "Example Quest",
				PhysicalPlatforms: []string{"PS4"},
				Platforms:         []string{"PS4"},
			}, "en", nil)
			require.NoError(t, err)

			t.Run("THEN that one is used", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "The 2016 reboot.", got.Summary)
			})
		})
	})

	t.Run("GIVEN the only video game with the title is on other systems", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		d := f.details()

		t.Run("WHEN a Switch copy's sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{
				Title:     "Example Quest",
				Platforms: []string{"Switch"},
			}, "en", nil)

			t.Run("THEN nothing is returned: it may be another game", func(t *testing.T) {
				require.NoError(t, err)
				assert.Nil(t, got)
			})
		})
	})

	t.Run("GIVEN the only video game with the title has no platforms on Wikidata", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		e := f.entities["Q100"]
		delete(e.claims, "P400")
		f.entities["Q100"] = e
		d := f.details()

		t.Run("WHEN a Switch copy's sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{
				Title:     "Example Quest",
				Platforms: []string{"Switch"},
			}, "en", nil)
			require.NoError(t, err)

			t.Run("THEN it is used: nothing contradicts it", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, []string{"Invented Studio"}, got.Developers)
			})
		})
	})

	t.Run("GIVEN a game whose article is a disambiguation page", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		f.articles["en:Example Quest"] = article{
			extract:        "Example Quest may refer to:",
			disambiguation: true,
		}
		d := f.details()

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, pcGame, "en", nil)
			require.NoError(t, err)

			t.Run("THEN the page is not used as a summary, but the facts are", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Empty(t, got.Summary)
				assert.Empty(t, got.SummarySource)
				assert.Equal(t, []string{"Invented Studio"}, got.Developers)
			})
		})
	})

	t.Run("GIVEN a title Wikidata does not know", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		d := f.details()

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := d.Details(ctx, media.CoverQuery{Title: "Unknown Quest"}, "en", nil)

			t.Run("THEN nothing is returned, after the search alone", func(t *testing.T) {
				require.NoError(t, err)
				assert.Nil(t, got)
				assert.Equal(t, []string{"wbsearchentities"}, f.actions())
			})
		})
	})

	t.Run("GIVEN Wikidata answers with an API error", func(t *testing.T) {
		f := newFakeWiki(t)
		f.setFail("internal_api_error_DBQueryError")
		d := f.details()

		t.Run("WHEN a sheet is asked", func(t *testing.T) {
			_, err := d.Details(ctx, pcGame, "en", nil)

			t.Run("THEN its message is reported, with what to do", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "The API answered internal_api_error_DBQueryError.")
				assert.Contains(t, err.Error(), "try again later; if it persists, report this message")
			})
		})

		t.Run("WHEN it is lagging behind", func(t *testing.T) {
			f.setFail("maxlag")

			_, err := d.Details(ctx, pcGame, "en", nil)

			t.Run("THEN it is a rate limit", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})

		// Last: a 429 leaves the servers alone for a while.
		t.Run("WHEN it asks to slow down", func(t *testing.T) {
			f.setFail("429")

			_, err := d.Details(ctx, pcGame, "en", nil)

			t.Run("THEN it is a rate limit too, so the sheet is not cached", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})

		t.Run("WHEN another sheet is asked at once", func(t *testing.T) {
			asked := len(f.actions())

			_, err := d.Details(ctx, pcGame, "en", nil)

			t.Run("THEN it is rate-limited without a request: the servers are left alone for a while", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Len(t, f.actions(), asked)
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
	west := entity{
		labels: map[string]string{"mul": "Example West"},
		claims: map[string][]string{
			"P31":  {videoGame},
			"P400": {"Q48263"},
		},
		sitelinks: map[string]string{"enwiki": "Example West"},
	}

	t.Run("GIVEN Wikidata knows the game by its name, without the edition tag", func(t *testing.T) {
		f := newFakeWiki(t)
		f.entities["Q500"] = west
		f.searches["en:Example West"] = []string{"Q500"}
		f.articles["en:Example West"] = article{extract: "Example West is a western."}

		t.Run("WHEN the sheet of a GOTY disc is asked", func(t *testing.T) {
			got, err := f.details().Details(ctx, disc, "en", nil)
			require.NoError(t, err)

			t.Run("THEN the name is searched and the game found", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "Example West is a western.", got.Summary)
				assert.Equal(t, []string{"en:Example West"}, f.searchLog())
			})
		})
	})

	t.Run("GIVEN Wikidata only knows the raw title", func(t *testing.T) {
		f := newFakeWiki(t)
		raw := west
		raw.labels = map[string]string{"mul": "Example West GOTY"}
		f.entities["Q501"] = raw
		f.searches["en:Example West GOTY"] = []string{"Q501"}

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			got, err := f.details().Details(ctx, disc, "en", nil)
			require.NoError(t, err)

			t.Run("THEN the raw title is searched after the name found nothing", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, []string{"en:Example West", "en:Example West GOTY"}, f.searchLog())
			})
		})
	})
}

func TestDetails_trademarkedTitleSearchedOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a trademarked title Wikidata does not know", func(t *testing.T) {
		f := newFakeWiki(t)

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			got, err := f.details().Details(ctx, media.CoverQuery{Title: "Plain Quest™"}, "en", nil)
			require.NoError(t, err)

			t.Run("THEN it is searched once: the trademark sign alone makes no other title", func(t *testing.T) {
				assert.Nil(t, got)
				assert.Equal(t, []string{"en:Plain Quest"}, f.searchLog())
			})
		})
	})
}

func TestDetails_otherScripts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Wikidata only knows another game, titled in a script MatchKey drops", func(t *testing.T) {
		const title = "ファイナルクエスト"

		f := newFakeWiki(t)
		f.entities["Q700"] = entity{
			labels: map[string]string{"mul": "ドラゴンクエスト"},
			claims: map[string][]string{
				"P31":  {videoGame},
				"P400": {windows},
			},
			sitelinks: map[string]string{"enwiki": "Invented Other Quest"},
		}
		f.articles["en:Invented Other Quest"] = article{extract: "Another invented game."}

		for _, term := range []string{game.SearchTitle(title), game.NormalizeTitle(title)} {
			f.searches["en:"+term] = []string{"Q700"}
		}

		t.Run("WHEN the sheet of a game titled in such a script is asked", func(t *testing.T) {
			got, err := f.details().Details(ctx, media.CoverQuery{
				Title:     title,
				Platforms: []string{"Steam"},
			}, "en", nil)
			require.NoError(t, err)

			t.Run("THEN the other game's sheet is not used: two empty match keys are not the same game", func(t *testing.T) {
				assert.Nil(t, got)
			})
		})
	})
}

func TestDetails_exactTitleFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game and its remake, whose titles only match loosely", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		f.entities["Q103"] = entity{
			labels: map[string]string{"mul": "Example Quest: Definitive Edition"},
			claims: map[string][]string{
				"P31":  {videoGame},
				"P400": {windows},
			},
			sitelinks: map[string]string{"enwiki": "Example Quest: Definitive Edition"},
		}
		f.searches["en:Example Quest"] = []string{"Q103", "Q100"}

		t.Run("WHEN the original's sheet is asked", func(t *testing.T) {
			got, err := f.details().Details(ctx, media.CoverQuery{
				Title:     "Example Quest",
				Platforms: []string{"Steam"},
			}, "en", nil)
			require.NoError(t, err)

			t.Run("THEN the item with exactly that title is used, not the remake", func(t *testing.T) {
				require.NotNil(t, got)
				assert.Equal(t, "https://en.wikipedia.org/wiki/Example_Quest", got.SummarySource.URL)
			})
		})
	})
}

func TestDetails_applies(t *testing.T) {
	d := New("dev")

	for _, tc := range []struct {
		name string
		q    media.CoverQuery
		want bool
	}{
		{"a game without store links", media.CoverQuery{Title: "Example Quest"}, true},
		{"a game linked to another store", media.CoverQuery{Links: map[string]string{"gog": "1"}}, true},
		{"a game linked to Steam, whose localized summary always wins", media.CoverQuery{Links: map[string]string{"steam": "620"}}, false},
		{"a game linked to Steam on the fallback pass (Steam had nothing)", media.CoverQuery{
			Links:    map[string]string{"steam": "620"},
			Fallback: true,
		}, true},
	} {
		assert.Equal(t, tc.want, d.Applies(tc.q), tc.name)
	}
}

func TestDetails_releaseDatePrecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name  string
		dates []string
		want  string
	}{
		{"a full date beats the bare year it falls in", []string{"9:+2011-00-00T00:00:00Z", "+2011-04-19T00:00:00Z"}, "2011-04-19"},
		{"listed the other way round too", []string{"+2011-04-19T00:00:00Z", "9:+2011-00-00T00:00:00Z"}, "2011-04-19"},
		{"a month beats its year, a day its month", []string{"9:+2011-00-00T00:00:00Z", "10:+2011-04-00T00:00:00Z", "+2011-04-19T00:00:00Z"}, "2011-04-19"},
		{"an earlier year still wins", []string{"+2011-04-19T00:00:00Z", "9:+2010-00-00T00:00:00Z"}, "2010"},
		{"the earliest full date wins", []string{"+2011-04-19T00:00:00Z", "+2011-04-18T00:00:00Z"}, "2011-04-18"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWiki(t)
			exampleWorld(f)
			e := f.entities["Q100"]
			e.dates = tc.dates
			f.entities["Q100"] = e

			got, err := f.details().Details(ctx, media.CoverQuery{
				Title:     "Example Quest",
				Platforms: []string{"Steam"},
			}, "en", nil)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.ReleaseDate)
		})
	}
}

func TestDetails_refused(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Wikimedia refuses the client (no User-Agent, or blocked)", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		d := f.details()
		d.Wikidata.UserAgent = ""

		t.Run("WHEN a sheet is asked", func(t *testing.T) {
			_, err := d.Details(ctx, media.CoverQuery{Title: "Example Quest"}, "en", nil)

			t.Run("THEN the error says it was refused and what to do, as a problem of the provider", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "HTTP 403")
				assert.Contains(t, err.Error(), "try again later; if it persists, report this message")
				assert.ErrorIs(t, err, provider.ErrUnavailable)
			})
		})
	})
}

func TestDetails_outages(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, fail := range []string{"502", "hangup"} {
		t.Run("GIVEN Wikidata failing with "+fail, func(t *testing.T) {
			f := newFakeWiki(t)
			f.setFail(fail)

			t.Run("WHEN a sheet is asked", func(t *testing.T) {
				_, err := f.details().Details(ctx, media.CoverQuery{Title: "Example Quest"}, "en", nil)

				t.Run("THEN it is an outage, the provider's problem rather than the game's", func(t *testing.T) {
					require.ErrorIs(t, err, provider.ErrUnavailable)
				})
			})
		})
	}
}

func TestDetails_backsOffAfterADroppedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Wikidata dropped the connection without answering", func(t *testing.T) {
		f := newFakeWiki(t)
		exampleWorld(f)
		f.setFail("hangup")

		d := f.details()

		_, err := d.Details(ctx, media.CoverQuery{Title: "Example Quest"}, "en", nil)
		require.ErrorIs(t, err, provider.ErrUnavailable)

		t.Run("WHEN the next sheet is asked", func(t *testing.T) {
			f.setFail("")

			before := len(f.actions())
			_, err := d.Details(ctx, media.CoverQuery{Title: "Example Quest"}, "en", nil)

			t.Run("THEN the servers are left alone: it is refused as resting", func(t *testing.T) {
				require.ErrorIs(t, err, ratelimit.ErrResting)
				assert.Len(t, f.actions(), before)
			})
		})
	})
}

func TestTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Wikidata answers", func(t *testing.T) {
		f := newFakeWiki(t)
		d := f.details()

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := d.Test(ctx, nil)

			t.Run("THEN it works with one search", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, []string{"wbsearchentities"}, f.actions())
			})
		})

		t.Run("WHEN it fails", func(t *testing.T) {
			f.setFail("readonly")

			err := d.Test(ctx, nil)

			t.Run("THEN the error says so", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "readonly")
			})
		})
	})
}
