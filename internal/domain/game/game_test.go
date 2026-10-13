package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestMatchKey(t *testing.T) {
	cases := map[string]string{
		"Celeste™ (Steam Key)":            "celeste",
		"Slay the Spire - Deluxe Edition": "slay the spire",
		"Pokémon & Friends":               "pokemon and friends",
		"Celeste Steam Key":               "celeste",
		"Hades - GOG Key":                 "hades",
		"Control Epic Games Store Key":    "control",
		"Forza Horizon 5 Xbox One Key":    "forza horizon 5",
		"Steamworld Dig":                  "steamworld dig",
		"Epic Mickey":                     "epic mickey",
		"Steam Prison":                    "steam prison",
	}
	for in, want := range cases {
		assert.Equal(t, want, MatchKey(in), in)
	}
}

func TestCopyInvariants(t *testing.T) {
	g, err := New("Halo 3", t0)
	require.NoError(t, err)

	_, err = g.AddCopy(CopyDetails{
		Kind:   KindPhysical,
		Status: StatusRedeemed,
	}, t0)
	assert.Error(t, err, "a physical copy cannot be redeemed")

	c, err := g.AddCopy(CopyDetails{
		Kind:     KindPhysical,
		Platform: "xbox 360",
		Key:      "SHOULD-DROP",
	}, t0)
	require.NoError(t, err)
	assert.Equal(t, StatusOwned, c.Status)
	assert.Equal(t, "Xbox 360", c.Platform)
	assert.Empty(t, c.Key, "a physical copy has no key")
}

func TestRedundantKeys(t *testing.T) {
	g, err := New("Hades", t0)
	require.NoError(t, err)

	key, err := g.AddCopy(CopyDetails{
		Kind:     KindKey,
		Platform: "Steam",
		Status:   StatusRevealed,
		Key:      "K",
	}, t0)
	require.NoError(t, err)
	assert.False(t, g.IsRedundant(key), "not redundant without a library copy")

	_, err = g.AddCopy(CopyDetails{
		Kind:     KindLibrary,
		Platform: "steam",
	}, t0)
	require.NoError(t, err)
	assert.True(t, g.IsRedundant(g.Copies()[0]), "redundant once the game is in the Steam library")

	assert.Equal(t, 1, g.MarkRedundantKeysRedeemed(t0))
	assert.Equal(t, StatusRedeemed, g.Copies()[0].Status)
}

func TestAbsorb(t *testing.T) {
	a, _ := New("Witcher 3", t0)
	a.AddCopy(CopyDetails{
		Kind:     KindLibrary,
		Platform: "GOG",
	}, t0)
	b, _ := New("The Witcher 3: Wild Hunt", t0)
	b.UpdateInfo(Info{
		Title: b.Title(),
		Links: Links{LinkSteam: "292030"},
		Notes: "GOTY",
	}, t0)
	b.AddCopy(CopyDetails{
		Kind:     KindPhysical,
		Platform: "PS4",
	}, t0)
	a.Absorb(b, t0)

	assert.Len(t, a.Copies(), 2)
	assert.Empty(t, b.Copies())
	assert.Equal(t, "292030", a.Links()[LinkSteam])
	assert.Equal(t, "GOTY", a.Notes())
}

