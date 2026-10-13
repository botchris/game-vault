package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// rawVocabulary lists the string literals that equal a platform or system name (ignoring case) but
// are rightly not the domain's constants: values of an external API, compared with or sent to it.
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

	// TheGamesDB's own platform name for PC.
	{"internal/adapters/outbound/thegamesdb/provider.go", "platformNames", "PC"},

	// Ubisoft's game type, and the word in its product names, that mark a PC game.
	{"internal/adapters/outbound/ubisoft/provider.go", "onPC", "PC"},

	// The EA app writes its name with a lowercase "app"; the copies' origin keeps the spelling
	// they were imported with (the platform is game.PlatformEA, "EA App").
	{"internal/adapters/outbound/eaapp/provider.go", "origin", "EA app"},
}

// TestVocabulary_adaptersUseTheDomainNames fails when the code outside the domain writes a platform
// or system name as a string literal. A literal that equals a name drifts from it the day one
// changes, and one that differs only in case ("EA app" for "EA App") silently breaks the owned-key
// check, which compares platforms exactly. Use the game.Platform…, game.System… constants instead,
// or, for a value of an external API, add the literal to rawVocabulary with the reason. Display
// names (a descriptor's Name field) are labels, not platforms, and are not checked; nor are
// literals in lowercase, which are ids, flags and query values ("steam", "xbox", "ps4"), unless the
// name itself is lowercase ("itch.io").
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

				ast.Inspect(decl, func(n ast.Node) bool {
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
