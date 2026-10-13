package game

import (
	"regexp"
	"strings"
)

// platformTokens recognizes platforms inside retail product names, longest patterns first.
var platformTokens = []struct {
	re       *regexp.Regexp
	platform string
}{
	{regexp.MustCompile(`(?i)\b(xbox\s*360|x360|xb360)\b`), SystemXbox360},
	{regexp.MustCompile(`(?i)\b(xbox\s*series\s*[xs](\s*\|\s*[xs])?)\b`), SystemXboxSeries},
	{regexp.MustCompile(`(?i)\b(xbox\s*one|xb1|xbone)\b`), SystemXboxOne},
	{regexp.MustCompile(`(?i)\b(playstation\s*5|ps5)\b`), SystemPS5},
	{regexp.MustCompile(`(?i)\b(playstation\s*4|ps4)\b`), SystemPS4},
	{regexp.MustCompile(`(?i)\b(playstation\s*3|ps3)\b`), SystemPS3},
	{regexp.MustCompile(`(?i)\b(playstation\s*2|ps2)\b`), SystemPS2},
	{regexp.MustCompile(`(?i)\b(ps\s*vita|psvita)\b`), SystemPSVita},
	{regexp.MustCompile(`(?i)\bpsp\b`), SystemPSP},
	{regexp.MustCompile(`(?i)\bwii\s*u\b`), SystemWiiU},
	{regexp.MustCompile(`(?i)\bwii\b`), SystemWii},
	{regexp.MustCompile(`(?i)\b(nintendo\s*)?switch\b`), SystemSwitch},
	{regexp.MustCompile(`(?i)\b(nintendo\s*)?3ds\b`), System3DS},
	{regexp.MustCompile(`(?i)\b(nintendo\s*)?ds\b`), SystemDS},
	{regexp.MustCompile(`(?i)\bgamecube\b`), SystemGameCube},
	{regexp.MustCompile(`(?i)\bxbox\b`), SystemXbox},
	{regexp.MustCompile(`(?i)\b(pc\s*(dvd|cd)?(-?rom)?|windows)\b`), SystemPC},
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
	s := raw
	for _, t := range platformTokens {
		if loc := t.re.FindStringIndex(s); loc != nil {
			if platform == "" {
				platform = t.platform
			}

			s = s[:loc[0]] + " " + s[loc[1]:]
		}
	}

	if m := reEdition.FindString(s); m != "" {
		edition = editionName(m)
		s = strings.Replace(s, m, " ", 1)
	}

	s = reRegion.ReplaceAllString(s, " ")
	s = reNoise.ReplaceAllString(s, " ")
	s = reGTIN.ReplaceAllString(s, " ")
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
