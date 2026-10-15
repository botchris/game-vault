package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
)

// gameWith returns a game titled "Halo 3" with one copy per details.
func gameWith(t *testing.T, copies ...CopyDetails) *Game {
	t.Helper()

	g, err := New("Halo 3", t0)
	require.NoError(t, err)

	for _, d := range copies {
		_, err := g.AddCopy(d, t0)
		require.NoError(t, err)
	}

	return g
}

func on(kind Kind, platform string) CopyDetails {
	return CopyDetails{
		Kind:     kind,
		Platform: platform,
	}
}

func systemsOf(editions []Edition) []string {
	out := make([]string, 0, len(editions))
	for _, e := range editions {
		out = append(out, e.System)
	}

	return out
}

func TestEditions(t *testing.T) {
	t.Run("GIVEN a game owned only in PC stores (Steam, a Humble Steam key, GOG)", func(t *testing.T) {
		g := gameWith(t, on(KindLibrary, "Steam"), on(KindKey, "Steam"), on(KindLibrary, "GOG"))

		t.Run("THEN it has exactly one edition, PC, and it is the main one", func(t *testing.T) {
			assert.Equal(t, []Edition{{
				System: SystemPC,
				Main:   true,
			}}, g.Editions())
			assert.Equal(t, SystemPC, g.MainEdition().System)
		})
	})

	t.Run("GIVEN copies on PC, PS3 (a disc) and Xbox 360 (a key)", func(t *testing.T) {
		g := gameWith(t, on(KindLibrary, "Steam"), on(KindLibrary, "GOG"), on(KindKey, "Xbox 360"), on(KindPhysical, "PS3"))

		t.Run("THEN the edition with a disc is the main one, first, then the rest by system", func(t *testing.T) {
			assert.Equal(t, []string{"PS3", "PC", "Xbox 360"}, systemsOf(g.Editions()))
			assert.True(t, g.Editions()[0].Main)
			assert.False(t, g.Editions()[1].Main)
		})

		t.Run("WHEN the user makes Xbox 360 the main edition", func(t *testing.T) {
			require.NoError(t, g.SetMainSystem("Xbox 360", t0))

			t.Run("THEN it comes first", func(t *testing.T) {
				assert.Equal(t, []string{"Xbox 360", "PC", "PS3"}, systemsOf(g.Editions()))
				assert.Equal(t, "Xbox 360", g.MainSystem())
			})

			t.Run("AND an empty choice goes back to the default rule", func(t *testing.T) {
				require.NoError(t, g.SetMainSystem("", t0))
				assert.Equal(t, "PS3", g.MainEdition().System)
			})
		})

		t.Run("WHEN a system the game has no copy on is chosen THEN it is refused, naming it", func(t *testing.T) {
			err := g.SetMainSystem("Wii", t0)

			require.ErrorIs(t, err, ErrEditionNotFound)
			assert.Contains(t, err.Error(), "Wii")
		})
	})

	t.Run("GIVEN no physical copy", func(t *testing.T) {
		t.Run("THEN the edition with the most copies is the main one", func(t *testing.T) {
			g := gameWith(t, on(KindKey, "Switch"), on(KindLibrary, "Steam"), on(KindKey, "Steam"))
			assert.Equal(t, SystemPC, g.MainEdition().System)
		})

		t.Run("AND on a tie, the first by system", func(t *testing.T) {
			g := gameWith(t, on(KindKey, "Switch"), on(KindLibrary, "Steam"))
			assert.Equal(t, SystemPC, g.MainEdition().System)
		})
	})

	t.Run("GIVEN two editions with discs THEN the one with more copies is the main one", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "Xbox 360"), on(KindKey, "Xbox 360"))
		assert.Equal(t, "Xbox 360", g.MainEdition().System)
	})

	t.Run("GIVEN a game without copies THEN it has no editions", func(t *testing.T) {
		g := gameWith(t)
		assert.Empty(t, g.Editions())
		assert.Equal(t, Edition{}, g.MainEdition())
	})
}

