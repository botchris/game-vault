// Command docspacing checks the layout of documented members in grouped declarations, a house
// rule no linter covers (see .claude/skills/write-go/SKILL.md):
//
//   - a member's doc comment is separated from the previous member by a blank line;
//   - the first member's doc comment follows the opening brace or parenthesis directly;
//   - every method of a named interface type has a doc comment (inline interfaces such as
//     interface{ Scan(...any) error } in a parameter list are left alone).
//
// Grouped declarations are interface and struct types and parenthesised const, var and type
// blocks. Usage: docspacing [-fix] DIR..., where -fix inserts and removes the blank lines (missing
// interface docs still have to be written by hand). Generated files are skipped.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// problem is one rule broken at a line of a file.
type problem struct {
	line int
	msg  string

	// insertBefore and removeAt are 1-based lines for -fix (0 when there is nothing to do).
	insertBefore int
	removeAt     int
}

func main() {
	fix := flag.Bool("fix", false, "insert and remove blank lines instead of only reporting")

	flag.Parse()

	failed := false

	for _, dir := range flag.Args() {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}

			bad, err := checkFile(path, *fix)
			if err != nil {
				return err
			}

			failed = failed || bad

			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}

	if failed {
		os.Exit(1)
	}
}

// checkFile reports the problems in one file, or fixes the blank lines when fix is set. It
// returns whether problems remain.
func checkFile(path string, fix bool) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, err
	}

	if ast.IsGenerated(f) {
		return false, nil
	}

	problems := check(fset, f)
	if len(problems) == 0 {
		return false, nil
	}

	if fix {
		problems = apply(path, src, problems)
	}

	for _, p := range problems {
		fmt.Printf("%s:%d: %s\n", path, p.line, p.msg)
	}

	return len(problems) > 0, nil
}

// member is one entry of a grouped declaration: its doc comment (may be nil) and its extent.
type member struct {
	doc   *ast.CommentGroup
	start token.Pos
	end   token.Pos
}

// check finds the problems of every grouped declaration in the file.
func check(fset *token.FileSet, f *ast.File) []problem {
	var out []problem

	line := func(p token.Pos) int { return fset.Position(p).Line }

	group := func(open token.Pos, members []member) {
		for i, m := range members {
			if m.doc == nil {
				continue
			}

			docLine := line(m.doc.Pos())
			if i == 0 {
				if docLine > line(open)+1 {
					out = append(out, problem{
						line:     docLine,
						msg:      "no blank line between the opening brace and the first doc comment",
						removeAt: docLine - 1,
					})
				}

				continue
			}

			if docLine == line(members[i-1].end)+1 {
				out = append(out, problem{
					line:         docLine,
					msg:          "doc comment needs a blank line above it, to separate it from the previous member",
					insertBefore: docLine,
				})
			}
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GenDecl:
			if !n.Lparen.IsValid() {
				return true
			}

			var ms []member
			for _, s := range n.Specs {
				ms = append(ms, member{
					doc:   specDoc(s),
					start: s.Pos(),
					end:   s.End(),
				})
			}

			group(n.Lparen, ms)
		case *ast.TypeSpec:
			it, ok := n.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}

			for _, fl := range it.Methods.List {
				if _, isMethod := fl.Type.(*ast.FuncType); isMethod && fl.Doc == nil {
					out = append(out, problem{
						line: line(fl.Pos()),
						msg:  fmt.Sprintf("interface method %s needs a doc comment", fl.Names[0].Name),
					})
				}
			}
		case *ast.InterfaceType:
			group(n.Methods.Opening, fieldMembers(n.Methods))
		case *ast.StructType:
			group(n.Fields.Opening, fieldMembers(n.Fields))
		}

		return true
	})

	return out
}

func fieldMembers(fl *ast.FieldList) []member {
	ms := make([]member, 0, len(fl.List))
	for _, f := range fl.List {
		ms = append(ms, member{
			doc:   f.Doc,
			start: f.Pos(),
			end:   f.End(),
		})
	}

	return ms
}

func specDoc(s ast.Spec) *ast.CommentGroup {
	switch s := s.(type) {
	case *ast.ValueSpec:
		return s.Doc
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ImportSpec:
		return s.Doc
	}

	return nil
}

// apply fixes the blank lines in the file and returns the problems it could not fix.
func apply(path string, src []byte, problems []problem) []problem {
	lines := strings.Split(string(src), "\n")

	var left []problem

	edits := map[int]string{} // 1-based line → "insert" before it or "remove" it

	for _, p := range problems {
		switch {
		case p.insertBefore > 0:
			edits[p.insertBefore] = "insert"
		case p.removeAt > 0 && strings.TrimSpace(lines[p.removeAt-1]) == "":
			edits[p.removeAt] = "remove"
		default:
			left = append(left, p)
		}
	}

	at := make([]int, 0, len(edits))
	for l := range edits {
		at = append(at, l)
	}
	// From the bottom up, so earlier line numbers stay valid.
	sort.Sort(sort.Reverse(sort.IntSlice(at)))

	for _, l := range at {
		i := l - 1
		if edits[l] == "remove" {
			lines = append(lines[:i], lines[i+1:]...)
		} else {
			lines = append(lines[:i], append([]string{""}, lines[i:]...)...)
		}
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		left = append(left, problem{msg: "could not write the fixes: " + err.Error()})
	}

	return left
}
