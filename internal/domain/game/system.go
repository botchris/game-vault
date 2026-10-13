package game

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxSystemLen is the longest system name, in characters.
const maxSystemLen = 40

// SystemOf returns the system a copy on this platform is played on: PC for PC stores and
// launchers, the console itself for a console, the most likely console for a console's store, the
// platform itself (trimmed) when Game Vault does not know it, and Other when there is none.
func SystemOf(platform string) string {
	if k, ok := platformNamed(platform); ok {
		return k.system // also fixes the casing of console names
	}

	if p := strings.TrimSpace(platform); p != "" {
		return p
	}

	return SystemOther
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
