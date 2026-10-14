---
name: ubisoft
description: Ubisoft: blocked app id, PRODrememberMe ticket that rotates on every use, 50-item pages, box art sources
metadata:
  type: reference
---
As of 2026-10:
- App `314d4fef…` (used by Lutris and the GOG Galaxy plugin) is blocked on the public gateway;
  use ubisoft.com's `f35adcb5…`.
- The remember-me ticket is in Local Storage `PRODrememberMe` and **rotates on every use** (the
  old one dies). Copy it from a private window, or renewals sign the user's browser out.
- GraphQL `limit` is at most 50.
- Box art on `ubiservices.cdn.ubi.com/<spaceId>/spaceCardAsset/boxArt_mobile.jpg` for most
  games; the store's public Algolia search for the rest.
- 2026-10-14: in production `viewer.meta.ownedPlatformGroups` came back as a list of lists of
  `{name, type}` (the test fakes had a flat list), which failed the whole scan. It is now decoded
  leniently (flat, nested, or unknown = no information), and an answer whose fields changed shape
  is asked again with the minimal query, as a rejected query is.
