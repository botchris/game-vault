package game

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// The patterns of TitleIDKey: a copy of MatchKey's as they were on 2026-10-13. They are its own so
// that improving MatchKey (a new store, a new edition word) never changes an id. Never edit them.
var (
	idTrademarks = regexp.MustCompile(`[™®©]`)
	idBrackets   = regexp.MustCompile(`\(.*?\)|\[.*?\]`)
	idStoreKey   = regexp.MustCompile(`\b(` + idStoreNames + `)\s*(cd\s*)?key\b`)
	idSuffixes   = regexp.MustCompile(`\b(key|pc|digital|game of the year|goty|definitive|complete|deluxe|ultimate|enhanced|remastered|director'?s cut|anniversary|gold|edition)\b`)
	idNonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
)

// idStoreNames is storeNames as it was on 2026-10-13. Never edit it.
const idStoreNames = `steam|gog(\.com)?|epic(\s*games)?(\s*store)?|ubisoft(\s*connect)?|uplay|ea(\s*app)?|origin|` +
	`battle\.?\s*net|rockstar(\s*games)?|microsoft(\s*store)?|xbox(\s*(one|series\s*[xs](\s*\|\s*[xs])?|live))?|` +
	`windows|playstation(\s*[345])?|psn|ps[345]|nintendo(\s*(switch|eshop))?|switch|itch(\.io)?|amazon(\s*games)?`

// TitleIDKey derives a stable id from a title, for a source whose store gives an item no id of its
// own (Battle.net, EA): the copy's ExternalID is built from it. It reduces the title the way
// MatchKey did on 2026-10-13 ("Celeste™ (Steam Key)" → "celeste") and, unlike MatchKey, is frozen:
// an ExternalID never changes format, or the next scan imports the copy again. Use MatchKey to
// compare titles; use this only for ids. TestTitleIDKey_neverChanges pins its output.
func TitleIDKey(title string) string {
	s := idTrademarks.ReplaceAllString(title, "")

	// Accents go as in stripMarks, spelled out here so that a change there leaves ids alone.
	marks := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	if t, _, err := transform.String(marks, s); err == nil {
		s = t
	}

	s = strings.ToLower(s)
	s = idBrackets.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = idStoreKey.ReplaceAllString(s, " ")
	s = idSuffixes.ReplaceAllString(s, " ")
	s = idNonAlnum.ReplaceAllString(s, " ")

	return strings.Join(strings.Fields(s), " ")
}
