// Command fieldlines checks a house rule no linter covers (see .claude/skills/write-go/SKILL.md):
// one struct field per line, in struct literals with several keyed fields and in struct types.
//
// Usage: go run ./tools/fieldlines [-fix] ./... — -fix breaks the fields onto lines of their own;
// run gofmt afterwards (task lint:fix does) to indent and align them. Generated files are skipped.
package main

import "golang.org/x/tools/go/analysis/singlechecker"

func main() { singlechecker.Main(Analyzer) }
