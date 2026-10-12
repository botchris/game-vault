package game

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchKey_concurrent(t *testing.T) {
	titles := map[string]string{
		"Pokémon™ Épée":                    "pokemon epee",
		"Ōkami HD":                         "okami hd",
		"Brütal Légend (Steam Key)":        "brutal legend",
		"Señor Café: Ñandú Édition Gold":   "senor cafe nandu",
		"Ys VIII: Lacrimosa of DANA – PS4": "ys viii lacrimosa of dana ps4",
	}

	t.Run("GIVEN titles with accents, WHEN many goroutines reduce them at once", func(t *testing.T) {
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			wrong []string
		)

		for range 16 {
			wg.Go(func() {
				for range 500 {
					for in, want := range titles {
						if got := MatchKey(in); got != want {
							mu.Lock()

							wrong = append(wrong, in+" → "+got)
							mu.Unlock()
						}
					}
				}
			})
		}

		wg.Wait()

		t.Run("THEN every result is the same as one call alone", func(t *testing.T) {
			assert.Empty(t, wrong)
		})
	})
}

func TestSearchTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Example West GOTY":               "Example West",
		"Example West 2":                  "Example West 2",
		"Mafia: Definitive Edition":       "Mafia",
		"Example Game (PC Key)":           "Example Game",
		"Celeste™ (Steam Key)":            "Celeste",
		"Hades GOG Key":                   "Hades",
		"Slay the Spire - Deluxe Edition": "Slay the Spire",
		"The Witcher 3: Wild Hunt – Game of the Year Edition": "The Witcher 3: Wild Hunt",
		"Pokémon™ Épée":                 "Pokémon Épée",
		"Ratchet & Clank [PS4]":         "Ratchet & Clank",
		"Deluxe Edition":                "Deluxe Edition", // nothing left: keep the title
		"  Spaced   Out  ":              "Spaced Out",
		"Foo - Deluxe Edition: The Bar": "Foo: The Bar",
		"Foo - Bar":                     "Foo - Bar",
		"Golden Sun":                    "Golden Sun",
		// Tag words that are part of the name are lost here; searches fall back to the raw title.
		"Gold Rush!":            "Rush!",
		"PC Building Simulator": "Building Simulator",
	} {
		assert.Equal(t, want, SearchTitle(title), title)
	}
}

func TestNormalizeTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Celeste™":        "Celeste",
		"  Hades®  II ":   "Hades II",
		"Example © Quest": "Example Quest",
		"Example West":    "Example West",
	} {
		assert.Equal(t, want, NormalizeTitle(title), title)
	}
}

func TestSameTitle(t *testing.T) {
	for _, tc := range []struct {
		a    string
		b    string
		want bool
	}{
		{"Example - A Wild Adventure", "Example: A Wild Adventure", true},
		{"EXAMPLE QUEST™", "Example Quest", true},
		{"Example Quest: Part 2", "Example Quest Part 2", true},
		{"Pokémon Example", "Pokemon Example", true},
		{"Example Remastered", "Example", false},
		{"Doom (2016)", "Doom", false},
		{"Example Quest 2", "Example Quest", false},
		{"Re-Volt", "Revolt", false},
		{"F.E.A.R.", "Fear", false},
		{"Hit Man", "Hitman", false},
		{"Ratchet & Clank", "Ratchet and Clank", true},
		{"Señor Café", "senor cafe", true},
		{"Ørn", "Orn", true},
		{"Bjæl Straße", "bjael strasse", true},
		{"Łódź Quest", "Lodz Quest", true},
		{"ファイナルクエスト", "ドラゴンクエスト", false},
		{"ファイナル・クエスト", "ファイナル クエスト", true},
		{"", "", false},
		{"!!!", "???", false},
	} {
		assert.Equal(t, tc.want, SameTitle(tc.a, tc.b), "%q vs %q", tc.a, tc.b)
	}
}
