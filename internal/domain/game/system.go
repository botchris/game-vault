package game

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Systems a copy can be played on that are not console names (consoles are their own system).
const (
	// SystemPC is the system of every PC store and launcher.
	SystemPC = "PC"

	// SystemOther is the system of a copy without a platform.
	SystemOther = "Other"
)

// maxSystemLen is the longest system name, in characters.
const maxSystemLen = 40

// pcPlatforms are the stores and launchers whose games are played on a PC.
var pcPlatforms = []string{
	"Steam", "Epic Games", "GOG", "EA App", "Ubisoft Connect", "Battle.net", "itch.io", "Amazon Games",
	"Rockstar", "Riot", "Battlestate (Tarkov)", SystemPC,
}

// storeSystems maps the stores that sell games for several systems to the most likely one.
var storeSystems = map[string]string{
	"PlayStation Store":      "PS4",
	"Nintendo eShop":         "Switch",
	"Microsoft Store / Xbox": SystemPC,
}

// SystemOf returns the system a copy on this platform is played on: PC for PC stores and
// launchers, the console itself for a console, the most likely console for a console's store, the
// platform itself (trimmed) when Game Vault does not know it, and Other when there is none.
func SystemOf(platform string) string {
	p := CanonicalPlatform(platform) // also fixes the casing of console names
	if p == "" {
		return SystemOther
	}

	if slices.Contains(pcPlatforms, p) {
		return SystemPC
	}

	if s, ok := storeSystems[p]; ok {
		return s
	}

	return p
}

// normalizeSystem checks a system named by the user or a source and returns it the way SystemOf
// names it ("ps4" → "PS4", "Steam" → "PC"). Empty stays empty: automatic.
func normalizeSystem(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}

	if utf8.RuneCountInString(s) > maxSystemLen {
		return "", invalid("a system name has at most %d characters", maxSystemLen)
	}

	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", invalid("system %q contains control characters", s)
	}

	if !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return "", invalid("system %q needs at least one letter or digit", s)
	}

	return SystemOf(s), nil
}
