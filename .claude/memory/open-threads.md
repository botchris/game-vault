---
name: open-threads
description: Things offered to the user but not done yet; ask before doing them
metadata:
  type: project
---
- Adding physical games from a photo of the box (local OCR + catalog match + CLIP tie-break, no
  paid LLMs): spike done, paused until the user has 30-50 test photos. See [[cover-recognition]].
- PlayStation Store cover provider (today PS games get TheGamesDB box art, or Steam's when the
  game is also on Steam).
- New sources: itch.io (official API with a personal key — the easiest), Amazon Games / Prime
  Gaming (Heroic's approach). Nintendo is not feasible: there is no way to list purchases.
- A manual "open in EAN-Search" button on the Scan page for unknown barcodes (offered, never
  answered). See [[ean-search]].
- A CI workflow running `task lint test build:release` once the project is on GitHub.
- Moving the old `config/README.md` text into `docs/technical.md` (`config/` is now fully ignored
  by git).
