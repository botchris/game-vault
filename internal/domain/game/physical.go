package game

import (
	"slices"
	"strings"
)

// Grade is how good a physical copy looks, as collectors grade it. Empty means not stated.
type Grade string

// Values of Grade, best first.
const (
	GradeSealed     Grade = "sealed"
	GradeMint       Grade = "mint"
	GradeVeryGood   Grade = "very_good"
	GradeGood       Grade = "good"
	GradeAcceptable Grade = "acceptable"
	GradeDamaged    Grade = "damaged"
)

// Grades lists every grade, best first.
var Grades = []Grade{GradeSealed, GradeMint, GradeVeryGood, GradeGood, GradeAcceptable, GradeDamaged}

// Valid reports whether g is a known grade or empty.
func (g Grade) Valid() bool { return g == "" || slices.Contains(Grades, g) }

// Content is one part of what a physical copy comes with.
type Content string

// Values of Content, in the order they are listed.
const (
	ContentBox    Content = "box"
	ContentManual Content = "manual"
	ContentMedia  Content = "media"  // the disc or cartridge
	ContentExtras Content = "extras" // map, poster, figure, art book…
)

// AllContents lists every content, in the order they are listed.
var AllContents = []Content{ContentBox, ContentManual, ContentMedia, ContentExtras}

// Contents is the set of parts a physical copy comes with. It is a bit set, so it has no
// duplicates, always lists in the same order and compares with ==. Zero means not stated.
type Contents uint8

// ContentsOf builds the set from its parts, refusing unknown ones.
func ContentsOf(cs ...Content) (Contents, error) {
	var out Contents

	for _, c := range cs {
		i := slices.Index(AllContents, c)
		if i < 0 {
			return 0, invalid("content %q is not one of box, manual, media, extras", c)
		}

		out |= 1 << i
	}

	return out, nil
}

// Has reports whether the set holds c.
func (c Contents) Has(x Content) bool {
	i := slices.Index(AllContents, x)
	return i >= 0 && c&(1<<i) != 0
}

// List returns the parts in the fixed order.
func (c Contents) List() []Content {
	var out []Content

	for _, x := range AllContents {
		if c.Has(x) {
			out = append(out, x)
		}
	}

	return out
}

// valid reports whether c holds only known parts.
func (c Contents) valid() bool { return c < 1<<len(AllContents) }

// Money is an amount of an ISO 4217 currency, in that currency's minor unit (cents for EUR and
// USD, yen for JPY, fils for BHD): see CurrencyDigits. The zero value means no price.
type Money struct {
	Amount   int64
	Currency string
}

// IsZero reports whether there is no price.
func (m Money) IsZero() bool { return m.Amount == 0 }

func (m Money) normalize() (Money, error) {
	m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))

	switch {
	case m.Amount < 0:
		return Money{}, invalid("a price cannot be negative")
	case m.Amount == 0:
		return Money{}, nil
	case !isCurrencyCode(m.Currency):
		return Money{}, invalid("a price needs its currency as a three-letter code (EUR, USD…)")
	}

	return m, nil
}

// IsCurrencyCode reports whether s looks like an ISO 4217 code: three letters A–Z.
func IsCurrencyCode(s string) bool { return isCurrencyCode(s) }

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}

// currencyDigits lists the ISO 4217 currencies whose minor unit is not two digits.
var currencyDigits = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0,
	"RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

// CurrencyDigits returns how many decimals a currency has (2 unless ISO 4217 says otherwise).
func CurrencyDigits(code string) int {
	if d, ok := currencyDigits[strings.ToUpper(code)]; ok {
		return d
	}

	return 2
}
