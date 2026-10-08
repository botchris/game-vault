package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func key(ext, title string, appID int64, status Status) ImportedCopy {
	return ImportedCopy{ExternalID: ext, Title: title, SteamAppID: appID,
		Details: CopyDetails{Kind: KindKey, Platform: "Steam", Status: status, Key: "KEY-" + ext}}
}

func TestConsolidatorMatchesAndDedupes(t *testing.T) {
	// The Steam library already has Hades and Celeste.
	lib := NewConsolidator(nil).Apply("steam-src", []ImportedCopy{
		{ExternalID: "steam:1145360", Title: "Hades", SteamAppID: 1145360, Details: CopyDetails{Kind: KindLibrary, Platform: "Steam"}},
		{ExternalID: "steam:504230", Title: "Celeste", SteamAppID: 504230, Details: CopyDetails{Kind: KindLibrary, Platform: "Steam"}},
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
	g.UpdateCopy(g.Copies()[0].ID, CopyDetails{Kind: KindKey, Platform: "Steam", Status: StatusRedeemed, Key: "KEY-humble:B:0"}, t0)

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