func TestParseBarcode(t *testing.T) {
	ok := map[string]Barcode{
		"2 001234 567893": "2001234567893", // an invented EAN-13 (prefix 200: in-store numbers)
		"3307215643006":   "3307215643006",
		"885370201215":    "0885370201215", // UPC-A is stored as EAN-13
		"":                "",
	}
	for in, want := range ok {
		got, err := ParseBarcode(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"2001234567894", "12345", "20012345678A3"} {
		_, err := ParseBarcode(bad)
		assert.Error(t, err, bad)
	}

	g, err := New("Example West", t0)
	require.NoError(t, err)

	_, err = g.AddCopy(CopyDetails{
		Kind:     KindPhysical,
		Platform: "Xbox 360",
		Barcode:  "2001234567893",
	}, t0)
	require.NoError(t, err)

	key, err := g.AddCopy(CopyDetails{
		Kind:     KindKey,
		Platform: "Steam",
		Barcode:  "2001234567893",
	}, t0)
	require.NoError(t, err)
	assert.Empty(t, key.Barcode, "only physical copies keep a barcode")

	_, found := g.CopyWithBarcode("2001234567893")
	assert.True(t, found, "CopyWithBarcode should find the disc")
}

func TestGame_CopyWithBarcode_cannotChangeTheGame(t *testing.T) {
	t.Run("GIVEN a disc with a barcode and a photo", func(t *testing.T) {
		g, err := New("Example West", t0)
		require.NoError(t, err)

		disc, err := g.AddCopy(CopyDetails{
			Kind:     KindPhysical,
			Platform: "Xbox 360",
			Barcode:  "2001234567893",
		}, t0)
		require.NoError(t, err)

		_, err = g.AddPhotos(disc.ID, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)

		t.Run("WHEN the copy found by its barcode is changed", func(t *testing.T) {
			c, ok := g.CopyWithBarcode("2001234567893")
			require.True(t, ok)

			c.Photos[0].Caption = "changed behind the game's back"

			t.Run("THEN the game's copy is unchanged", func(t *testing.T) {
				assert.Empty(t, g.Copies()[0].Photos[0].Caption)
			})
		})
	})
}

func TestGame_playStatusAndRating(t *testing.T) {
	t.Run("GIVEN a game without play status or rating", func(t *testing.T) {
		g, err := New("Hollow Knight", t0)
		require.NoError(t, err)

		t.Run("WHEN the user marks it as playing with four stars", func(t *testing.T) {
			changed, err := g.UpdateInfo(Info{
				Title:      g.Title(),
				PlayStatus: PlayPlaying,
				Rating:     4,
			}, t0)
			require.NoError(t, err)

			t.Run("THEN both are kept and the cover is untouched", func(t *testing.T) {
				assert.Equal(t, PlayPlaying, g.PlayStatus())
				assert.Equal(t, Rating(4), g.Rating())
				assert.False(t, changed)
			})
		})

		t.Run("WHEN the status or the rating is out of range", func(t *testing.T) {
			_, badStatus := g.UpdateInfo(Info{
				Title:      g.Title(),
				PlayStatus: "wishlist",
			}, t0)
			_, badRating := g.UpdateInfo(Info{
				Title:  g.Title(),
				Rating: MaxRating + 1,
			}, t0)

			t.Run("THEN the change is refused and the game keeps its values", func(t *testing.T) {
				var verr *errkind.ValidationError
				require.ErrorAs(t, badStatus, &verr)
				require.ErrorAs(t, badRating, &verr)
				assert.Equal(t, PlayPlaying, g.PlayStatus())
				assert.Equal(t, Rating(4), g.Rating())
			})
		})
	})

	t.Run("GIVEN two games being merged", func(t *testing.T) {
		a, _ := New("Celeste", t0)
		b, _ := New("Celeste (2018)", t0)
		_, err := b.UpdateInfo(Info{
			Title:      b.Title(),
			PlayStatus: PlayFinished,
			Rating:     5,
		}, t0)
		require.NoError(t, err)

		t.Run("WHEN the kept game has neither", func(t *testing.T) {
			a.Absorb(b, t0)

			t.Run("THEN it takes the absorbed game's", func(t *testing.T) {
				assert.Equal(t, PlayFinished, a.PlayStatus())
				assert.Equal(t, Rating(5), a.Rating())
			})
		})

		t.Run("WHEN the kept game already has its own", func(t *testing.T) {
			c, _ := New("Celeste", t0)
			_, err := c.UpdateInfo(Info{
				Title:      c.Title(),
				PlayStatus: PlayAbandoned,
				Rating:     2,
			}, t0)
			require.NoError(t, err)
			c.Absorb(b, t0)

			t.Run("THEN they win", func(t *testing.T) {
				assert.Equal(t, PlayAbandoned, c.PlayStatus())
				assert.Equal(t, Rating(2), c.Rating())
			})
		})
	})
}
