package game

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// This file holds how titles are compared and searched for: MatchKey, which consolidation merges
// games by, SameTitle and SameMatchKey, which providers match search results with, and the search
// terms sent to game databases.

var (
	reTrademarks = regexp.MustCompile(`[™®©]`)
	reBrackets   = regexp.MustCompile(`\(.*?\)|\[.*?\]`)
	reStoreKey   = regexp.MustCompile(`\b(` + storeNames + `)\s*(cd\s*)?key\b`)
	reSuffixes   = regexp.MustCompile(`\b(key|pc|digital|game of the year|goty|definitive|complete|deluxe|ultimate|enhanced|remastered|director'?s cut|anniversary|gold|edition)\b`)
	reNonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
)

// stripMarks returns a transformer that removes accents ("Pokémon" → "Pokemon"). A chain keeps
// buffers between calls and is not safe for concurrent use, and titles are matched in parallel
// (the media service's lookups), so each call builds its own.
func stripMarks() transform.Transformer {
	return transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
}

// storeNames are the stores a key can be for, as sellers write them in titles ("Celeste Steam Key",
// "Hades - GOG Key"). Only "<store> key" is dropped: a bare store name can be part of the title
// ("Epic Mickey", "Switch Force").
const storeNames = `steam|gog(\.com)?|epic(\s*games)?(\s*store)?|ubisoft(\s*connect)?|uplay|ea(\s*app)?|origin|` +
	`battle\.?\s*net|rockstar(\s*games)?|microsoft(\s*store)?|xbox(\s*(one|series\s*[xs](\s*\|\s*[xs])?|live))?|` +
	`windows|playstation(\s*[345])?|psn|ps[345]|nintendo(\s*(switch|eshop))?|switch|itch(\.io)?|amazon(\s*games)?`

// Case-insensitive versions of MatchKey's tags, for SearchTitle, which keeps the title's casing.
var (
	reStoreKeyAnyCase = regexp.MustCompile(`(?i)` + reStoreKey.String())
	reSuffixesAnyCase = regexp.MustCompile(`(?i)` + reSuffixes.String())

	// reDangling matches separators left at either end once tags are gone ("Mafia: ", "Game - ").
	reDangling = regexp.MustCompile(`^[\s:;,\-–—|/]+|[\s:;,\-–—|/]+$`)

	// reSeparatorRun matches two or more separators in a row left inside a title ("Foo - : The Bar").
	reSeparatorRun = regexp.MustCompile(`\s*[:;,\-–—|/](\s*[:;,\-–—|/])+\s*`)
)

// NormalizeTitle drops trademark signs and repeated spaces, which stores add or leave out: two
// titles that differ only in those are the same text to search for.
func NormalizeTitle(title string) string {
	return strings.Join(strings.Fields(reTrademarks.ReplaceAllString(title, "")), " ")
}

// reYearSuffix matches a year in parentheses at the end of a title, with the spaces around it.
var reYearSuffix = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

// StripYearSuffix drops a year in parentheses at the end of a title ("Example Quest Remastered
// (2017)" → "Example Quest Remastered"), which stores add to tell a remake or a remaster from the
// original while other stores name it without. Nothing else changes, spaces at the start included.
func StripYearSuffix(title string) string {
	return reYearSuffix.ReplaceAllString(title, "")
}

// SearchTitle returns the title to search a game database for: the title without the edition and
// store tags MatchKey ignores ("Example West GOTY" → "Example West", "Example Game (PC
// Key)" → "Example Game"), its other words, numbers and casing kept. Databases name the game, not
// the edition a copy is, so a search for the raw title often finds nothing. A title made only of tags
// is returned as it is.
func SearchTitle(title string) string {
	s := reTrademarks.ReplaceAllString(title, "")
	s = reBrackets.ReplaceAllString(s, " ")
	s = reStoreKeyAnyCase.ReplaceAllString(s, " ")
	s = reSuffixesAnyCase.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = reSeparatorRun.ReplaceAllString(s, ": ")
	s = reDangling.ReplaceAllString(s, "")

	if s == "" {
		return NormalizeTitle(title)
	}

	return s
}

