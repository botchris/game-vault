package csvfile

import (
	"strings"
	"testing"
	"time"

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
	in := "title,platform,kind,status,cdKey,deadline,source,purchaseDate,edition,condition,location,steamAppId,notes,externalId\n" +
		"Hades,Steam,digital,owned,,,Steam,,,,,1145360,,steam:1145360\n" +
		"Celeste,Steam,key,unrevealed,,2026-10-19,Humble – Indie Bundle,2023-05-01,,,,,,humble:AAA:1\n"

	copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
	if err != nil || len(warnings) != 0 || len(copies) != 2 {
		t.Fatalf("copies=%v warnings=%v err=%v", copies, warnings, err)
	}

	lib, key := copies[0], copies[1]
	if lib.Details.Kind != game.KindLibrary || lib.ExternalID != "steam:1145360" || lib.SteamAppID != 1145360 {
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
