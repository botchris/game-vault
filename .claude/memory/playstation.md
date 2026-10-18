---
name: playstation
description: PlayStation: NPSSO sign-in, registered GraphQL hashes, CSRF header
metadata:
  type: reference
---
As of 2026-10:
- Names come in the account's language unless asked: since 2026-10-18 both Sony clients send
  `Accept-Language: en-US` (as PSNAWP does); untested on a real account. Existing copies keep the
  title they were imported with (matched by external id).
- NPSSO from `ca.account.sony.com/api/v1/ssocookie` (the user must be signed in on
  playstation.com).
- The library GraphQL only runs registered query hashes, and checks the hash before auth (so
  hashes can be verified without an account).
- Requests need `Content-Type: application/json` (CSRF guard).
- PS Store covers dropped (2026-10-10): PS games already get covers from Steam or TheGamesDB, and playstation.com's website terms (section 10) forbid automated access, so store
  pages are never read. The library query's `image.url` (square MASTER art) is still unused.
- The library lists apps, demos, betas, soundtracks and videos as "PS4" with `CUSA…` ids, and its
  answer has no field that tells them apart. Since 2026-10-12 they are withdrawn on each scan by:
  - the PlayStation app's played-titles list, `GET m.np.playstation.com/api/gamelist/v2/users/me/titles`
    (psn-api `getUserPlayedGames`, PSNAWP), same Bearer token, `limit=200&offset=N` paged by
    `nextOffset`; a bogus token gives 401 `{"error":{"reason":"invalidToken",...}}`. Read twice
    (no `categories`, then `categories=ps4_game,ps5_native_game,pspc_game`): a purchased title
    matching (title id, `concept.titleIds`, `concept.id`) one that is in the first list but not the
    second is not a game. Never-played titles stay; if a list fails, no category exclusions (warn log).
    Response shape taken from psn-api's types, only tested against fakes so far;
  - name markers (`nonGameName`): trailing Demo / Trial / Beta / Soundtrack / OST / Theme / Avatar(s) /
    Art Book (+ Spanish), "Behind the Scenes" / "Making of" anywhere (Spanish "Detrás de (las) cámaras" / "Cómo se hizo" since 2026-10-15), and (2026-10-13) the same words at the
    start when a joiner follows ("Banda sonora de X", "Soundtrack of X", "Original Soundtrack: X",
    "Demo - X"; no "of" for demo/trial/theme/avatar, since "Trial of the Lantern" is a game). ™ ® are
    ignored; 2026-10-18 added trailing Tech Test, "Teaser" anywhere, leading "The Art of ", quotes
  ignored (`RESIDENT EVIL 3 "Raccoon City Demo"`). Withdrawn copies keep their id, so earlier imports are removed on the next scan.
  The store catalog would tell every title's kind, but it is not used: playstation.com's terms forbid
  automated access. Anything else is still removed by hand with "Remove and do not import again".