func TestEditionCovers(t *testing.T) {
	t.Run("GIVEN a PS3 disc with a photo and an Xbox 360 disc", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "Xbox 360"))
		ps3 := g.Copies()[0].ID
		_, err := g.AddPhotos(ps3, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)

		t.Run("WHEN the PS3 photo is chosen for the Xbox 360 edition THEN it is refused, naming the system", func(t *testing.T) {
			err := g.SetEditionCover("Xbox 360", EditionCover{Photo: pid(1)}, t0)

			var ve *errkind.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, err.Error(), "Xbox 360")
		})

		t.Run("WHEN a cover is chosen for a system the game has no copy on THEN it is refused, naming it", func(t *testing.T) {
			err := g.SetEditionCover("Wii", EditionCover{URL: "https://example.test/wii.jpg"}, t0)

			require.ErrorIs(t, err, ErrEditionNotFound)
			assert.Equal(t, errkind.NotFound, errkind.Of(err), "a stale page, like an unknown id")
			assert.Contains(t, err.Error(), "Wii")

			require.ErrorIs(t, g.SetMainSystem("Wii", t0), ErrEditionNotFound)
		})

		t.Run("WHEN a URL that is not http(s) is chosen THEN it is refused", func(t *testing.T) {
			var ve *errkind.ValidationError
			assert.ErrorAs(t, g.SetEditionCover("PS3", EditionCover{URL: "javascript:alert(1)"}, t0), &ve)
		})

		t.Run("WHEN each edition gets its own cover", func(t *testing.T) {
			require.NoError(t, g.SetEditionCover("PS3", EditionCover{Photo: pid(1)}, t0))
			require.NoError(t, g.SetEditionCover("Xbox 360", EditionCover{URL: " https://example.test/x360.jpg "}, t0))

			t.Run("THEN each edition shows its own", func(t *testing.T) {
				p, _ := g.Edition("PS3")
				x, _ := g.Edition("Xbox 360")

				assert.Equal(t, EditionCover{Photo: pid(1)}, p.Cover)
				assert.Equal(t, EditionCover{URL: "https://example.test/x360.jpg"}, x.Cover)
			})

			t.Run("AND a zero cover lets the providers choose again", func(t *testing.T) {
				require.NoError(t, g.SetEditionCover("Xbox 360", EditionCover{}, t0))

				x, _ := g.Edition("Xbox 360")
				assert.True(t, x.Cover.IsZero())
			})
		})

		t.Run("WHEN the user moves the PS3 disc to PS4", func(t *testing.T) {
			d := g.Copies()[0].CopyDetails
			d.System = "PS4"
			_, err := g.UpdateCopy(ps3, d, t0)
			require.NoError(t, err)

			t.Run("THEN the PS3 edition and its photo cover are gone, and the photo is not the PS4 cover", func(t *testing.T) {
				assert.Equal(t, []string{"PS4", "Xbox 360"}, systemsOf(g.Editions()))
				assert.Empty(t, g.Covers())
			})
		})
	})

	t.Run("GIVEN the user made the PS3 edition, with a chosen cover, the main one", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindLibrary, "Steam"), on(KindLibrary, "GOG"))
		require.NoError(t, g.SetEditionCover("PS3", EditionCover{URL: "https://example.test/ps3.jpg"}, t0))
		require.NoError(t, g.SetMainSystem("PS3", t0))

		t.Run("WHEN an override moves its only copy to PC", func(t *testing.T) {
			d := g.Copies()[0].CopyDetails
			d.System = "PC"
			_, err := g.UpdateCopy(g.Copies()[0].ID, d, t0)
			require.NoError(t, err)

			t.Run("THEN the main choice is cleared, its cover dropped, and PC is the main edition", func(t *testing.T) {
				assert.Empty(t, g.MainSystem())
				assert.Empty(t, g.Covers())
				assert.Equal(t, []Edition{{
					System: SystemPC,
					Main:   true,
				}}, g.Editions())
			})
		})
	})

	t.Run("GIVEN a cover photo two discs of the edition share", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "PS3"))
		a, b := g.Copies()[0].ID, g.Copies()[1].ID
		_, err := g.AddPhotos(a, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)
		_, err = g.AddPhotos(b, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)
		require.NoError(t, g.SetEditionCover("PS3", EditionCover{Photo: pid(1)}, t0))

		t.Run("WHEN it is removed from one disc THEN it stays, since the other disc has it", func(t *testing.T) {
			_, err := g.RemovePhoto(a, pid(1), t0)
			require.NoError(t, err)
			assert.Equal(t, pid(1), g.Covers()["PS3"].Photo)
		})

		t.Run("WHEN the disc that still has it is removed THEN the cover is dropped", func(t *testing.T) {
			_, err := g.RemoveCopy(b, t0)
			require.NoError(t, err)
			assert.Empty(t, g.Covers())
		})
	})
}

