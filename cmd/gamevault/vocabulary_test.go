package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// rawVocabulary lists the string literals that equal a platform or system name (ignoring case), or
// are link keys, but are rightly not the domain's constants: values of an external API, compared
// with or sent to it, and the template plugin's imaginary store.
// Each entry is the file (from the repository root), the top-level declaration it is in, and the
// literal, so that the same word written anywhere else still fails the test.
var rawVocabulary = []struct {
	file  string
	decl  string
	value string
}{
	// Sony's platform codes in the entitlement list ("PS5", "PSVITA"…), mapped to Game Vault's.
	{"internal/adapters/outbound/playstation/provider.go", "sonyPlatforms", "PS5"},
	{"internal/adapters/outbound/playstation/provider.go", "sonyPlatforms", "PS4"},
	{"internal/adapters/outbound/playstation/provider.go", "sonyPlatforms", "PS3"},
	{"internal/adapters/outbound/playstation/provider.go", "sonyPlatforms", "PS VITA"},

	// Ubisoft's game type, and the word in its product names, that mark a PC game.
	{"internal/adapters/outbound/ubisoft/provider.go", "onPC", "PC"},

	// The EA app writes its name with a lowercase "app"; the copies' origin keeps the spelling
	// they were imported with (the platform is game.PlatformEA, "EA App").
	{"internal/adapters/outbound/eaapp/provider.go", "origin", "EA app"},

	// The imaginary store of the plugin template, which is never registered: a real store adds its
	// link key to the domain (internal/domain/game/platforms.go).
	{"internal/adapters/outbound/example/plugin.go", "LinkedStore", "example"},
}

// systemTables are provider tables keyed by catalog system names written as literals: a hundred
// systems have no game constant, and game.CheckProviderCoverage fails on a key that is not a
// catalog name, so they cannot drift; the same goes for each provider's unmappedSystems. Literals
// inside them are not checked.
var systemTables = []struct {
	file string
	decl string
}{
	{"internal/adapters/outbound/igdb/match.go", "platformIDs"},
	{"internal/adapters/outbound/igdb/match.go", "unmappedSystems"},
	{"internal/adapters/outbound/thegamesdb/provider.go", "platformNames"},
	{"internal/adapters/outbound/thegamesdb/provider.go", "unmappedSystems"},
	{"internal/adapters/outbound/wikipedia/details.go", "platformItems"},
	{"internal/adapters/outbound/wikipedia/details.go", "unmappedSystems"},
}

// TestVocabulary_adaptersUseTheDomainNames fails when the code outside the domain writes a platform
// or system name as a string literal. A literal that equals a name drifts from it the day one
// changes, and one that differs only in case ("EA app" for "EA App") silently breaks the owned-key
// check, which compares platforms exactly. Use the game.Platform…, game.System… constants instead,
// or, for a value of an external API, add the literal to rawVocabulary with the reason. Display
// names (a descriptor's Name field) are labels, not platforms, and are not checked; nor are
// literals in lowercase, which are ids, flags and query values ("steam", "xbox", "ps4"), unless the
// name itself is lowercase ("itch.io"). Link keys are lowercase, so they are checked apart: a string literal as
// a key of a game.Links literal or as a game.Store's Key fails here, and
// TestPlugins_linkKeysAreTheDomains checks the key of every store a plugin links to at run time.
func TestVocabulary_adaptersUseTheDomainNames(t *testing.T) {
	names := append(game.KnownPlatforms(), game.SystemOther)

	known := func(s string) (string, bool) {
		for _, n := range names {
			if strings.EqualFold(n, strings.TrimSpace(s)) && (s != strings.ToLower(s) || n == s) {
				return n, true
			}
		}

		return "", false
	}

	used := make([]bool, len(rawVocabulary))
	allowed := func(file, decl, value string) bool {
		for i, a := range rawVocabulary {
			if a.file == file && a.decl == decl && a.value == value {
				used[i] = true
				return true
			}
		}

		return false
	}

	root := filepath.Join("..", "..")

	for _, dir := range []string{"internal/adapters", "internal/application"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			rel = filepath.ToSlash(rel)
			fset := token.NewFileSet()

			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}

			for _, decl := range f.Decls {
				name := declName(decl)
				if slices.ContainsFunc(systemTables, func(tb struct {
					file string
					decl string
				}) bool {
					return tb.file == rel && tb.decl == name
				}) {
					continue
				}

				ast.Inspect(decl, func(n ast.Node) bool {
					if lit, ok := n.(*ast.CompositeLit); ok {
						for _, key := range literalLinkKeys(lit) {
							if v, err := strconv.Unquote(key.Value); err == nil && allowed(rel, name, v) {
								continue
							}

							assert.Failf(t, "link key written as a literal", "%s:%d (in %s): link key %s: use a game.Link… constant",
								rel, fset.Position(key.Pos()).Line, name, key.Value)
						}
					}

					if kv, ok := n.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Name" {
							return false // a display name
						}
					}

					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}

					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						return true
					}

					if want, ok := known(value); ok && !allowed(rel, name, value) {
						pos := fset.Position(lit.Pos())
						assert.Failf(t, "platform or system written as a literal",
							"%s:%d (in %s): %q is the domain's %q: use its game constant, or list it in rawVocabulary",
							rel, pos.Line, name, value, want)
					}

					return true
				})
			}

			return nil
		})
		require.NoError(t, err)
	}

	// An entry nothing matches any more would let the literal back in unnoticed later: remove it.
	for i, a := range rawVocabulary {
		assert.True(t, used[i], "rawVocabulary entry %s %s %q matches no literal: remove it", a.file, a.decl, a.value)
	}
}

// literalLinkKeys returns the link keys written as string literals in a game.Links or game.Store
// literal ({"steam": "620"}, game.Store{Key: "gog"}).
func literalLinkKeys(lit *ast.CompositeLit) []*ast.BasicLit {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "game" || (sel.Sel.Name != "Links" && sel.Sel.Name != "Store") {
		return nil
	}

	var out []*ast.BasicLit

	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key := kv.Key
		if sel.Sel.Name == "Store" {
			if id, ok := kv.Key.(*ast.Ident); !ok || id.Name != "Key" {
				continue
			}

			key = kv.Value
		}

		if b, ok := key.(*ast.BasicLit); ok && b.Kind == token.STRING {
			out = append(out, b)
		}
	}

	return out
}

// declName returns the name of a top-level declaration: the function's, or the first name the
// declaration introduces.
func declName(d ast.Decl) string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.ValueSpec:
				return s.Names[0].Name
			case *ast.TypeSpec:
				return s.Name.Name
			}
		}
	}

	return ""
}
