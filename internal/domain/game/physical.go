package game

import "slices"

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

// grades lists every grade, best first.
var grades = []Grade{GradeSealed, GradeMint, GradeVeryGood, GradeGood, GradeAcceptable, GradeDamaged}

// Grades returns every grade, best first (a copy: callers cannot change the domain's list).
func Grades() []Grade { return slices.Clone(grades) }

// Valid reports whether g is a known grade or empty.
func (g Grade) Valid() bool { return g == "" || slices.Contains(grades, g) }

// Content is one part of what a physical copy comes with.
type Content string

// Values of Content, in the order they are listed.
const (
	ContentBox    Content = "box"
	ContentManual Content = "manual"
	ContentMedia  Content = "media"  // the disc or cartridge
	ContentExtras Content = "extras" // map, poster, figure, art book…
)

// allContents lists every content, in the order they are listed. A content's position is its bit
// in Contents, which is stored: never reorder it, only append.
var allContents = []Content{ContentBox, ContentManual, ContentMedia, ContentExtras}

// AllContents returns every content, in the order they are listed (a copy: callers cannot change
// the domain's list, whose order gives the stored bits their meaning).
func AllContents() []Content { return slices.Clone(allContents) }

// Contents is the set of parts a physical copy comes with. It is a bit set, so it has no
// duplicates, always lists in the same order and compares with ==. Zero means not stated.
type Contents uint8

// ContentsOf builds the set from its parts, refusing unknown ones.
func ContentsOf(cs ...Content) (Contents, error) {
	var out Contents

	for _, c := range cs {
		i := slices.Index(allContents, c)
		if i < 0 {
			return 0, invalid("content %q is not one of box, manual, media, extras", c)
		}

		out |= 1 << i
	}

	return out, nil
}

// Has reports whether the set holds c.
func (c Contents) Has(x Content) bool {
	i := slices.Index(allContents, x)
	return i >= 0 && c&(1<<i) != 0
}

// List returns the parts in the fixed order.
func (c Contents) List() []Content {
	var out []Content

	for _, x := range allContents {
		if c.Has(x) {
			out = append(out, x)
		}
	}

	return out
}

// valid reports whether c holds only known parts.
func (c Contents) valid() bool { return c < 1<<len(allContents) }
