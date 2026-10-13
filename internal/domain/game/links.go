package game

import (
	"errors"
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

// HasPCStore reports whether the links name a game in a store that sells PC games.
func (l Links) HasPCStore() bool {
	for k, v := range l {
		if v != "" && IsPCStoreLink(k) {
			return true
		}
	}

	return false
}

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

// normalize trims every id and drops the empty ones (an empty id unlinks the store). One invalid
// link fails them all: it is for links the user or a source set, which are taken whole or not at all.
func (l Links) normalize() (Links, error) {
	out := Links{}

	for k, v := range l {
		k, v, err := normalizeLink(k, v)
		if err != nil {
			return nil, err
		}

		if v != "" {
			out[k] = v
		}
	}

	return out, nil
}

// Valid returns the valid links, normalized like a game's, and an error naming each invalid one.
// It is for links found automatically, where one bad id must not cost the good ones found with it.
func (l Links) Valid() (Links, error) {
	out := Links{}

	var errs []error

	for _, k := range l.Keys() { // sorted, so the error reads the same every time
		k, v, err := normalizeLink(k, l[k])
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if v != "" {
			out[k] = v
		}
	}

	return out, errors.Join(errs...)
}

// normalizeLink trims a link and checks it. An empty id is valid: it means no link.
func normalizeLink(k, v string) (key, id string, err error) {
	k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
	if v == "" {
		return k, "", nil
	}

	if !reLinkKey.MatchString(k) {
		return "", "", invalid("link key %q must be a short lowercase name like \"steam\"", k)
	}

	if len(v) > maxLinkID || strings.ContainsFunc(v, isSpace) {
		return "", "", invalid("the %s link must be an id without spaces", k)
	}

	return k, v, nil
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
