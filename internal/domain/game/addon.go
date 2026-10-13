package game

import "regexp"

// reAddOn recognizes titles of extra content: "GRIP: Combat Racing Artifex DLC", "X - Soundtrack"….
var reAddOn = regexp.MustCompile(`(?i)\b(dlcs?|season\s+pass(es)?|expansions?(\s+pass)?|soundtracks?|ost|add-?on|content\s+pack|bonus\s+content|costumes?|skins?\s+pack|art\s*book|upgrade\s+pack|character\s+pack|map\s+pack)\b`)

// IsAddOnTitle reports whether a title looks like extra content for another game (DLC, a
// soundtrack, a season pass…). Stores often have no art of their own for these.
func IsAddOnTitle(title string) bool { return reAddOn.MatchString(title) }

// StripAddOnMarker returns the part of an add-on's title before the word that marks it as extra
// content ("GRIP: Combat Racing Artifex " for "GRIP: Combat Racing Artifex DLC"), where the base
// game's title is to be found; the whole title when it has no such word.
func StripAddOnMarker(title string) string {
	if loc := reAddOn.FindStringIndex(title); loc != nil {
		return title[:loc[0]]
	}

	return title
}
