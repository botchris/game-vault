package csvfile

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

func TestDecodeSpanishSemicolon(t *testing.T) {
	in := "titulo;plataforma;tipo;fecha_compra\nHalo 3;Xbox 360;físico;12/05/2008\nGears;Xbox 360;;\n"

	copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
	if err != nil || len(warnings) != 0 || len(copies) != 2 {
		t.Fatalf("copies=%v warnings=%v err=%v", copies, warnings, err)
	}

	if d := copies[0].Details; d.Kind != game.KindPhysical || d.AcquiredOn != "2008-05-12" {
		t.Errorf("unexpected %+v", d)
	}

	if copies[0].ExternalID == copies[1].ExternalID {
		t.Error("external ids must differ")
	}
}

// The export of the first Game Vault version must import without losing ids, so later scans
// of Humble and Steam update those copies instead of duplicating them.
func TestDecodeLegacyExport(t *testing.T) {
	in := "title,platform,kind,status,cdKey,deadline,source,purchaseDate,edition,condition,location,links,notes,externalId\n" +
		"Hades,Steam,digital,owned,,,Steam,,,,,steam:1145360,,steam:1145360\n" +
		"Celeste,Steam,key,unrevealed,,2026-10-19,Humble – Indie Bundle,2023-05-01,,,,,,humble:AAA:1\n"

	copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
	if err != nil || len(warnings) != 0 || len(copies) != 2 {
		t.Fatalf("copies=%v warnings=%v err=%v", copies, warnings, err)
	}

	lib, key := copies[0], copies[1]
	if lib.Details.Kind != game.KindLibrary || lib.ExternalID != "steam:1145360" || lib.Links[game.LinkSteam] != "1145360" {
		t.Errorf("library row: %+v", lib)
	}

	if key.Details.Kind != game.KindKey || key.Details.RedeemBy != "2026-10-19" || key.Details.Origin != "Humble – Indie Bundle" || key.ExternalID != "humble:AAA:1" {
		t.Errorf("key row: %+v", key)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	g, _ := game.New("Halo 3", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g.AddCopy(game.CopyDetails{Kind: game.KindPhysical, Platform: "Xbox 360", Location: "Shelf"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var b strings.Builder
	if err := (Codec{}).Encode(&b, []*game.Game{g}); err != nil {
		t.Fatal(err)
	}

	copies, _, err := Codec{}.Decode(strings.NewReader(b.String()))
	if err != nil || len(copies) != 1 || copies[0].Details.Location != "Shelf" || copies[0].Title != "Halo 3" {
		t.Fatalf("round trip failed: %+v %v", copies, err)
	}
}

func TestLinks_roundTrip(t *testing.T) {
	t.Run("GIVEN a game linked to two stores", func(t *testing.T) {
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		g, _ := game.New("Hades", now)
		_, err := g.UpdateInfo(game.Info{Title: "Hades", Links: game.Links{"steam": "1145360", "gog": "1207658924"}}, now)
		require.NoError(t, err)
		g.AddCopy(game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam"}, now)

		t.Run("WHEN it is exported and imported again", func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, Codec{}.Encode(&b, []*game.Game{g}))

			copies, warnings, err := Codec{}.Decode(strings.NewReader(b.String()))
			require.NoError(t, err)

			t.Run("THEN the links column lists both, sorted, and they come back", func(t *testing.T) {
				assert.Contains(t, b.String(), "gog:1207658924 steam:1145360")
				require.Len(t, copies, 1)
				assert.Empty(t, warnings)
				assert.Equal(t, game.Links{"steam": "1145360", "gog": "1207658924"}, copies[0].Links)
			})
		})
	})

	t.Run("GIVEN a links cell with a pair that is not store:id", func(t *testing.T) {
		in := "title,links\nHades,steam:1145360 1207658924\n"

		t.Run("WHEN it is imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)

			t.Run("THEN the valid link is kept and the other is reported", func(t *testing.T) {
				require.Len(t, copies, 1)
				assert.Equal(t, game.Links{"steam": "1145360"}, copies[0].Links)
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], `"1207658924"`)
			})
		})
	})
}
