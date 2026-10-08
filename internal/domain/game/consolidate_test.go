package game

import "testing"

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
