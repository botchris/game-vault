package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func problemsIn(t *testing.T, src string) []problem {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	require.NoError(t, err)

	return check(fset, f)
}

func TestCheck(t *testing.T) {
	t.Run("GIVEN members separated by blank lines, the first one right after the brace", func(t *testing.T) {
		src := `package x

// P is a port.
type P interface {
	// A does a.
	A()

	// B does b.
	B()
}

var (
	// ErrA is a.
	ErrA = 1

	// ErrB is b.
	ErrB = 2
)
`

		t.Run("THEN there is nothing to report", func(t *testing.T) {
			assert.Empty(t, problemsIn(t, src))
		})
	})

	t.Run("GIVEN a doc comment cuddled to the previous member and one after a blank opening line", func(t *testing.T) {
		src := `package x

var (

	// ErrA is a.
	ErrA = 1
	// ErrB is b.
	ErrB = 2
)
`

		t.Run("THEN both are reported, with the lines to fix", func(t *testing.T) {
			p := problemsIn(t, src)
			require.Len(t, p, 2)
			assert.Equal(t, 4, p[0].removeAt)
			assert.Equal(t, 7, p[1].insertBefore)
		})
	})

	t.Run("GIVEN a named interface with an undocumented method and an inline interface", func(t *testing.T) {
		src := `package x

// P is a port.
type P interface {
	A()
}

func scan(sc interface{ Scan(...any) error }) {}
`

		t.Run("THEN only the named interface's method is reported", func(t *testing.T) {
			p := problemsIn(t, src)
			require.Len(t, p, 1)
			assert.Contains(t, p[0].msg, "interface method A")
		})
	})
}

func TestFix(t *testing.T) {
	t.Run("GIVEN a file with both blank-line problems", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.go")
		require.NoError(t, os.WriteFile(path, []byte("package x\n\nvar (\n\n\t// A is a.\n\tA = 1\n\t// B is b.\n\tB = 2\n)\n"), 0o600))

		t.Run("WHEN it is fixed", func(t *testing.T) {
			left, err := checkFile(path, true)
			require.NoError(t, err)

			t.Run("THEN the blank lines are where they belong and nothing is left", func(t *testing.T) {
				assert.False(t, left)

				got, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "package x\n\nvar (\n\t// A is a.\n\tA = 1\n\n\t// B is b.\n\tB = 2\n)\n", string(got))
			})
		})
	})
}
