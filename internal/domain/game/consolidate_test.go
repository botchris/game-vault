package game

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// steamLink links to a Steam AppID; 0 means no link.
func steamLink(appID int64) Links {
	if appID == 0 {
		return nil
	}

	return Links{LinkSteam: strconv.FormatInt(appID, 10)}
}

func key(ext, title string, appID int64, status Status) ImportedCopy {
	return ImportedCopy{
		ExternalID: ext,
		Title:      title,
		Links:      steamLink(appID),
		Details: CopyDetails{
			Kind:     KindKey,
			Platform: "Steam",
			Status:   status,
			Key:      "KEY-" + ext,
		},
	}
}

func TestConsolidatorMatchesAndDedupes(t *testing.T) {
	// The Steam library already has Hades and Celeste.
	lib := NewConsolidator(nil).Apply("steam-src", []ImportedCopy{
		{
			ExternalID: "steam:1145360",
			Title:      "Hades",
			Links:      steamLink(1145360),
			Details: CopyDetails{
				Kind:     KindLibrary,
				Platform: "Steam",
			},
		},
		{
			ExternalID: "steam:504230",
			Title:      "Celeste",
			Links:      steamLink(504230),
			Details: CopyDetails{
				Kind:     KindLibrary,
				Platform: "Steam",
			},
		},
	}, t0)
	if lib.GamesCreated != 2 || lib.CopiesAdded != 2 {
		t.Fatalf("library import: %+v", lib)
	}

	c := NewConsolidator(lib.Changed)

	res := c.Apply("humble-src", []ImportedCopy{
		key("humble:A:0", "Hades", 1145360, StatusRevealed),            // matched by app id
		key("humble:A:1", "Celeste™ (Steam Key)", 0, StatusUnrevealed), // matched by title
		key("humble:A:2", "Brand New Game", 0, StatusUnrevealed),       // new game
	}, t0)
	if res.GamesCreated != 1 || res.CopiesAdded != 3 {
		t.Fatalf("humble import: %+v", res)
	}

	for _, g := range res.Changed {
		if g.Title() == "Hades" && (len(g.Copies()) != 2 || !g.IsRedundant(g.Copies()[1])) {
			t.Errorf("Hades should hold the library copy and a redundant key: %+v", g.Copies())
		}
	}

	// Re-scanning is idempotent.
	again := NewConsolidator(append(lib.Changed, res.Changed[2])).Apply("humble-src", []ImportedCopy{
		key("humble:A:0", "Hades", 1145360, StatusRevealed),
	}, t0)
	if again.CopiesUnchanged != 1 || again.CopiesAdded != 0 {
		t.Fatalf("re-scan should not add copies: %+v", again)
	}
}

func TestConsolidatorKeepsRedeemed(t *testing.T) {
	r := NewConsolidator(nil).Apply("h", []ImportedCopy{key("humble:B:0", "Portal", 400, StatusRevealed)}, t0)
	g := r.Changed[0]
	g.MarkRedundantKeysRedeemed(t0) // no library copy: nothing happens
	g.UpdateCopy(g.Copies()[0].ID, CopyDetails{
		Kind:     KindKey,
		Platform: "Steam",
		Status:   StatusRedeemed,
		Key:      "KEY-humble:B:0",
	}, t0)

	again := NewConsolidator([]*Game{g}).Apply("h", []ImportedCopy{key("humble:B:0", "Portal", 400, StatusRevealed)}, t0)
	if again.CopiesUnchanged != 1 || g.Copies()[0].Status != StatusRedeemed {
		t.Fatalf("a redeemed key must stay redeemed: %+v %+v", again, g.Copies()[0])
	}
}

