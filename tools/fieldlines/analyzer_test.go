package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestAnalyzer checks the reports (the "want" comments in testdata/src/a/a.go) and the fixes
// (a.go.golden, before gofmt indents and aligns them): struct literals and types are reported, map
// literals, positional literals and single-field literals are not.
func TestAnalyzer(t *testing.T) {
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), Analyzer, "a")
}
