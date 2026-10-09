package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports struct literals and struct types with more than one field on a line.
var Analyzer = &analysis.Analyzer{
	Name: "fieldlines",
	Doc: `one struct field per line

A struct literal with two or more keyed fields puts each field on its own line, and the closing
brace on a line of its own. A struct type declares one field per line, one name per field. Map
literals, positional struct literals (test tables) and literals with a single field are left alone.`,
	Run: run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if ast.IsGenerated(f) {
			continue
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				checkLiteral(pass, n)
			case *ast.StructType:
				checkStructType(pass, n)
			}

			return true
		})
	}

	return nil, nil //nolint:nilnil // an analyzer without facts has no result
}

// checkLiteral reports a keyed struct literal whose fields share lines, with the fix that breaks
// them onto lines of their own (gofmt then indents and aligns them).
func checkLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	if len(lit.Elts) < 2 || !isStruct(pass.TypesInfo.TypeOf(lit)) {
		return
	}

	for _, e := range lit.Elts {
		if _, keyed := e.(*ast.KeyValueExpr); !keyed {
			return
		}
	}

	line := func(p token.Pos) int { return pass.Fset.Position(p).Line }

	var edits []analysis.TextEdit

	prevEnd := lit.Lbrace
	for _, e := range lit.Elts {
		if line(e.Pos()) == line(prevEnd) {
			edits = append(edits, analysis.TextEdit{
				Pos:     e.Pos(),
				End:     e.Pos(),
				NewText: []byte("\n"),
			})
		}

		prevEnd = e.End()
	}

	last := lit.Elts[len(lit.Elts)-1]
	if line(last.End()) == line(lit.Rbrace) {
		// Whatever sits between the last field and the brace is a comma or spaces: a trailing comma
		// is required once the brace moves to its own line.
		edits = append(edits, analysis.TextEdit{
			Pos:     last.End(),
			End:     lit.Rbrace,
			NewText: []byte(",\n"),
		})
	}

	if len(edits) == 0 {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos:     lit.Pos(),
		End:     lit.End(),
		Message: "struct literal with several fields on a line: put each field on its own line",
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "one field per line",
			TextEdits: edits,
		}},
	})
}

// checkStructType reports struct types that declare several fields on one line, either as one
// field with several names (a, b string) or as fields on the same line. The fix splits them, one
// name per field and one field per line; a multi-line struct whose fields share a line with a
// semicolon is only reported.
func checkStructType(pass *analysis.Pass, st *ast.StructType) {
	line := func(p token.Pos) int { return pass.Fset.Position(p).Line }

	fields := st.Fields.List
	if len(fields) == 0 {
		return
	}

	if line(st.Fields.Opening) == line(st.Fields.Closing) {
		if len(fields) > 1 || len(fields[0].Names) > 1 {
			lines := make([]string, 0, len(fields))
			for _, f := range fields {
				lines = append(lines, declarations(pass, f)...)
			}

			pass.Report(analysis.Diagnostic{
				Pos:     st.Pos(),
				Message: "struct type with several fields on a line: declare each field on its own line",
				SuggestedFixes: []analysis.SuggestedFix{{
					Message: "one field per line",
					TextEdits: []analysis.TextEdit{{
						Pos:     st.Fields.Opening + 1,
						End:     st.Fields.Closing,
						NewText: []byte("\n" + strings.Join(lines, "\n") + "\n"),
					}},
				}},
			})
		}

		return
	}

	prevLine := -1

	for _, f := range fields {
		if line(f.Pos()) == prevLine {
			pass.Reportf(f.Pos(), "struct type with several fields on a line: declare each field on its own line")
		}

		prevLine = line(f.End())

		if len(f.Names) < 2 {
			continue
		}

		pass.Report(analysis.Diagnostic{
			Pos:     f.Pos(),
			Message: "struct field with several names: declare each field on its own line",
			SuggestedFixes: []analysis.SuggestedFix{{
				Message: "one field per line",
				TextEdits: []analysis.TextEdit{{
					Pos:     f.Names[0].Pos(),
					End:     f.End(),
					NewText: []byte(strings.Join(declarations(pass, f), "\n")),
				}},
			}},
		})
	}
}

// declarations returns a field as one declaration per name: "a, b string" is "a string" and
// "b string" (an embedded field is itself).
func declarations(pass *analysis.Pass, f *ast.Field) []string {
	typ := source(pass, f.Type.Pos(), f.End())
	if len(f.Names) == 0 {
		return []string{typ}
	}

	out := make([]string, 0, len(f.Names))
	for _, n := range f.Names {
		out = append(out, n.Name+" "+typ)
	}

	return out
}

// isStruct reports whether t is a struct, or a pointer to one (&T{…}), after resolving names.
func isStruct(t types.Type) bool {
	if t == nil {
		return false
	}

	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}

	_, ok := t.Underlying().(*types.Struct)

	return ok
}

// source returns the file's text between two positions (a field's type and tag).
func source(pass *analysis.Pass, from, to token.Pos) string {
	tf := pass.Fset.File(from)
	if tf == nil {
		return ""
	}

	content, err := pass.ReadFile(tf.Name())
	if err != nil {
		return ""
	}

	return string(content[tf.Offset(from):tf.Offset(to)])
}
