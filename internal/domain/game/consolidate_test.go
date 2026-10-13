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
	require.Equal(t, 2, lib.GamesCreated, "library import")
	require.Equal(t, 2, lib.CopiesAdded, "library import")

	c := NewConsolidator(lib.Changed)

	res := c.Apply("humble-src", []ImportedCopy{
		key("humble:A:0", "Hades", 1145360, StatusRevealed),            // matched by app id
		key("humble:A:1", "Celeste™ (Steam Key)", 0, StatusUnrevealed), // matched by title
		key("humble:A:2", "Brand New Game", 0, StatusUnrevealed),       // new game
	}, t0)
	require.Equal(t, 1, res.GamesCreated, "humble import")
	require.Equal(t, 3, res.CopiesAdded, "humble import")

	for _, g := range res.Changed {
		if g.Title() == "Hades" {
			require.Len(t, g.Copies(), 2, "Hades holds the library copy and the key")
			assert.True(t, g.IsRedundant(g.Copies()[1]), "the key is redundant")
		}
	}

	// Re-scanning is idempotent.
	again := NewConsolidator(append(lib.Changed, res.Changed[2])).Apply("humble-src", []ImportedCopy{
		key("humble:A:0", "Hades", 1145360, StatusRevealed),
	}, t0)
	assert.Equal(t, 1, again.CopiesUnchanged, "re-scan")
	assert.Zero(t, again.CopiesAdded, "re-scan should not add copies")
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
	assert.Equal(t, 1, again.CopiesUnchanged)
	assert.Equal(t, StatusRedeemed, g.Copies()[0].Status, "a redeemed key must stay redeemed")
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
			again := NewConsolidator(first.Changed).Apply("", []ImportedCopy{psn("")}, t0)

			t.Run("THEN the override is kept, and the second import leaves the copy unchanged", func(t *testing.T) {
				assert.Equal(t, 1, set.CopiesUpdated)
				assert.Equal(t, "PS5", first.Changed[0].Copies()[0].CopyDetails.System)
				assert.Equal(t, 1, again.CopiesUnchanged)
				assert.Zero(t, again.CopiesUpdated)
				assert.Empty(t, again.Changed)
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
				read := Rehydrate(Snapshot{
					ID:         g.ID(),
					Info:       g.Info(),
					Copies:     g.Copies(),
					Covers:     g.Covers(),
					MainSystem: g.MainSystem(),
					CreatedAt:  t0,
					UpdatedAt:  t0,
				})
				assert.Equal(t, map[string]EditionCover{"PS5": {URL: url}}, read.Covers())
			})
		})
	})
}

// reimport imports one copy with details d from a file, matched by its external id.
func reimport(games []*Game, d CopyDetails) ConsolidationResult {
	return NewConsolidator(games).Apply("", []ImportedCopy{{
		ExternalID: "file:1",
		Title:      "Portal",
		Details:    d,
	}}, t0)
}

func TestConsolidator_kindChangeDropsTheOldKindsFields(t *testing.T) {
	t.Run("library to key and back drops the key", func(t *testing.T) {
		g := reimport(nil, CopyDetails{
			Kind:     KindLibrary,
			Platform: "Steam",
		}).Changed[0]

		reimport([]*Game{g}, CopyDetails{
			Kind:     KindKey,
			Platform: "Steam",
			Status:   StatusRevealed,
			Key:      "AAAA-BBBB-CCCC",
			RedeemBy: "2030-01-31",
		})
		require.Equal(t, "AAAA-BBBB-CCCC", g.Copies()[0].Key)

		reimport([]*Game{g}, CopyDetails{
			Kind:     KindLibrary,
			Platform: "Steam",
		})

		c := g.Copies()[0]
		assert.Equal(t, KindLibrary, c.Kind)
		assert.Equal(t, StatusOwned, c.Status)
		assert.Empty(t, c.Key)
		assert.Empty(t, c.RedeemBy)
		assert.False(t, c.IsPendingKey())
	})

	t.Run("physical to library drops the barcode and keeps what the user wrote", func(t *testing.T) {
		g := reimport(nil, CopyDetails{
			Kind:     KindPhysical,
			Platform: "PS4",
			Barcode:  "4006381333931",
			Grade:    GradeMint,
			Contents: 1,
			Location: "Shelf 2",
			Notes:    "Signed by a friend",
			System:   "PS5",
			Price: Money{
				Amount:   2000,
				Currency: "EUR",
			},
		}).Changed[0]
		id := g.Copies()[0].ID
		require.NoError(t, g.SetCopyFields(id, FieldValues{"shelf": {Text: "top"}}, t0))

		_, found := g.CopyWithBarcode("4006381333931")
		require.True(t, found)

		reimport([]*Game{g}, CopyDetails{
			Kind:     KindLibrary,
			Platform: "PlayStation Store",
		})

		c := g.Copies()[0]
		assert.Equal(t, KindLibrary, c.Kind)
		assert.Empty(t, c.Barcode)
		assert.Empty(t, c.Grade)
		assert.Zero(t, c.Contents)
		assert.Empty(t, c.Location)

		_, found = g.CopyWithBarcode("4006381333931")
		assert.False(t, found, "a barcode scan must not find a copy that is no longer physical")

		assert.Equal(t, "Signed by a friend", c.Notes)
		assert.Equal(t, "PS5", c.CopyDetails.System)
		assert.Equal(t, "top", c.Fields["shelf"].Text)
		assert.Equal(t, Money{
			Amount:   2000,
			Currency: "EUR",
		}, c.Price)
	})
}