// SearchTerms returns the names to search a game database for, in order: the title without its
// edition and store tags (SearchTitle), since databases name the game and not the edition a copy is
// ("Example West", not the "GOTY" disc), then the raw title (NormalizeTitle) for when stripping
// went too far ("Gold Rush!" became "Rush!"). The raw title is left out when it is the same name
// (a ™ alone, or casing, is no other title), and so are empty names. Every provider that searches by
// title uses these terms, so an edition title is found the same way everywhere.
func SearchTerms(title string) []string {
	var terms []string

	for _, t := range []string{SearchTitle(title), NormalizeTitle(title)} {
		if t != "" && !slices.ContainsFunc(terms, func(x string) bool { return strings.EqualFold(x, t) }) {
			terms = append(terms, t)
		}
	}

	return terms
}

// reNotLetterOrDigit matches what separates the words SameTitle compares.
var reNotLetterOrDigit = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// letterFolds maps the letters that do not decompose into a base letter and an accent.
var letterFolds = strings.NewReplacer("ø", "o", "æ", "ae", "œ", "oe", "ß", "ss", "ł", "l", "đ", "d", "ð", "d", "þ", "th", "ı", "i")

// SameTitle reports whether two titles name the same game exactly: the same words in the same
// order, ignoring case, accents, trademark signs, punctuation and spacing ("&" counts as "and").
// "Example - A Wild Adventure" is "Example: A Wild Adventure", but every word and number counts
// and words are not glued together: "Example Remastered" is not "Example", "Doom (2016)" is not
// "Doom", "Re-Volt" is not "Revolt". Titles without any word are never the same. Providers use it
// to match a search result to a game; MatchKey is the looser comparison that also ignores edition
// and store tags.
func SameTitle(a, b string) bool {
	wa, wb := titleWords(a), titleWords(b)

	return len(wa) > 0 && slices.Equal(wa, wb)
}

// SameMatchKey reports whether two titles have the same MatchKey, the loose comparison that also
// ignores edition and store tags ("Example Quest - Deluxe Edition" is "Example Quest"). Titles
// whose key is empty are never the same: MatchKey keeps only Latin letters and digits, so every
// title in another script (Japanese, Cyrillic, Greek…) reduces to "", and two different games
// would otherwise match. Providers compare with SameTitle first, which handles every script.
func SameMatchKey(a, b string) bool {
	k := MatchKey(a)

	return k != "" && k == MatchKey(b)
}

// TitleKey returns the words SameTitle compares, joined by single spaces: two titles are the same
// for SameTitle exactly when their keys are equal and not empty. Lookups index titles by it instead
// of comparing every title with every other.
func TitleKey(title string) string { return strings.Join(titleWords(title), " ") }

// titleWords returns the words SameTitle compares: lower case, without accents, split on anything
// but letters and digits.
func titleWords(title string) []string {
	s := strings.ToLower(reTrademarks.ReplaceAllString(title, ""))
	if t, _, err := transform.String(stripMarks(), s); err == nil {
		s = t
	}

	s = strings.ReplaceAll(letterFolds.Replace(s), "&", " and ")

	return strings.Fields(reNotLetterOrDigit.ReplaceAllString(s, " "))
}

// MatchKey reduces a title to a comparable form, e.g. "Celeste™ (Steam Key)" → "celeste" and
// "Hades GOG Key" → "hades": the store a key is for is not part of the game's name.
// Two titles with the same match key are treated as the same game when consolidating.
func MatchKey(title string) string {
	s := reTrademarks.ReplaceAllString(title, "")
	if t, _, err := transform.String(stripMarks(), s); err == nil {
		s = t
	}

	s = strings.ToLower(s)
	s = reBrackets.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = reStoreKey.ReplaceAllString(s, " ")
	s = reSuffixes.ReplaceAllString(s, " ")
	s = reNonAlnum.ReplaceAllString(s, " ")

	return strings.Join(strings.Fields(s), " ")
}
