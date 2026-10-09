# Game Vault Connector — privacy

- **What it reads:** only what a sign-in recipe from your Game Vault names: a cookie, the cookies
  sent to one address, a value a site keeps in its local storage, a parameter of the address a store
  redirects to after sign-in, or a field of a page fetched with your session. It reads them only on
  store hosts you allowed for that Game Vault address, and only after you started a sign-in.
- **Where it sends it:** only to the Game Vault tab that asked, in the same browser, as the answer
  to that request. Game Vault then saves it on your own server, as if you had pasted it.
- **What it stores:** the Game Vault addresses you enabled and the store hosts each may read. It
  never stores, logs or transmits credentials anywhere else.
- **No remote code, no analytics, no third parties.** Recipes are data the extension validates; it
  runs no code it receives.
