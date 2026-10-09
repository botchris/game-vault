package game

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestMatchKey(t *testing.T) {
	cases := map[string]string{
		"Celeste™ (Steam Key)":            "celeste",
		"Slay the Spire - Deluxe Edition": "slay the spire",
		"Pokémon & Friends":               "pokemon and friends",
	}
	for in, want := range cases {
		if got := MatchKey(in); got != want {
			t.Errorf("MatchKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCopyInvariants(t *testing.T) {
	g, _ := New("Halo 3", t0)
	if _, err := g.AddCopy(CopyDetails{Kind: KindPhysical, Status: StatusRedeemed}, t0); err == nil {
		t.Error("a physical copy cannot be redeemed")
	}

	c, err := g.AddCopy(CopyDetails{Kind: KindPhysical, Platform: "xbox 360", Key: "SHOULD-DROP"}, t0)
	if err != nil {
		t.Fatal(err)
	}

	if c.Status != StatusOwned || c.Platform != "Xbox 360" || c.Key != "" {
		t.Errorf("defaults not applied: %+v", c)
	}
}

func TestRedundantKeys(t *testing.T) {
	g, _ := New("Hades", t0)

	key, _ := g.AddCopy(CopyDetails{Kind: KindKey, Platform: "Steam", Status: StatusRevealed, Key: "K"}, t0)
	if g.IsRedundant(key) {
		t.Fatal("not redundant without a library copy")
	}

	g.AddCopy(CopyDetails{Kind: KindLibrary, Platform: "steam"}, t0)

	if !g.IsRedundant(g.Copies()[0]) {
		t.Fatal("expected redundant once the game is in the Steam library")
	}

	if n := g.MarkRedundantKeysRedeemed(t0); n != 1 || g.Copies()[0].Status != StatusRedeemed {
		t.Fatalf("expected the key to be redeemed, got n=%d %+v", n, g.Copies()[0])
	}
}

func TestAbsorb(t *testing.T) {
	a, _ := New("Witcher 3", t0)
	a.AddCopy(CopyDetails{Kind: KindLibrary, Platform: "GOG"}, t0)
	b, _ := New("The Witcher 3: Wild Hunt", t0)
	b.UpdateInfo(Info{Title: b.Title(), Links: Links{LinkSteam: "292030"}, Notes: "GOTY"}, t0)
	b.AddCopy(CopyDetails{Kind: KindPhysical, Platform: "PS4"}, t0)
	a.Absorb(b, t0)

	if len(a.Copies()) != 2 || len(b.Copies()) != 0 || a.Links()[LinkSteam] != "292030" || a.Notes() != "GOTY" {
		t.Fatalf("unexpected merge result: copies=%d links=%v notes=%q", len(a.Copies()), a.Links(), a.Notes())
	}
}

func TestUpdateInfoCover(t *testing.T) {
	g, _ := New("Halo 3", t0)
	if _, err := g.UpdateInfo(Info{Title: "Halo 3", CoverURL: "javascript:alert(1)"}, t0); err == nil {
		t.Error("non-http cover urls must be rejected")
	}

	changed, err := g.UpdateInfo(Info{Title: "Halo 3", CoverURL: "https://example.com/halo.jpg"}, t0)
	if err != nil || !changed {
		t.Fatalf("cover change not reported: %v %v", changed, err)
	}

	if changed, _ := g.UpdateInfo(Info{Title: "Halo 3 ", CoverURL: "https://example.com/halo.jpg", Notes: "x"}, t0); changed {
		t.Error("only title/notes changed, the cover did not")
	}
}

func TestParseBarcode(t *testing.T) {
	ok := map[string]Barcode{
		"5 026555 255042": "5026555255042", // Red Dead Redemption, Xbox 360 PAL
		"3307215643006":   "3307215643006",
		"885370201215":    "0885370201215", // UPC-A is stored as EAN-13
		"":                "",
	}
	for in, want := range ok {
		if got, err := ParseBarcode(in); err != nil || got != want {
			t.Errorf("ParseBarcode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	for _, bad := range []string{"5026555255043", "12345", "50265552550A2"} {
		if _, err := ParseBarcode(bad); err == nil {
			t.Errorf("ParseBarcode(%q) should fail", bad)
		}
	}

	g, _ := New("Red Dead Redemption", t0)
	g.AddCopy(CopyDetails{Kind: KindPhysical, Platform: "Xbox 360", Barcode: "5026555255042"}, t0)

	key, _ := g.AddCopy(CopyDetails{Kind: KindKey, Platform: "Steam", Barcode: "5026555255042"}, t0)
	if key.Barcode != "" {
		t.Error("only physical copies keep a barcode")
	}

	if _, ok := g.CopyWithBarcode("5026555255042"); !ok {
		t.Error("CopyWithBarcode should find the disc")
	}
}
