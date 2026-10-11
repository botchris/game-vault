---
name: wikipedia
description: Wikidata/Wikipedia APIs used for game sheets, matching rules, User-Agent policy, CC BY-SA credit, probed shapes
metadata:
  type: reference
---
As of 2026-10-11 (plugin `internal/adapters/outbound/wikipedia`, details `wikipedia-details`,
enabled by default, no settings):
- **Terms:** texts are CC BY-SA 4.0: the summary carries `GameDetails.SummarySource` (name, article
  URL, license) and the sheet shows "Source: Wikipedia" linking to the article. Wikimedia's
  User-Agent policy: descriptive agent with contact info → `GameVault/<version>
  (https://github.com/botchris/game-vault)`; an empty agent gets HTTP 403. Requests one at a time,
  200 ms apart.
- **Endpoints:** `www.wikidata.org/w/api.php?action=wbsearchentities` (title in "en" and the UI
  language), `action=wbgetentities` (claims, sitelinks `<lang>wiki|enwiki`, labels `<lang>|en|mul`),
  `<lang>.wikipedia.org/w/api.php?action=query&prop=extracts|info|pageprops&exintro&explaintext&inprop=url&redirects&formatversion=2`.
- **Probed live (2026-10-11, public):** many labels are only in `mul` now (e.g. "Portal 2"), and
  `languages=en` alone returns empty labels for some items (PS4, Xbox One): always ask for `mul`.
  API errors come with HTTP 200 and `{"error":{"code","info"}}` (e.g. `missingparam`,
  `no-such-entity`); `maxlag`/`ratelimited` are treated as rate limits. Disambiguation pages have
  `pageprops.disambiguation`. Platform items checked: Windows Q1406, PS5 Q63184502, PS4 Q5014725,
  PS3 Q10683, PS2 Q10680, PS1 Q10677, PSP Q170325, Vita Q188808, Xbox Series X/S Q98973368, One
  Q13361286, 360 Q48263, Xbox Q132020, Switch Q19610114, Wii U Q56942, Wii Q8079, GameCube Q182172,
  N64 Q184839, 3DS Q203597, DS Q170323, Game Boy Q186437 (note: Q10676 is the Sega Genesis).
- **Matching:** only instances of Q7889 whose label/alias equals the title; exactly one on the
  game's systems (P400), or the only one when no system is known or it lists no platforms. A smoke
  run against the real APIs found Portal 2 (es/en), Hades (fr), Breath of the Wild (es),
  Celeste (de), Halo 3, and returned nothing for the ambiguous "Doom" on PC.
- **Review fixes (2026-10-11):** `Applies` is false for Steam-linked games unless `q.Fallback`
  (Steam's summary always wins; spares 4-5 requests per game in the scan); regional UI languages
  use the primary subtag; P577 ties go to the more precise date; the license links to
  creativecommons.org (`Attribution.LicenseURL`) and the UI only renders https hrefs; Wikidata
  genres join the genre filter unmapped (accepted).
- The summary is in the UI language when that Wikipedia has the article, else English
  ([[multilingual]]); labels follow the same fallback, genres capitalized.