func TestEditions_absorb(t *testing.T) {
	t.Run("GIVEN a game with a chosen PS3 cover, and another with PS3 and Xbox 360 covers and a main system", func(t *testing.T) {
		kept := gameWith(t, on(KindPhysical, "PS3"))
		require.NoError(t, kept.SetEditionCover("PS3", EditionCover{URL: "https://example.test/kept.jpg"}, t0))

		other := gameWith(t, on(KindPhysical, "PS3"), on(KindPhysical, "Xbox 360"))
		require.NoError(t, other.SetEditionCover("PS3", EditionCover{URL: "https://example.test/lost.jpg"}, t0))
		require.NoError(t, other.SetEditionCover("Xbox 360", EditionCover{URL: "https://example.test/x360.jpg"}, t0))
		require.NoError(t, other.SetMainSystem("Xbox 360", t0))

		t.Run("WHEN the first absorbs the second", func(t *testing.T) {
			kept.Absorb(other, t0)

			t.Run("THEN the editions follow the copies, the kept game's cover wins and the other's fill the gaps", func(t *testing.T) {
				assert.Equal(t, []string{"Xbox 360", "PS3"}, systemsOf(kept.Editions()))
				assert.Equal(t, map[string]EditionCover{
					"PS3":      {URL: "https://example.test/kept.jpg"},
					"Xbox 360": {URL: "https://example.test/x360.jpg"},
				}, kept.Covers())
			})

			t.Run("AND the other's main system fills the kept game's empty choice", func(t *testing.T) {
				assert.Equal(t, "Xbox 360", kept.MainSystem())
			})
		})
	})

	t.Run("GIVEN both games chose a main system WHEN one absorbs the other THEN the kept game's choice wins", func(t *testing.T) {
		kept := gameWith(t, on(KindPhysical, "PS3"))
		require.NoError(t, kept.SetMainSystem("PS3", t0))
		other := gameWith(t, on(KindPhysical, "Xbox 360"))
		require.NoError(t, other.SetMainSystem("Xbox 360", t0))

		kept.Absorb(other, t0)
		assert.Equal(t, "PS3", kept.MainSystem())
	})
}

