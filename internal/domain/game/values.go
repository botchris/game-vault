package game

import (
	"errors"
	"fmt"
	"regexp"
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

// ParseBarcode validates a barcode, ignoring spaces and dashes ("5 026555 255042").
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
	reSuffixes   = regexp.MustCompile(`\b(steam\s*key|steam|key|pc|digital|game of the year|goty|definitive|complete|deluxe|ultimate|enhanced|remastered|director'?s cut|anniversary|gold|edition)\b`)
	reNonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
	stripMarks   = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
)

// MatchKey reduces a title to a comparable form, e.g. "Celeste™ (Steam Key)" → "celeste".
// Two titles with the same match key are treated as the same game when consolidating.
func MatchKey(title string) string {
	s := reTrademarks.ReplaceAllString(title, "")
	if t, _, err := transform.String(stripMarks, s); err == nil {
		s = t
	}
	s = strings.ToLower(s)
	s = reBrackets.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = reSuffixes.ReplaceAllString(s, " ")
	s = reNonAlnum.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}