func TestConsolidator_adoptsPreviousExternalID(t *testing.T) {
	t.Run("GIVEN an order whose keys were all saved under one old id", func(t *testing.T) {
		// What the old Humble ids did: both keys of order G were "humble:G:0", so the second key
		// overwrote the first copy, which stayed in Tunic's game with the other game's key.
		tunic := key("humble:G:0", "Tunic", 553420, StatusUnrevealed)
		tunic.Details.Notes = "box on the shelf"
		old := NewConsolidator(nil).Apply("humble-src", []ImportedCopy{
			tunic,
			key("humble:G:0", "Streets of Rage 4", 985890, StatusRevealed),
		}, t0)
		require.Len(t, old.Changed, 1)
		require.Equal(t, "KEY-humble:G:0", old.Changed[0].Copies()[0].Key)

		t.Run("WHEN the order is scanned with the new ids", func(t *testing.T) {
			newTunic := key("humble:G:tunic_steam:0", "Tunic", 553420, StatusUnrevealed)
			newTunic.Details.Key = ""
			newTunic.PreviousExternalID = "humble:G:0"
			sor4 := key("humble:G:streetsofrage4_steam:0", "Streets of Rage 4", 985890, StatusRevealed)
			sor4.PreviousExternalID = "humble:G:0"

			res := NewConsolidator(old.Changed).Apply("humble-src", []ImportedCopy{newTunic, sor4}, t0)

			t.Run("THEN the old copy is adopted by its own game and the other key gets a new copy", func(t *testing.T) {
				assert.Equal(t, 1, res.CopiesUpdated)
				assert.Equal(t, 1, res.CopiesAdded)
				assert.Equal(t, 1, res.GamesCreated)

				t.Run("AND the adopted copy loses the other game's key but keeps the user's notes", func(t *testing.T) {
					cp := old.Changed[0].Copies()[0]
					assert.Equal(t, "humble:G:tunic_steam:0", cp.ExternalID)
					assert.Empty(t, cp.Key)
					assert.Equal(t, StatusUnrevealed, cp.Status)
					assert.Equal(t, "box on the shelf", cp.Notes)
				})
			})

			t.Run("AND scanning again changes nothing", func(t *testing.T) {
				again := NewConsolidator(res.Changed).Apply("humble-src", []ImportedCopy{newTunic, sor4}, t0)
				assert.Equal(t, 0, again.CopiesAdded)
				assert.Equal(t, 2, again.CopiesUnchanged)
			})
		})
	})

	t.Run("GIVEN an old copy in another game", func(t *testing.T) {
		old := NewConsolidator(nil).Apply("humble-src", []ImportedCopy{key("humble:G:0", "Tunic", 553420, StatusRevealed)}, t0)

		t.Run("WHEN a key of a different game names it as its previous id", func(t *testing.T) {
			other := key("humble:G:hades_steam:0", "Hades", 1145360, StatusRevealed)
			other.PreviousExternalID = "humble:G:0"
			res := NewConsolidator(old.Changed).Apply("humble-src", []ImportedCopy{other}, t0)

			t.Run("THEN the old copy is left alone and the key gets its own game", func(t *testing.T) {
				assert.Equal(t, 1, res.CopiesAdded)
				assert.Equal(t, "humble:G:0", old.Changed[0].Copies()[0].ExternalID)
			})
		})
	})
}

func TestConsolidator_withdrawn(t *testing.T) {
	t.Run("GIVEN a source that imported a coupon as a game and a spare key of a game", func(t *testing.T) {
		lib := NewConsolidator(nil).Apply("steam-src", []ImportedCopy{
			{
				ExternalID: "steam:100",
				Title:      "Teleglitch",
				Links:      steamLink(100),
				Details: CopyDetails{
					Kind:     KindLibrary,
					Platform: "Steam",
				},
			},
		}, t0)
		old := NewConsolidator(lib.Changed).Apply("humble-src", []ImportedCopy{
			key("humble:C:coupon:0", "45% off Coupon", 0, StatusRevealed),
			key("humble:C:teleglitch_deadline:0", "Teleglitch", 100, StatusRevealed),
		}, t0)
		require.Equal(t, 2, old.CopiesAdded)

		games := old.Changed

		t.Run("WHEN the source withdraws both", func(t *testing.T) {
			res := NewConsolidator(games).Apply("humble-src", []ImportedCopy{
				{
					ExternalID: "humble:C:coupon:0",
					Title:      "45% off Coupon",
					Withdrawn:  true,
				},
				{
					ExternalID: "humble:C:teleglitch_deadline:0",
					Title:      "Teleglitch",
					Links:      steamLink(100),
					Withdrawn:  true,
				},
			}, t0)

			t.Run("THEN both copies are removed", func(t *testing.T) {
				assert.Equal(t, 2, res.CopiesRemoved)

				t.Run("AND the coupon's game is emptied, while the real game keeps its library copy", func(t *testing.T) {
					require.Len(t, res.Emptied, 1)
					require.Len(t, res.Changed, 1)
					assert.Equal(t, "Teleglitch", res.Changed[0].Title())
					assert.Len(t, res.Changed[0].Copies(), 1)
				})
			})
		})
	})

	t.Run("GIVEN copies of an order saved under one shared old id, in a real game", func(t *testing.T) {
		old := NewConsolidator(nil).Apply("humble-src", []ImportedCopy{key("humble:G:0", "Tunic", 553420, StatusRevealed)}, t0)

		t.Run("WHEN a non-game key of that order is withdrawn with the old id as previous", func(t *testing.T) {
			res := NewConsolidator(old.Changed).Apply("humble-src", []ImportedCopy{
				{
					ExternalID:         "humble:G:ashampoo:0",
					PreviousExternalID: "humble:G:0",
					Title:              "Ashampoo Photo Optimizer 7",
					Withdrawn:          true,
				},
			}, t0)

			t.Run("THEN the game's copy is left alone", func(t *testing.T) {
				assert.Equal(t, 0, res.CopiesRemoved)
				assert.Len(t, old.Changed[0].Copies(), 1)
			})
		})
	})

	t.Run("GIVEN a copy of the same id added by another source", func(t *testing.T) {
		old := NewConsolidator(nil).Apply("csv-import", []ImportedCopy{key("humble:C:coupon:0", "45% off Coupon", 0, StatusRevealed)}, t0)

		t.Run("WHEN the Humble source withdraws it", func(t *testing.T) {
			res := NewConsolidator(old.Changed).Apply("humble-src", []ImportedCopy{{
				ExternalID: "humble:C:coupon:0",
				Title:      "45% off Coupon",
				Withdrawn:  true,
			}}, t0)

			t.Run("THEN it is kept", func(t *testing.T) {
				assert.Equal(t, 0, res.CopiesRemoved)
			})
		})
	})
}