func TestCoverFingerprints(t *testing.T) {
	t.Run("GIVEN a PS3 disc and a Steam key", func(t *testing.T) {
		g := gameWith(t, on(KindPhysical, "PS3"), on(KindKey, "Steam"))
		key := g.Copies()[1]

		t.Run("WHEN the key is revealed THEN no edition changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			d := key.CopyDetails
			d.Status = StatusRevealed
			_, err := g.UpdateCopy(key.ID, d, t0)
			require.NoError(t, err)
			assert.Empty(t, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN a GOG copy is added THEN only PC changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			_, err := g.AddCopy(on(KindLibrary, "GOG"), t0)
			require.NoError(t, err)
			assert.Equal(t, []string{SystemPC}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN a Wii disc is added THEN the new Wii edition changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			_, err := g.AddCopy(on(KindPhysical, "Wii"), t0)
			require.NoError(t, err)
			assert.Equal(t, []string{"Wii"}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN the PS3 cover is chosen THEN only PS3 changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			require.NoError(t, g.SetEditionCover("PS3", EditionCover{URL: "https://example.test/ps3.jpg"}, t0))
			assert.Equal(t, []string{"PS3"}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN the store links change THEN every edition changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			info := g.Info()
			info.Links = Links{LinkSteam: "620"}
			_, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)
			assert.Equal(t, []string{"PC", "PS3", "Wii"}, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN a second Steam key is added THEN no edition changed: the cover inputs are the same", func(t *testing.T) {
			before := g.CoverFingerprints()
			_, err := g.AddCopy(on(KindKey, "Steam"), t0)
			require.NoError(t, err)
			assert.Empty(t, ChangedSystems(before, g.CoverFingerprints()))
		})

		t.Run("WHEN the Wii disc moves to PS3, which has a disc already, THEN only Wii changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			wii := g.Copies()[3]
			d := wii.CopyDetails
			d.Platform = "PS3"
			_, err := g.UpdateCopy(wii.ID, d, t0)
			require.NoError(t, err)
			assert.Equal(t, []string{"Wii"}, ChangedSystems(before, g.CoverFingerprints()))
		})
	})

	t.Run("GIVEN a game without copies with a chosen cover", func(t *testing.T) {
		g := gameWith(t)
		require.NoError(t, g.SetEditionCover("", EditionCover{URL: "https://example.test/halo.jpg"}, t0))

		t.Run("WHEN its first copy, a PS3 disc, is added THEN the empty system and PS3 changed", func(t *testing.T) {
			before := g.CoverFingerprints()
			_, err := g.AddCopy(on(KindPhysical, "PS3"), t0)
			require.NoError(t, err)
			assert.Equal(t, []string{"", "PS3"}, ChangedSystems(before, g.CoverFingerprints()))
		})
	})

	t.Run("GIVEN a game without copies WHEN its cover is chosen THEN the empty system changed", func(t *testing.T) {
		g := gameWith(t)
		before := g.CoverFingerprints()
		require.NoError(t, g.SetEditionCover("", EditionCover{URL: "https://example.test/halo.jpg"}, t0))
		assert.Equal(t, []string{""}, ChangedSystems(before, g.CoverFingerprints()))
	})
}

func TestCoverWithoutCopies(t *testing.T) {
	const url = "https://example.test/halo.jpg"

	// coverOnly returns a game without copies whose cover is url.
	coverOnly := func(t *testing.T) *Game {
		t.Helper()

		g := gameWith(t)
		require.NoError(t, g.SetEditionCover("", EditionCover{URL: url}, t0))

		return g
	}

	t.Run("GIVEN a game without copies with a chosen cover", func(t *testing.T) {
		g := coverOnly(t)

		t.Run("THEN it is the main cover, and the game still has no editions", func(t *testing.T) {
			assert.Equal(t, EditionCover{URL: url}, g.MainEdition().Cover)
			assert.Empty(t, g.Editions())
		})

		t.Run("WHEN its first copy, a PS3 disc, is added THEN the cover moves to the PS3 edition", func(t *testing.T) {
			_, err := g.AddCopy(on(KindPhysical, "PS3"), t0)
			require.NoError(t, err)
			assert.Equal(t, map[string]EditionCover{"PS3": {URL: url}}, g.Covers())

			p, _ := g.Edition("PS3")
			assert.Equal(t, EditionCover{URL: url}, p.Cover)
		})

		t.Run("AND the empty system is refused once the game has copies", func(t *testing.T) {
			var ve *errkind.ValidationError
			assert.ErrorAs(t, g.SetEditionCover("", EditionCover{URL: url}, t0), &ve)
		})
	})

	t.Run("GIVEN a game without copies with a cover, and another with a PS3 disc", func(t *testing.T) {
		t.Run("WHEN the PS3 edition has no cover THEN it takes the first game's", func(t *testing.T) {
			kept := coverOnly(t)
			kept.Absorb(gameWith(t, on(KindPhysical, "PS3")), t0)
			assert.Equal(t, map[string]EditionCover{"PS3": {URL: url}}, kept.Covers())
		})

		t.Run("WHEN the PS3 edition already has a cover THEN it keeps it and the other is dropped", func(t *testing.T) {
			kept := coverOnly(t)
			other := gameWith(t, on(KindPhysical, "PS3"))
			require.NoError(t, other.SetEditionCover("PS3", EditionCover{URL: "https://example.test/ps3.jpg"}, t0))

			kept.Absorb(other, t0)
			assert.Equal(t, map[string]EditionCover{"PS3": {URL: "https://example.test/ps3.jpg"}}, kept.Covers())
		})
	})
}

func TestPCBox(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a PS4-only game linked to the PlayStation store and Steam", func(t *testing.T) {
		g, err := New("Console Quest", t0)
		require.NoError(t, err)
		_, err = g.AddCopy(on(KindLibrary, "PS4"), t0)
		require.NoError(t, err)

		info := g.Info()
		info.Links = Links{
			"psn":     "PPSA0001",
			LinkSteam: "100",
		}
		_, err = g.UpdateInfo(info, t0)
		require.NoError(t, err)

		t.Run("THEN it has a PC-store link and borrows a PC box, with its own cover inputs", func(t *testing.T) {
			assert.True(t, g.Links().HasPCStore())
			assert.True(t, g.BorrowsPCBox())
			assert.Contains(t, g.CoverFingerprints(), SystemPC)
		})

		t.Run("WHEN the Steam link changes THEN the PC box changed too", func(t *testing.T) {
			before := g.CoverFingerprints()
			info := g.Info()
			info.Links = Links{
				"psn":     "PPSA0001",
				LinkSteam: "1",
			}
			_, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)
			assert.Contains(t, ChangedSystems(before, g.CoverFingerprints()), SystemPC)
		})

		t.Run("WHEN the Steam link goes THEN there is no PC box any more, which counts as a change", func(t *testing.T) {
			before := g.CoverFingerprints()
			info := g.Info()
			info.Links = Links{"psn": "PPSA0001"}
			_, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)

			assert.False(t, g.BorrowsPCBox())
			assert.NotContains(t, g.CoverFingerprints(), SystemPC)
			assert.Contains(t, ChangedSystems(before, g.CoverFingerprints()), SystemPC)
		})
	})

	t.Run("GIVEN a game with a PC edition, and one without copies", func(t *testing.T) {
		pc, err := New("Celeste", t0)
		require.NoError(t, err)
		_, err = pc.AddCopy(on(KindLibrary, "Steam"), t0)
		require.NoError(t, err)

		bare, err := New("Bare Quest", t0)
		require.NoError(t, err)

		for _, g := range []*Game{pc, bare} {
			info := g.Info()
			info.Links = Links{LinkSteam: "504230"}
			_, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)
		}

		t.Run("THEN neither borrows a PC box: one has its own, the other only its game cover", func(t *testing.T) {
			assert.False(t, pc.BorrowsPCBox())
			assert.False(t, bare.BorrowsPCBox())
		})
	})

	t.Run("GIVEN an Xbox 360 game linked to the Microsoft Store only", func(t *testing.T) {
		g, err := New("Console Quest", t0)
		require.NoError(t, err)
		_, err = g.AddCopy(on(KindPhysical, "Xbox 360"), t0)
		require.NoError(t, err)

		info := g.Info()
		info.Links = Links{"xbox": "9N0000000001"}
		_, err = g.UpdateInfo(info, t0)
		require.NoError(t, err)

		t.Run("THEN it borrows no PC box: that store's art is the Xbox edition's own, not a PC box", func(t *testing.T) {
			assert.False(t, g.BorrowsPCBox())
			assert.NotContains(t, g.CoverFingerprints(), SystemPC)
		})

		t.Run("WHEN it is also linked to Steam THEN it does", func(t *testing.T) {
			info := g.Info()
			info.Links = Links{
				"xbox":    "9N0000000001",
				LinkSteam: "100",
			}
			_, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)
			assert.True(t, g.BorrowsPCBox())
		})
	})

	t.Run("GIVEN links of console stores only", func(t *testing.T) {
		assert.False(t, Links{"psn": "1", "nintendo": "2"}.HasPCStore())
		assert.True(t, IsPCStoreLink("gog"))
		assert.False(t, IsPCStoreLink("psn"))
	})
}

