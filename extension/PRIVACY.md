# Game Vault Connector — privacy

- **What it reads:** only what a sign-in recipe from your Game Vault names: a cookie, the cookies
  sent to one address, a value a site keeps in its local storage, a parameter of the address a store
  redirects to after sign-in, or a field of a page fetched with your session. It reads them only on
  store hosts you allowed for that Game Vault address, and only after you started a sign-in.
- **Where it sends it:** only to the Game Vault tab that asked, in the same browser, as the answer
  to that request. Game Vault then saves it on your own server, as if you had pasted it.
- **On store pages** (only with "Show what I own on store pages" on, and only on the Steam, GOG,
  Humble Bundle and Fanatical sites the browser allowed): it reads the page you are viewing, namely
  the game's id in its address or page and the game titles it shows, and sends only those, with your
  access token and without cookies, to the Game Vault address you chose. It never sends anything to
  the store, never reads your account there, and keeps the answers in memory for a minute only. The
  store's own page cannot see the answers: they are drawn in a closed, isolated part of the page.
  On Fanatical the labels only read the page you are viewing and store nothing.
- **What it stores:** the Game Vault addresses you enabled and the store hosts each may read, and the
  Game Vault address used for store pages with its access token (in the extension's local storage,
  bound to that address: changing the address clears the token). It
  never stores, logs or transmits credentials anywhere else.
- **No remote code, no analytics, no third parties.** Recipes are data the extension validates; it
  runs no code it receives.
