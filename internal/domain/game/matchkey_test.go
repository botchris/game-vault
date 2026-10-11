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
		"Red Dead Redemption GOTY":                            "Red Dead Redemption",
		"Red Dead Redemption 2":                               "Red Dead Redemption 2",
		"Mafia: Definitive Edition":                           "Mafia",
		"Example Game (PC Key)":                               "Example Game",
		"Celeste™ (Steam Key)":                                "Celeste",
		"Hades GOG Key":                                       "Hades",
		"Slay the Spire - Deluxe Edition":                     "Slay the Spire",
		"The Witcher 3: Wild Hunt – Game of the Year Edition": "The Witcher 3: Wild Hunt",
		"Pokémon™ Épée":                                       "Pokémon Épée",
		"Ratchet & Clank [PS4]":                               "Ratchet & Clank",
		"Deluxe Edition":                                      "Deluxe Edition", // nothing left: keep the title
		"  Spaced   Out  ":                                    "Spaced Out",
	} {
		assert.Equal(t, want, SearchTitle(title), title)
	}
}
