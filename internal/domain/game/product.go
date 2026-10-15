package game

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// systemToken is one name or alias of a catalog system as ParseProductName looks for it.
type systemToken struct {
	// re matches the alias as a whole word; its first group is the alias itself.
	re *regexp.Regexp

	// pattern is the alias's own regexp source (aliasPattern), without the word boundaries.
	pattern string

	// system is the catalog name the alias belongs to.
	system string

	// length is the length of the alias's key (SystemKey), the order tokens are tried in: the
	// pattern's own length would count the separators between parts.
	length int

	// weak is set for an alias that is one word of letters ("Genesis", "Switch", "Wii", "DS"): an
	// ordinary word in a title, so it only counts in brackets, at the end of the name, or written in
	// capitals when it has at most four letters ("DS", "SNES").
	weak bool
}

// systemTokens are every name and alias of the catalog, longest first ("Switch 2" before "Switch",
// "Game Boy Advance" before "Game Boy"). Names holding brackets ("Mac (classic)") are left out:
// their aliases are enough.
var systemTokens = func() []systemToken {
	var out []systemToken

	for _, s := range catalog {
		for _, w := range append([]string{s.Name}, s.Aliases...) {
			if strings.ContainsAny(w, "()[]") {
				continue
			}

			p := aliasPattern(w)
			out = append(out, systemToken{
				re:      regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(` + p + `)(?:$|[^\p{L}\p{N}])`),
				pattern: p,
				length:  len(SystemKey(w)),
				system:  s.Name,
				weak:    isWeakAlias(w),
			})
		}
	}

	slices.SortStableFunc(out, func(a, b systemToken) int { return b.length - a.length })

	return out
}()

// reAliasRuns splits an alias into runs of letters, runs of digits and single other characters.
var reAliasRuns = regexp.MustCompile(`\p{L}+|\p{N}+|[^\s.\-\p{L}\p{N}]`)

// aliasPattern is the regexp source of an alias whose parts may be written together or apart with
// spaces, hyphens or dots ("PS4", "PS 4", "PlayStation4", "Mega-Drive").
func aliasPattern(alias string) string {
	runs := reAliasRuns.FindAllString(alias, -1)
	for i, r := range runs {
		runs[i] = regexp.QuoteMeta(r)
	}

	return strings.Join(runs, `[\s.\-]*`)
}

// isWeakAlias reports whether an alias is a single word of letters (see systemToken.weak).
func isWeakAlias(alias string) bool {
	return alias != "" && !strings.ContainsFunc(alias, func(r rune) bool { return !unicode.IsLetter(r) })
}

// SystemPattern is a regexp source matching any catalog name or alias (case-insensitive by the
// caller's flags), longest first, for adapters that strip system words from titles. It is a bare
// alternation: callers wrap it in (?:...) before adding anything around it.
func SystemPattern() string {
	parts := make([]string, 0, len(systemTokens))
	for _, t := range systemTokens {
		parts = append(parts, t.pattern)
	}

	return strings.Join(parts, "|")
}

// matchIn returns the position of the first acceptable match of the token's alias in s, nil when
// there is none. A weak alias is acceptable inside brackets, at the end of s (nothing but
// punctuation after it), or in capitals when it has at most four letters.
func (t systemToken) matchIn(s string) []int {
	for _, m := range t.re.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[2], m[3]
		word := s[start:end]

		if !t.weak || inBrackets(s, start, end) || strings.TrimFunc(s[end:], isNotAlnum) == "" || isShortCaps(word) {
			return []int{start, end}
		}
	}

	return nil
}

// isShortCaps reports whether word is written in capitals and has at most four letters ("DS").
func isShortCaps(word string) bool {
	return len(word) <= 4 && word == strings.ToUpper(word)
}

// inBrackets reports whether s[start:end] is the only thing inside a pair of round or square brackets.
func inBrackets(s string, start, end int) bool {
	before := strings.TrimRight(s[:start], " ")
	after := strings.TrimLeft(s[end:], " ")

	return before != "" && after != "" && strings.ContainsRune("([", rune(before[len(before)-1])) &&
		strings.ContainsRune(")]", rune(after[0]))
}

// isNotAlnum reports whether r is neither a letter nor a digit.
func isNotAlnum(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsNumber(r)
}

var (
	reRegion       = regexp.MustCompile(`(?i)\((sp|es|uk|eu|fr|de|it|pal|ntsc|us|esp|ita)\)|\b(pal|ntsc|espa[ñn]ol|spanish|english)\b`)
	reNoise        = regexp.MustCompile(`(?i)\b(video\s*game|videojuego|juego|game|microsoft|sony|nintendo)\b`)
	reBracketChars = regexp.MustCompile(`[\[\](){}]`)
	reGTIN         = regexp.MustCompile(`\b\d{8,14}\b`) // barcodes echoed in the product name
	reSpaces       = regexp.MustCompile(`\s+`)
	reRoman        = regexp.MustCompile(`(?i)\b(i{1,3}|iv|vi{0,3}|ix|x)\b`)
	reEdition      = regexp.MustCompile(`(?i)\b(ed\.?|edici[oó]n|edition)\s+(special|especial|limitada|limited|collector'?s?|coleccionista|gold|goty|complete|completa)\b|\b(special|limited|collector'?s?|gold|goty|complete)\s+edition\b`)
)

// ParseProductName turns a retail product name such as "Assassin's Creed Iii Ed. Special Ps3(sp)"
// into a searchable game title, the platform and the edition it mentions:
// ("Assassin's Creed III", "PS3", "Special Edition").
func ParseProductName(raw string) (title, platform, edition string) {
	// Barcodes and region tags go first, so "Switch PAL" ends with its system.
	s := reGTIN.ReplaceAllString(raw, " ")
	s = reRegion.ReplaceAllString(s, " ")

	for _, t := range systemTokens {
		for {
			loc := t.matchIn(s)
			if loc == nil {
				break
			}

			if platform == "" {
				platform = t.system
			}

			s = s[:loc[0]] + " " + s[loc[1]:]
		}
	}

	if m := reEdition.FindString(s); m != "" {
		edition = editionName(m)
		s = strings.Replace(s, m, " ", 1)
	}

	s = reNoise.ReplaceAllString(s, " ")
	s = reBracketChars.ReplaceAllString(s, " ")

	s = strings.Trim(reSpaces.ReplaceAllString(s, " "), " -–:,")
	if s == strings.ToUpper(s) && strings.ToLower(s) != s {
		s = titleCase(s) // "DEAD SPACE 3" → "Dead Space 3"
	}
	// Retail databases often title-case roman numerals ("Iii"); restore them.
	s = reRoman.ReplaceAllStringFunc(s, strings.ToUpper)

	return s, platform, edition
}

// titleCase capitalises each word of an all-caps title.
func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		words[i] = string(r)
	}

	return strings.Join(words, " ")
}

func editionName(m string) string {
	l := strings.ToLower(m)
	switch {
	case strings.Contains(l, "goty"):
		return "GOTY"
	case strings.Contains(l, "collector") || strings.Contains(l, "coleccionista"):
		return "Collector's Edition"
	case strings.Contains(l, "limit"):
		return "Limited Edition"
	case strings.Contains(l, "gold"):
		return "Gold Edition"
	case strings.Contains(l, "complet"):
		return "Complete Edition"
	default:
		return "Special Edition"
	}
}
