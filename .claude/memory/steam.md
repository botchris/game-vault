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
