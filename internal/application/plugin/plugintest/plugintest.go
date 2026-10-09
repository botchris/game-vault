// Package plugintest checks that a plugin is complete before it reaches the UI: ids and names set,
// every text it shows translated, every media provider in the right chain with a default place,
// and every store it links games to named. cmd/gamevault runs it on every registered plugin, so a
// new plugin is checked without writing anything.
package plugintest

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// Translations are the UI's translation keys, per language ("en" → {"sources.steam.description"…}).
type Translations map[string]map[string]bool

// LoadTranslations reads the UI's translation files (web/src/i18n/locales/*.json) from the
// repository that contains dir, flattening nested keys ("sources" → "steam" → "description" becomes
// "sources.steam.description").
func LoadTranslations(dir string) (Translations, error) {
	root, err := repoRoot(dir)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(root, "web", "src", "i18n", "locales", "*.json"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no translation files under %s", root)
	}

	out := Translations{}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}

		var tree map[string]any
		if err := json.Unmarshal(data, &tree); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}

		keys := map[string]bool{}
		flatten("", tree, keys)
		out[filepath.Base(f[:len(f)-len(filepath.Ext(f))])] = keys
	}

	return out, nil
}

func flatten(prefix string, tree map[string]any, out map[string]bool) {
	for k, v := range tree {
		if sub, ok := v.(map[string]any); ok {
			flatten(prefix+k+".", sub, out)
			continue
		}

		out[prefix+k] = true
	}
}

func repoRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}

		dir = parent
	}
}

var reLinkKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// Check reports, as test errors, everything incomplete in p (see Problems).
func Check(t *testing.T, p plugin.Plugin, tr Translations) {
	t.Helper()

	for _, problem := range Problems(p, tr) {
		t.Error(problem)
	}
}

// Problems lists everything incomplete in p, empty when it is ready.
func Problems(p plugin.Plugin, tr Translations) []string {
	c := &checker{tr: tr}

	if p.ID == "" || p.Name == "" {
		c.add("plugin %q: needs an ID and a Name", p.ID)
	}

	if len(p.Sources)+len(p.Covers)+len(p.Metadata)+len(p.Barcodes) == 0 {
		c.add("plugin %q: adds nothing (no source and no provider)", p.ID)
	}

	for _, s := range p.Sources {
		c.source(p.ID, s)
	}

	for _, cp := range p.Covers {
		c.provider(p.ID, cp, provider.KindCover)
	}

	for _, m := range p.Metadata {
		c.provider(p.ID, m, provider.KindMetadata)
	}

	for _, b := range p.Barcodes {
		c.provider(p.ID, b, provider.KindBarcode)
	}

	return c.problems
}

type checker struct {
	tr       Translations
	problems []string
}

func (c *checker) add(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

func (c *checker) source(plugin string, s sync.Provider) {
	d := s.Descriptor()
	where := fmt.Sprintf("plugin %q, source %q", plugin, d.Type)

	if d.Type == "" || d.Name == "" {
		c.add("%s: needs a Type and a Name", where)
	}

	c.translated(where, d.DescriptionKey)
	c.fields(where, d.Fields)

	if l, ok := s.(sync.StoreLinker); ok {
		c.store(where, l.LinkStore())
	}
}

func (c *checker) provider(plugin string, p media.Provider, kind provider.Kind) {
	d := p.Descriptor()
	where := fmt.Sprintf("plugin %q, provider %q", plugin, d.ID)

	if d.ID == "" || d.Name == "" {
		c.add("%s: needs an ID and a Name", where)
	}

	if d.Kind != kind {
		c.add("%s: is listed as a %s provider but its descriptor says %q", where, kind, d.Kind)
	}

	if d.DefaultOrder <= 0 {
		c.add("%s: needs a DefaultOrder (its place in the %s chain for new installs)", where, kind)
	}

	c.translated(where, d.DescriptionKey)
	c.fields(where, d.Fields)

	if l, ok := p.(media.StoreLinker); ok {
		c.store(where, l.LinkStore())
	}
}

func (c *checker) fields(where string, fs schema.Fields) {
	for _, f := range fs.Public() {
		c.translated(where+", field "+f.Key, f.LabelKey)

		if f.HelpKey != "" {
			c.translated(where+", field "+f.Key, f.HelpKey)
		}
	}
}

func (c *checker) translated(where, key string) {
	if key == "" {
		c.add("%s: a translation key is empty", where)
		return
	}

	for _, lang := range slices.Sorted(maps.Keys(c.tr)) {
		if !c.tr[lang][key] {
			c.add("%s: %q is missing in %s.json", where, key, lang)
		}
	}
}

func (c *checker) store(where string, s game.Store) {
	if !reLinkKey.MatchString(s.Key) || s.Name == "" {
		c.add("%s: links to a store without a valid Key (%q) or a Name", where, s.Key)
	}
}
