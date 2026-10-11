---
name: multilingual
description: The product is multilingual — English by default, Spanish optional, more languages later; never design around Spanish
metadata:
  type: feedback
---

Game Vault is multilingual (stated by the maintainer on 2026-10-11): English is the default UI language,
Spanish is an option, and more languages may be added. Features that depend on language (metadata
summaries, Wikipedia articles, provider requests) follow the UI language and fall back to English;
none treats Spanish as a priority or a special case.

**Why:** a design note proposed favouring Spanish summaries; the maintainer corrected that the
project is not Spanish-first.
**How to apply:** take the language from the request or UI setting, support any language code, and
fall back to English; write examples and docs in English with "the UI language" rather than
"Spanish".
