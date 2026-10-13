package game

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"gamevault/internal/domain/errkind"
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

// storeDateLayouts are the ways stores write a date or a timestamp: ISO with or without a zone
// and fractions of a second (Humble's orders, itch.io, Fanatical), with a space for a T (itch.io's
// older endpoints), and in English words (Humble's release dates).
var storeDateLayouts = []string{
	"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", time.RFC3339, time.DateOnly, time.DateTime,
	"January 2, 2006", "January 2 2006", "Jan 2, 2006", "Jan 2 2006",
}

// ParseStoreDate returns the day of a date or timestamp a store wrote, in the zone it was written
// in, or "" when it is in none of the store formats Game Vault knows.
func ParseStoreDate(s string) Date {
	s = strings.TrimSpace(s)
	for _, layout := range storeDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return DateOf(t)
		}
	}

	return ""
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

// Errors returned by the domain. Their kind (errkind) tells adapters which status to answer with.
var (
	// ErrGameNotFound means there is no game with that id.
	ErrGameNotFound = errkind.New(errkind.NotFound, "game not found")

	// ErrCopyNotFound means the game has no copy with that id.
	ErrCopyNotFound = errkind.New(errkind.NotFound, "copy not found")

	// ErrPhotoNotFound means the copy has no photo with that id.
	ErrPhotoNotFound = errkind.New(errkind.NotFound, "photo not found")

	// ErrEditionNotFound means the game has no copy on the system asked for: the page is stale.
	// Like an unknown id, it is NotFound.
	ErrEditionNotFound = errkind.New(errkind.NotFound, "this game has no copy on that system; reload the page")
)

func invalid(format string, args ...any) error { return errkind.Invalidf(format, args...) }
