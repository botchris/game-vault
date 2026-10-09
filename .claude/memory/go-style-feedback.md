---
name: go-style-feedback
description: Always load the write-go skill before touching Go; doc-comment layout rules the user insists on
metadata:
  type: feedback
---
- **Load the `write-go` skill before writing or editing any Go**, every time (asked 2026-10-09
  after I skipped it and broke its rules).
- Documented members of a grouped declaration (interface methods, struct fields, const/var
  blocks) are separated by a blank line; every interface method has a doc comment, `Descriptor()`
  included; member docs start with the name (`// Forwarded reports that…`, never `// Forwarded:`).

**Why:** no linter enforced it, so I kept writing cuddled, undocumented members.
**How to apply:** `task lint` now runs `tools/docspacing` and fails on it; `task lint:fix` adds the
blank lines. Missing interface docs must be written by hand. See [[toolchain]].
