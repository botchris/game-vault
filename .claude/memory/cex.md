---
name: cex
description: CeX (webuy.com) box detail API: free EAN lookup with good PAL coverage; terms, limits and what we may use (2026-10-08)
metadata:
  type: reference
---
Checked 2026-10-08, suggested by the user as a barcode source (CeX's product ids are EANs).
- The site (Nuxt SPA) calls `GET https://wss2.cex.es.webuy.io/v3/boxes/{EAN}/detail` (no key, no
  cookie). Found: `response.data.boxDetails[0]` with `boxName` ("Dead Space 3 (2 Discs)"),
  `categoryName` / `categoryFriendlyName` ("Xbox 360 Juegos"), `superCatName` ("Juegos"),
  `imageUrls`, prices. Unknown EAN: HTTP 200 with `"data": null`. Other countries use their own
  host (`wss2.cex.uk.webuy.io`, …) and catalog.
- Terms (es.webuy.com/site/terms) do not mention bots or automated access; content and "data
  compilations" are CeX's copyright. robots.txt allows product pages. Pages load Cloudflare's
  challenge script, but a plain request with an honest User-Agent got JSON (no challenge).
- Community docs: github.com/Dionakra/webuy-api (archived 2021, no license, plain fetch with no
  special headers). Same `v3` API on every country (`wss2.cex.{uk,es,ie,pt,…}.webuy.io/v3`), so
  it has been stable since at least 2019. Also documents `/boxes?q=` search, `/supercats`,
  `/categories`, store stock, top sellers.
- Use `superCatId == 1` for games: names are localized ("Juegos" in ES, "Gaming" in UK), and so
  are categories ("Xbox 360 Juegos" in ES; "Playstation4 Software" / friendly "Playstation4
  Games" in UK). `attributeInfo` (publisher, PEGI, developer) was null for the ES box tried.
- **Implemented 2026-10-08** as the barcode provider `cex` (first in the default chain): countries
  asked one by one, the ones that answered first (state field `country_hits`, persisted by
  `provider.UpdateState` after each lookup); user setting `countries` limits them. A product's
  EAN is only in the catalog of the country that sells that edition, and GS1 prefixes do not
  tell the market (Spanish Dead Space 3: prefix 503 = UK, only in the ES catalog).
- **How to apply:** one lookup per user scan, never crawling; keep only name and platform for barcode lookups;
  never store or show CeX's images; prices only as below. If Cloudflare starts challenging, fail with a clear error;
  never work around it (CLAUDE.md safety rules). Related: [[ean-search]], [[provider-chains]].
- **Prices allowed (user's decision, 2026-10-09):** the `cex-prices` provider stores CeX's
  `sellPrice`, `cashPrice` and `exchangePrice` (decimal units of the country's currency: 20 / 6 / 10
  EUR for Dead Space 3 in ES) **for the user's own copies only**: one query per copy, on its own
  random date (20–40 days) or the "Update price" button, never crawling, no images. Product page:
  `https://{country}.webuy.com/product-detail/?id={EAN}` (301 from the URL without the slash).
- **Search is blocked:** `GET /v3/boxes?q=…` answered a Cloudflare challenge (403 "Attention
  Required") on 2026-10-09 in ES and UK while `boxes/{EAN}/detail` kept answering JSON. Never work
  around it; copies without a barcode are not priced.
