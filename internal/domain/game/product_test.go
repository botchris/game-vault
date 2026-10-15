package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
		// Systems the regexes did not know, and names that are ordinary words.
		{"Nintendo Switch 2 Edition The Legend of Zelda Tears of the Kingdom", "The Legend of Zelda Tears of the Kingdom", "Switch 2", ""},
		{"Mario Kart World (Switch 2)", "Mario Kart World", "Switch 2", ""},
		{"Mario Kart 8 Deluxe Switch PAL", "Mario Kart 8 Deluxe", "Switch", ""},
		{"Crash Bandicoot PlayStation 1", "Crash Bandicoot", "PS1", ""},
		{"Metal Gear Solid PS1", "Metal Gear Solid", "PS1", ""},
		{"Pokemon Yellow Game Boy Color", "Pokemon Yellow", "Game Boy Color", ""},
		{"Pokemon Ruby Game Boy Advance PAL", "Pokemon Ruby", "Game Boy Advance", ""},
		{"Tetris Game Boy", "Tetris", "Game Boy", ""},
		{"Sonic the Hedgehog 2 Sega Mega Drive", "Sonic the Hedgehog 2", "Mega Drive / Genesis", ""},
		{"Super Mario World SNES PAL", "Super Mario World", "SNES", ""},
		{"Genesis Noir PS4", "Genesis Noir", "PS4", ""},
		{"Wii Sports Wii", "Wii Sports", "Wii", ""},
		{"Saturn Bomberman (Saturn)", "Saturn Bomberman", "Saturn", ""},
		{"Mario Kart DS", "Mario Kart", "DS", ""},
		{"Ridge Racer PSP", "Ridge Racer", "PSP", ""},
		{"Super Mario 64 N64", "Super Mario 64", "N64", ""},
		{"Halo Combat Evolved Xbox", "Halo Combat Evolved", "Xbox", ""},
		{"The Sims PC CD-ROM", "The Sims", "PC", ""},
		{"Playstation4 Games", "Games", "PS4", ""},
	}
	for _, c := range cases {
		title, platform, edition := ParseProductName(c.raw)
		assert.Equal(t, []string{c.title, c.platform, c.edition}, []string{title, platform, edition}, c.raw)
	}
}
