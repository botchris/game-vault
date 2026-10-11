---
name: steam
description: Steam store APIs used, rate limit, and Humble AppIDs that do not exist
metadata:
  type: reference
---
As of 2026-10:
- Store `appdetails` and `IStoreBrowseService/GetItems` (hashed asset paths for new apps) are
  public; about 200 requests per 5 minutes.
- Humble sometimes gives AppIDs that do not exist on the store (DLC): covered by the add-on cover
  fallback (see [[provider-chains]]).
- 2026-10-11: the Steam cover provider also searches `/api/storesearch/` by title on the cover
  fallback pass for PC editions without an AppID (one request; exact MatchKey, single app). A
  Battle.net game with no art may be searched twice (Battle.net's covers, then this).
- 2026-10-11 (review): `Store` has a throttle for the store host only (storesearch and appdetails:
  one at a time, burst 10, then 1.5 s apart; > 5 s wait for the turn or the pace → `ErrRateLimited`)
  and a 10-min search cache keyed without case, spaces or a trailing "(year)" (absorbs Battle.net's
  then Steam's search of one title). IStoreBrowseService art lookups (api.steampowered.com, the Web
  API, much higher limits) stay unthrottled and fall back to the guessed CDN URLs on error:
  throttling them made a fresh library's covers fill in over minutes (controller decision).
  Title matches: exact (case/™-insensitive) first, MatchKey only when none.
