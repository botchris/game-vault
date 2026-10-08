---
name: humble
description: Humble Bundle orders API: keyindex is not a key id (use machine_name); non-game keys filtered by platform; the one-copy-per-order bug (fixed 2026-10-08)
metadata:
  type: reference
---
As of 2026-10, `/api/v1/orders?all_tpkds=true` returns each order's keys in `tpkd_dict.all_tpks`.
- `keyindex` only numbers repeated copies of the **same game** in an order; it is 0 for almost
  every key. `machine_name` (e.g. `tunic_steam`) tells the keys of an order apart.
- Until 2026-10-08 the ExternalID was `humble:<gamekey>:<keyindex>`, so every key of an order
  shared one id and the consolidator folded them into one copy (game of the first key, key and
  status of the last); Humble Choice months showed one game each. Now `humble:<gamekey>:<machine_name>:<keyindex>`, with the old id
  as `PreviousExternalID` (see [[data-model]]).
- Bundles also hold non-game keys (software: Ashampoo, Corel, Magix; courses: Udemy, Zenva;
  in-game items: Duelyst, Sega, Destiny emblems; "45% off … PS Store Coupon"). Their key type is
  a vendor name, so only keys whose platform is in `game.IsKnownPlatform` (and not a coupon title)
  are games; the rest are returned `Withdrawn` so earlier imports are removed (2026-10-08).
- Lesson: the old test fixture invented keyindex 0..4; fixtures must copy the real shape.
- Humble Choice keys are ordinary tpks of the month's order once the game is chosen; no separate
  endpoint is needed. Playnite's "Humble Choice" option is the DRM-free Trove/Collection catalog,
  not keys.
