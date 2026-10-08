---
name: go-gotchas
description: filepath.Glob breaks on game-data folder names with brackets
metadata:
  type: feedback
---
- `filepath.Glob` treats `[id]` in game-data folder names as a character class: use
  `os.ReadDir`.
