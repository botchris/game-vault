package game

import "testing"

func TestParseProductName(t *testing.T) {
	cases := []struct {
		raw      string
		title    string
		platform string
		edition  string
	}{
		// Real UPCitemdb answers for PAL (Spanish) boxes.
		{"Assassin's Creed Iii Ed. Special Ps3(sp)", "Assassin's Creed III", "PS3", "Special Edition"},
		{"Call Of Duty Modern Warfare 2 Ps3(sp)", "Call Of Duty Modern Warfare 2", "PS3", ""},
		{"Gears of War 3  Microsoft  Xbox 360  885370201215", "Gears of War 3", "Xbox 360", ""},
		{"Halo 3 (Xbox 360) PAL", "Halo 3", "Xbox 360", ""},
		// Real EAN-Search answer for Dead Space 3, Xbox 360 PAL.
		{"DEAD SPACE 3 X360", "Dead Space 3", "Xbox 360", ""},
		{"ASSASSINS CREED III PS3", "Assassins Creed III", "PS3", ""},
	}
	for _, c := range cases {
		title, platform, edition := ParseProductName(c.raw)
		if title != c.title || platform != c.platform || edition != c.edition {
			t.Errorf("ParseProductName(%q) = (%q, %q, %q), want (%q, %q, %q)", c.raw, title, platform, edition, c.title, c.platform, c.edition)
		}
	}
}