func TestConsolidator_keepsWhatTheUserSet(t *testing.T) {
	t.Run("GIVEN a library copy the user priced", func(t *testing.T) {
		r := NewConsolidator(nil).Apply("s", []ImportedCopy{{
			ExternalID: "steam:1",
			Title:      "Portal",
			Details: CopyDetails{
				Kind:     KindLibrary,
				Platform: "Steam",
			},
		}}, t0)
		g := r.Changed[0]
		_, err := g.UpdateCopy(g.Copies()[0].ID, CopyDetails{
			Kind:     KindLibrary,
			Platform: "Steam",
			Price: Money{
				Amount:   999,
				Currency: "EUR",
			},
		}, t0)
		require.NoError(t, err)

		t.Run("WHEN the library is scanned again", func(t *testing.T) {
			NewConsolidator([]*Game{g}).Apply("s", []ImportedCopy{{
				ExternalID: "steam:1",
				Title:      "Portal",
				Details: CopyDetails{
					Kind:     KindLibrary,
					Platform: "Steam",
				},
			}}, t0)

			t.Run("THEN the price is still there", func(t *testing.T) {
				assert.Equal(t, Money{
					Amount:   999,
					Currency: "EUR",
				}, g.Copies()[0].Price)
			})
		})
	})
}

func TestConsolidatorFileSystemColumn(t *testing.T) {
	psn := func(system string) ImportedCopy {
		return ImportedCopy{
			ExternalID: "csv:psn-astro",
			Title:      "Astro Bot",
			Details: CopyDetails{
				Kind:     KindLibrary,
				Platform: "PlayStation Store",
				System:   system,
			},
		}
	}

	t.Run("GIVEN a copy whose source says PS5", func(t *testing.T) {
		in := psn("")
		in.System = "PS5"

		first := NewConsolidator(nil).Apply("psn-src", []ImportedCopy{in}, t0)
		require.Len(t, first.Changed, 1)

		t.Run("WHEN a file exported from it is imported again", func(t *testing.T) {
			res := NewConsolidator(first.Changed).Apply("", []ImportedCopy{psn("PS5")}, t0)

			t.Run("THEN no override is stored and the copy is unchanged", func(t *testing.T) {
				assert.Equal(t, 1, res.CopiesUnchanged)
				assert.Empty(t, res.Changed)

				cp := first.Changed[0].Copies()[0]
				assert.Empty(t, cp.CopyDetails.System)
				assert.Equal(t, "PS5", cp.System())
			})
		})
	})

	t.Run("GIVEN a copy without a source system", func(t *testing.T) {
		first := NewConsolidator(nil).Apply("", []ImportedCopy{psn("")}, t0)

		t.Run("WHEN a file gives it PS5 and a later one leaves the system empty", func(t *testing.T) {
			set := NewConsolidator(first.Changed).Apply("", []ImportedCopy{psn("PS5")}, t0)
			NewConsolidator(first.Changed).Apply("", []ImportedCopy{psn("")}, t0)

			t.Run("THEN the override is kept", func(t *testing.T) {
				assert.Equal(t, 1, set.CopiesUpdated)
				assert.Equal(t, "PS5", first.Changed[0].Copies()[0].CopyDetails.System)
			})
		})
	})
}

func TestConsolidatorFirstCopyTakesCoverOnSourceSystem(t *testing.T) {
	const url = "https://example.test/astro.jpg"

	t.Run("GIVEN a game without copies whose cover was chosen", func(t *testing.T) {
		g, err := New("Astro Bot", t0)
		require.NoError(t, err)
		require.NoError(t, g.SetEditionCover("", EditionCover{URL: url}, t0))

		t.Run("WHEN a source whose system (PS5) differs from its platform's (PS4) adds its first copy", func(t *testing.T) {
			res := NewConsolidator([]*Game{g}).Apply("psn-src", []ImportedCopy{{
				ExternalID: "psn:astro",
				Title:      "Astro Bot",
				System:     "PS5",
				Details: CopyDetails{
					Kind:     KindLibrary,
					Platform: "PlayStation Store",
				},
			}}, t0)
			require.Len(t, res.Changed, 1)
			require.Equal(t, "PS4", SystemOf("PlayStation Store"))

			t.Run("THEN the cover moves to the PS5 edition", func(t *testing.T) {
				assert.Equal(t, map[string]EditionCover{"PS5": {URL: url}}, g.Covers())
			})

			t.Run("AND it survives a re-read", func(t *testing.T) {
				read := Rehydrate(g.ID(), g.Info(), g.Copies(), t0, t0)
				read.RestoreEditions(g.Covers(), g.MainSystem())
				assert.Equal(t, map[string]EditionCover{"PS5": {URL: url}}, read.Covers())
			})
		})
	})
}
