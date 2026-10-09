---
name: amazon
description: Amazon Games / Prime Gaming: launcher device sign-in (nile/Heroic), entitlements API, what the probes returned (2026-10-09)
metadata:
  type: reference
---
As of 2026-10-09, from imLinguin/nile (GPL-3, Heroic's Amazon backend; used as reference only):
- Sign-in as the Amazon Games launcher: device serial (random hex) → client id =
  hex("<serial>#A2UMVHOX7UP4V7"); PKCE S256; `https://amazon.com/ap/signin?…` with
  `openid.assoc_handle=amzn_sonic_games_launcher`, `openid.oa2.scope=device_auth_access`,
  `openid.oa2.client_id=device:<client id>`, `openid.return_to=https://www.amazon.com`. After
  signing in the browser lands on amazon.com with `openid.oa2.authorization_code=` in the URL.
- `POST https://api.amazon.com/auth/register` (auth_data with code + verifier + client id,
  registration_data with serial, device_type `A2UMVHOX7UP4V7`, app "AGSLauncher for Windows")
  → `response.success.tokens.bearer.{access_token,refresh_token,expires_in}`.
- `POST https://api.amazon.com/auth/token` (source_token = refresh token) → new access token;
  the refresh token does **not** rotate (nile never replaces it).
- Library: `POST https://gaming.amazon.com/api/distribution/entitlements`, headers
  `X-Amz-Target: com.amazon.animusdistributionservice.entitlement.AnimusEntitlementsService.GetEntitlements`,
  `x-amzn-token: <access token>`, `Content-Encoding: amz-1.0`; body with `keyId`
  `d5dc8b8b-86c8-4fc4-ae93-18c0def5314d`, `hardwareHash` = SHA-256(serial) upper hex,
  `maxResults` 50, `nextToken` paging. Items: `product.id`, `product.title` (may be missing),
  `product.productDetail.iconUrl` (square), `details.backgroundUrl1/2`, `details.websites.steam`.
  No portrait box art.
- Probes with bogus values: register → 400 `{"response":{"error":{"code":"InvalidValue"}}}`;
  token → 400 `error: InvalidValue` (source_token); entitlements → 401 `<NotAuthorizedException/>`.
- Prime Gaming also hands out codes for other stores (GOG, Epic, Legacy Games): those are not
  entitlements and are not listed here.
