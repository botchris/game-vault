package game

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Links maps a store or catalog to the game's id in it: {"steam": "620"}. Providers that know a
// store read its link to find the game's art and details, and imports use links to recognize the
// same game across sources, whatever its title there. No store is special: a link key is just the
// name a source and a provider agree on.
type Links map[string]string

// LinkSteam is the key of Steam AppIDs. Unlike other stores' keys it is shared here because sources
// other than Steam's (Humble Bundle, Amazon) report AppIDs too.
const LinkSteam = "steam"

// Store describes a store games can be linked to. The source that imports a store's library and the
// providers that read its links declare the same Store.
type Store struct {
	// Key is the link key of the store's ids ("steam", "epic"…).
	Key string

	// Name is the store's display name.
	Name string

	// PageURL is the address of a game's page in the store, with "{id}" where its id goes; empty
	// when the store has no page addressed by that id.
	PageURL string
}

var reLinkKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// maxLinkID bounds a link id; store ids are short numbers or slugs.
const maxLinkID = 200

// normalize trims every id and drops the empty ones (an empty id unlinks the store).
func (l Links) normalize() (Links, error) {
	out := Links{}

	for k, v := range l {
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if v == "" {
			continue
		}

		if !reLinkKey.MatchString(k) {
			return nil, invalid("link key %q must be a short lowercase name like \"steam\"", k)
		}

		if len(v) > maxLinkID || strings.ContainsFunc(v, isSpace) {
			return nil, invalid("the %s link must be an id without spaces", k)
		}

		out[k] = v
	}

	return out, nil
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// Keys returns the linked stores, sorted.
func (l Links) Keys() []string { return slices.Sorted(maps.Keys(l)) }

// Equal reports whether both have the same links.
func (l Links) Equal(o Links) bool { return maps.Equal(l, o) }

// clone returns a copy that never aliases l, nil when there are no links.
func (l Links) clone() Links {
	if len(l) == 0 {
		return nil
	}

	return maps.Clone(l)
}

// fill adds the links of o to l for stores l is not linked to yet, and returns the ones it added.
// A link already set (by hand, or by an earlier import) is never replaced.
func (l *Links) fill(o Links) Links {
	var added Links

	for _, k := range o.Keys() {
		if _, ok := (*l)[k]; ok || o[k] == "" {
			continue
		}

		if *l == nil {
			*l = Links{}
		}

		if added == nil {
			added = Links{}
		}

		(*l)[k], added[k] = o[k], o[k]
	}

	return added
}
