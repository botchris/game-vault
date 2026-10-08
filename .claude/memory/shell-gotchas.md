---
name: shell-gotchas
description: zsh variable names to avoid and invisible characters in source
metadata:
  type: feedback
---
- zsh: `path` is tied to `$PATH` and `GID` is read-only — never use them as variable names.
- Don't paste a raw BOM or other invisible characters into Go source; write escapes (`\ufeff`).