func TestEditions_followARenamedSystem(t *testing.T) {
	disc := func(platform string) Copy {
		return Copy{
			ID: NewID(),
			CopyDetails: CopyDetails{
				Kind:     KindPhysical,
				Platform: platform,
			},
			Photos: []Photo{{ID: pid(1)}},
		}
	}

	t.Run("GIVEN a stored game whose disc platform was typed in lowercase before the catalog knew it", func(t *testing.T) {
		g := Rehydrate(Snapshot{
			ID:         NewID(),
			Info:       Info{Title: "Sonic Adventure"},
			Copies:     []Copy{disc("dreamcast"), disc("PS2")},
			Covers:     map[string]EditionCover{"dreamcast": {Photo: pid(1)}},
			MainSystem: "dreamcast",
		})

		t.Run("WHEN it is loaded and the catalog fixes the casing of its system", func(t *testing.T) {
			require.Equal(t, []string{"Dreamcast", "PS2"}, g.Systems())

			t.Run("THEN the photo cover chosen for it follows the edition", func(t *testing.T) {
				assert.Equal(t, map[string]EditionCover{"Dreamcast": {Photo: pid(1)}}, g.Covers())
			})

			t.Run("AND it is still the main edition", func(t *testing.T) {
				assert.Equal(t, "Dreamcast", g.MainSystem())
			})
		})
	})

	t.Run("GIVEN a stored game whose disc platform is a catalog alias", func(t *testing.T) {
		d := disc("Mega Drive")
		g := Rehydrate(Snapshot{
			ID:         NewID(),
			Info:       Info{Title: "Sonic the Hedgehog"},
			Copies:     []Copy{d, disc("PS2")},
			Covers:     map[string]EditionCover{"Mega Drive": {URL: "https://example.test/md.jpg"}},
			MainSystem: "Mega Drive",
		})
		require.Equal(t, []string{"Mega Drive", "PS2"}, g.Systems(), "an alias is kept until the copy is saved")

		t.Run("WHEN the copy is saved and its platform becomes the catalog name", func(t *testing.T) {
			_, err := g.UpdateCopy(d.ID, d.CopyDetails, t0)
			require.NoError(t, err)
			require.Equal(t, []string{"Mega Drive / Genesis", "PS2"}, g.Systems())

			t.Run("THEN the chosen cover follows the edition", func(t *testing.T) {
				assert.Equal(t, map[string]EditionCover{"Mega Drive / Genesis": {URL: "https://example.test/md.jpg"}}, g.Covers())
			})

			t.Run("AND it is still the main edition", func(t *testing.T) {
				assert.Equal(t, "Mega Drive / Genesis", g.MainSystem())
			})
		})
	})

	t.Run("GIVEN a renamed system whose new name already has a cover of its own", func(t *testing.T) {
		g := Rehydrate(Snapshot{
			ID:     NewID(),
			Info:   Info{Title: "Sonic Adventure"},
			Copies: []Copy{disc("dreamcast"), disc("Dreamcast")},
			Covers: map[string]EditionCover{
				"dreamcast": {URL: "https://example.test/old.jpg"},
				"Dreamcast": {URL: "https://example.test/new.jpg"},
			},
		})

		t.Run("WHEN it is loaded THEN the edition keeps its own cover", func(t *testing.T) {
			assert.Equal(t, map[string]EditionCover{"Dreamcast": {URL: "https://example.test/new.jpg"}}, g.Covers())
		})
	})
}
