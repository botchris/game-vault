# "Do I already own it?" in store pages and for bundles

Status: approved design, waiting for the implementation plan. Branch `feature/ownership-check`.

## Why

The question Game Vault exists to answer is "do I already have it?". Today the answer needs Game
Vault open and a search. It should appear where the question is asked:

- on a store's product page, just before buying;
- on a bundle page, before deciding whether the bundle is worth it;
- for any list of titles pasted from elsewhere.

## Decisions

- **Stores in v1:** Steam, GOG, Humble Bundle and Fanatical.
  - Product pages get an "in your collection" label.
  - Humble and Fanatical bundle pages mark each game you own and show a summary.
- **Identification:** the extension authenticates to the user's own Game Vault with a read-only
  access token. On trusted networks no token is needed, as for the web UI.
- **Display:** a small label injected next to the title or buy button, isolated in a shadow root.
  Clicking it opens the game's sheet in Game Vault.
- **Bundle checker in the web UI:** a paste box for any list of titles, using the same lookup as the
  extension.
- **Privacy:** the extension only reads the page being viewed. It sends nothing but the identifiers
  or titles on that page, and only to the user's own server. It never sends requests to the store.

## Server

### Access tokens

- System → Security gets an "Access tokens" section:
  - create a token with a name (e.g. "Extension, laptop"), shown **once** after creation;
  - list the tokens with their name, created time and last-used time;
  - revoke any of them.
- **Storage:** only a hash of the token is stored, never the token itself. The token is random, at
  least 32 bytes, with a recognizable prefix (`gvt_`) so leaked tokens are easy to spot.
- **Scope:** a token grants only the ownership lookup. Any other RPC called with a token is refused
  as unauthenticated.
- **Transport:** `Authorization: Bearer gvt_…`. The existing auth middleware checks it. Requests
  through a proxy or with a host name still follow the security model: a token is a credential like
  a password, never a reason to trust the network.
- **Brute force:** failed token attempts count towards the existing sign-in rate limiting.
- **Auditing:** each successful use updates "last used" at most once a minute.

### Ownership lookup

`OwnershipService/CheckOwnership(items) → results`, one result per item, in order.

- **Item:** either `{store, id}` (e.g. `steam` + `620`, `gog` + product id) or `{title}`, or both.
  At most 200 items per request.
- **Matching, in order:**
  1. a store link (including automatic links) equal to `{store, id}`;
  2. then the word-based exact title match (`game.SameTitle`) on the title, ignoring edition and
     store tags only where the existing rules allow;
  3. a single match found only after dropping edition and store tags is **possible**, never owned.
- **Status:**
  - **owned** when exactly one game matches;
  - **possible** with up to 5 candidates when several match;
  - **not owned** otherwise.
- **For an owned game:** its id, title, and a summary of its copies: platform, system, kind
  (library, key, physical), and whether a key is pending (unredeemed), so a pending key reads
  differently from an owned copy. Also a relative link to its sheet (`#/game/<id>`).
- **No side effects:** read-only. No provider or network calls, nothing saved.

## Extension (Game Vault Connector)

### Settings (popup)

- **Access token:** a new field next to the Game Vault address, optional. It is stored in the
  extension's local storage.
- **Toggle "Show what I own on store pages":** turning it on asks for host permission for each of
  the four store sites. Without permission the extension does nothing on them.

### Content scripts

- **Registration:** one content script per store, registered dynamically only for granted hosts.
- **Reading the page:**
  - Steam takes the AppID from `store.steampowered.com/app/<id>`;
  - GOG takes the product id or title from the product page;
  - Humble and Fanatical take the game titles from product and bundle pages.

  What to read is described as data (selectors and patterns per store), not ad-hoc code, so a site
  change is a small fix.
- **Page changes:** single-page navigation and late-loaded content are followed with a
  MutationObserver, debounced.
- **Lookup:** the content script asks the background, and the background calls the user's server
  with the token. Results are cached per page for a short time.
- **Product label:**
  - "In your collection" with the platform badges and kinds, e.g. "PS4 · disc", "Steam · key not
    redeemed";
  - or a quieter "Not in your collection";
  - "Possible: …" when the match was ambiguous.

  Clicking it opens the game's sheet in a new tab.
- **Bundle pages:** a small mark on each owned game ("Owned on PC") and a summary at the top ("You
  own 4 of 9").
- **Server unreachable or token rejected:** a discreet "Game Vault unavailable" label, never "Not in
  your collection".
- **Isolation:** labels live in a shadow root with their own styles, and the extension's styles never
  leak into the store's page.
- **Texts:** follow the browser language (en, es), as the extension already does or will.

### Privacy

`extension/PRIVACY.md` is updated: what is read on store pages, what is sent, and that it only goes
to the user's server.

## Web: bundle checker

- **Entry point:** a "Do I own it?" button in the library opens a dialog.
- **Input:** a text area. Pasted text is cleaned:
  - one title per line;
  - bullets, numbering, prices and currency symbols, "DLC"/"Soundtrack" markers, and empty or
    duplicate lines are removed.

  The cleaning is a pure helper with Node tests.
- **Results:** one row per title:
  - **owned**, with badges, the pending-key state and a link to the sheet;
  - **possible**, with its candidates;
  - **not owned**.

  A summary ("You own 4 of 9") and a "Copy the ones I don't own" button.
- **Nothing is saved.** On phones the dialog is full screen, with the input above the results.
- Every text is in en and es.

## Testing

- **Go:**
  - tokens: create, list, revoke, only the hash stored, the prefix, scope (any other RPC refused),
    last used, brute-force counting;
  - lookup: by link, by automatic link, by title, ambiguous, not owned, the item limit, the
    pending-key summary;
  - RPC end to end with and without a token on a non-trusted request.
- **Extension (Node, fake `chrome`):** each store's reader against saved sample pages (invented
  titles), the label states (owned, possible, not owned, unavailable), and permission gating.
- **Web (Node):** the paste cleaning and the result grouping.
- **Browser:**
  - the bundle checker and the tokens section on a test server, desktop and mobile;
  - the extension in Chrome on real public Steam, GOG, Humble and Fanatical pages, read only and
    signed out.

## Delivery

Branch `feature/ownership-check`, one PR to `main`. Also:
- docs: `docs/technical.md` (tokens, the ownership lookup, the extension's store labels);
- `extension/README.md` and `extension/PRIVACY.md`;
- `README.md`: one line in the feature list;
- memory: `connector.md` and `security-model.md`.
