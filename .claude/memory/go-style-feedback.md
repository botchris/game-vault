---
name: go-style-feedback
description: Always load the write-go skill before touching Go; doc-comment layout and one-struct-field-per-line rules the user insists on
metadata:
  type: feedback
---
- **Load the `write-go` skill before writing or editing any Go**, every time (asked 2026-10-09
  after I skipped it and broke its rules).
- Documented members of a grouped declaration (interface methods, struct fields, const/var
  blocks) are separated by a blank line; every interface method has a doc comment, `Descriptor()`
  included; member docs start with the name (`// Forwarded reports that…`, never `// Forwarded:`).
- **One struct field per line** (asked 2026-10-09): a struct literal with several keyed fields puts
  each on its own line, even when short (`game.Store{Key: "x", Name: "X"}` is wrong); struct types
  declare one name per field. Maps, positional literals and single-field literals are exempt.

**Why:** no linter enforced it, so I kept writing cuddled, undocumented members.
**How to apply:** `task lint` runs `tools/docspacing` and `tools/fieldlines` and fails on them;
`task lint:fix` fixes both (missing interface docs must be written by hand). Write new code in that
shape from the start rather than relying on the fix. See [[toolchain]].
