package game

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVocabulary_valuesNeverChange pins every platform, system and link key. They are stored in game
// documents, CSV files, cover and photo file names and links: renaming one orphans what was saved
// under the old name and stops Humble keys from being flagged as owned in that store.
func TestVocabulary_valuesNeverChange(t *testing.T) {
	for _, tc := range []struct {
		constant string
		want     string
	}{
		{PlatformSteam, "Steam"},
		{PlatformEpic, "Epic Games"},
		{PlatformGOG, "GOG"},
		{PlatformEA, "EA App"},
		{PlatformBattlenet, "Battle.net"},
		{PlatformUbisoft, "Ubisoft Connect"},
		{PlatformMicrosoft, "Microsoft Store / Xbox"},
		{PlatformRockstar, "Rockstar"},
		{PlatformBattlestate, "Battlestate (Tarkov)"},
		{PlatformRiot, "Riot"},
		{PlatformItch, "itch.io"},
		{PlatformAmazon, "Amazon Games"},
		{PlatformPlayStation, "PlayStation Store"},
		{PlatformNintendo, "Nintendo eShop"},

		{SystemPC, "PC"},
		{SystemPS5, "PS5"},
		{SystemPS4, "PS4"},
		{SystemPS3, "PS3"},
		{SystemPS2, "PS2"},
		{SystemPS1, "PS1"},
		{SystemPSP, "PSP"},
		{SystemPSVita, "PS Vita"},
		{SystemXboxSeries, "Xbox Series"},
		{SystemXboxOne, "Xbox One"},
		{SystemXbox360, "Xbox 360"},
		{SystemXbox, "Xbox"},
		{SystemSwitch, "Switch"},
		{SystemWiiU, "Wii U"},
		{SystemWii, "Wii"},
		{SystemGameCube, "GameCube"},
		{SystemN64, "N64"},
		{System3DS, "3DS"},
		{SystemDS, "DS"},
		{SystemGameBoy, "Game Boy"},
		{SystemOther, "Other"},

		{LinkSteam, "steam"},
		{LinkEpic, "epic"},
		{LinkGOG, "gog"},
		{LinkEA, "ea"},
		{LinkBattlenet, "battlenet"},
		{LinkUbisoft, "ubisoft"},
		{LinkXbox, "xbox"},
		{LinkAmazon, "amazon"},
		{LinkItch, "itch"},
		{LinkPSN, "psn"},
	} {
		assert.Equal(t, tc.want, tc.constant)
	}
}

func TestKnownPlatforms(t *testing.T) {
	t.Run("GIVEN the platforms Game Vault knows", func(t *testing.T) {
		t.Run("THEN they are the stores, in a fixed order, then every catalog system", func(t *testing.T) {
			want := append(make([]string, 0, 14+len(Systems())),
				"Steam", "Epic Games", "GOG", "EA App", "Battle.net", "Ubisoft Connect", "Microsoft Store / Xbox",
				"Rockstar", "Battlestate (Tarkov)", "Riot", "itch.io", "Amazon Games", "PlayStation Store", "Nintendo eShop",
			)

			for _, s := range Systems() {
				want = append(want, s.Name)
			}

			assert.Equal(t, want, KnownPlatforms())
		})

		t.Run("THEN the consoles are the catalog's systems but PC, in catalog order", func(t *testing.T) {
			want := make([]string, 0, len(Systems()))

			for _, s := range Systems()[1:] {
				want = append(want, s.Name)
			}

			assert.Equal(t, want, Consoles())

			for _, c := range Consoles() {
				assert.Equal(t, c, SystemOf(c))
			}
		})

		t.Run("WHEN a caller changes the lists it was given THEN the domain's are unchanged", func(t *testing.T) {
			KnownPlatforms()[0] = "Stean"
			Consoles()[0] = "PS6"

			assert.True(t, IsKnownPlatform(PlatformSteam))
			assert.False(t, IsKnownPlatform("Stean"))
			assert.Equal(t, SystemPS5, Consoles()[0])
		})
	})
}

func TestStoreLinks(t *testing.T) {
	assert.Equal(t, []string{"steam", "epic", "gog", "ea", "battlenet", "ubisoft", "xbox", "itch", "amazon", "psn"}, StoreLinks())
}

func TestIsPCStoreLink(t *testing.T) {
	for _, key := range []string{"steam", "gog", "epic", "ea", "battlenet", "ubisoft", "xbox", "amazon", "itch"} {
		assert.True(t, IsPCStoreLink(key), key)
	}

	for _, key := range []string{"psn", "igdb", "", "Steam"} {
		assert.False(t, IsPCStoreLink(key), key)
	}
}

// TestIsXboxSystem checks that the Xbox consoles agree, on every system Game Vault knows, with the
// "Xbox" prefix BorrowsPCBox matched before (the Microsoft Store cover provider had this list).
func TestIsXboxSystem(t *testing.T) {
	for _, p := range KnownPlatforms() {
		s := SystemOf(p)
		assert.Equal(t, strings.HasPrefix(s, "Xbox"), IsXboxSystem(s), s)
	}

	assert.Equal(t, []string{"Xbox Series", "Xbox One", "Xbox 360", "Xbox"},
		slices.DeleteFunc(Consoles(), func(c string) bool { return !IsXboxSystem(c) }))
}
