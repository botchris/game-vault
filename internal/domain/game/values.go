package game

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ID identifies games and copies. IDs are UUIDv7, so they sort by creation time.
type ID string

// NewID returns a fresh identifier.
func NewID() ID { return ID(uuid.Must(uuid.NewV7()).String()) }

// Date is a calendar date formatted as YYYY-MM-DD. The zero value means "no date".
type Date string

// ParseDate validates s as a YYYY-MM-DD date. An empty string is a valid empty Date.
func ParseDate(s string) (Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}

	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return "", invalid("date %q must be YYYY-MM-DD", s)
	}

	return Date(s), nil
}

// DateOf converts a time to a Date.
func DateOf(t time.Time) Date { return Date(t.Format(time.DateOnly)) }

// Barcode is a GTIN printed on a box: EAN-13 (European games), UPC-A (US games) or EAN-8.
// UPC-A codes are stored as EAN-13 (with a leading 0) so both spellings compare equal.
type Barcode string

// ParseBarcode validates a barcode, ignoring spaces and dashes ("2 001234 567893").
// An empty string is a valid empty Barcode.
func ParseBarcode(s string) (Barcode, error) {
	digits := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-':
			return -1
		}

		return 'x'
	}, strings.TrimSpace(s))
	if digits == "" {
		return "", nil
	}

	if strings.Contains(digits, "x") || (len(digits) != 8 && len(digits) != 12 && len(digits) != 13) {
		return "", invalid("barcode %q must have 8, 12 or 13 digits", s)
	}

	if !gtinChecksumOK(digits) {
		return "", invalid("barcode %q has a wrong check digit; check it was read correctly", s)
	}

	if len(digits) == 12 {
		digits = "0" + digits
	}

	return Barcode(digits), nil
}

// gtinChecksumOK verifies the last digit: weights 3,1,3,1... from the right, excluding the check digit.
func gtinChecksumOK(d string) bool {
	sum := 0

	for i := len(d) - 2; i >= 0; i-- {
		n := int(d[i] - '0')
		if (len(d)-2-i)%2 == 0 {
			n *= 3
		}

		sum += n
	}

	return (10-sum%10)%10 == int(d[len(d)-1]-'0')
}

// Errors returned by the domain. Adapters map them to transport-specific codes.
var (
	ErrGameNotFound = errors.New("game not found")
	ErrCopyNotFound = errors.New("copy not found")

	// ErrPhotoNotFound means the copy has no photo with that id.
	ErrPhotoNotFound = errors.New("photo not found")

	// ErrInvalidBarcode wraps barcode input errors that are not ValidationErrors.
	ErrInvalidBarcode = errors.New("invalid barcode")
)

// ValidationError reports input that breaks a domain rule.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

var knownPlatforms = []string{
	// Stores and launchers
	"Steam", "Epic Games", "GOG", "EA App", "Battle.net", "Ubisoft Connect", "Microsoft Store / Xbox",
	"Rockstar", "Battlestate (Tarkov)", "Riot", "itch.io", "Amazon Games", "PlayStation Store", "Nintendo eShop",
	// Consoles and physical formats
	"PC", "PS5", "PS4", "PS3", "PS2", "PS1", "PSP", "PS Vita", "Xbox Series", "Xbox One", "Xbox 360", "Xbox",
	"Switch", "Wii U", "Wii", "GameCube", "N64", "3DS", "DS", "Game Boy",
}

// IsKnownPlatform reports whether p is one of the stores, launchers or consoles Game Vault knows,
// as opposed to whatever a source names a key type (a software vendor, a course site…).
func IsKnownPlatform(p string) bool {
	return slices.ContainsFunc(knownPlatforms, func(k string) bool { return strings.EqualFold(k, strings.TrimSpace(p)) })
}

// KnownPlatforms returns the stores, launchers and consoles Game Vault knows, PC and console names
// included (a copy of the list: callers cannot change it). The web's platform tables are checked
// against it.
func KnownPlatforms() []string { return slices.Clone(knownPlatforms) }

// CanonicalPlatform fixes the casing of known platforms ("steam" → "Steam") so they compare equal.
// Unknown platforms are returned trimmed but otherwise untouched.
func CanonicalPlatform(p string) string {
	p = strings.TrimSpace(p)
	for _, k := range knownPlatforms {
		if strings.EqualFold(k, p) {
			return k
		}
	}

	return p
}

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
