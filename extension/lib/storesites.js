// Sites that are never a Game Vault: every store's shop (the store pages with labels, data in
// ownership/stores.js) and the sign-in, account and community sites of the stores. The extension
// never offers the Game Vault bridge on them, never enables them as a Game Vault address, never
// lets them ask for a sign-in and never sends them the token. Data: when a sign-in recipe opens or
// reads a site under a domain missing here, add the domain (and to tests/store-sites.test.mjs).
import '../ownership/stores.js';

/** The stores' domains: each one and every site under it. */
export const STORE_DOMAINS = [
  // Steam
  'steampowered.com', 'steamcommunity.com',
  // GOG
  'gog.com',
  // Humble Bundle, Fanatical, itch.io
  'humblebundle.com', 'fanatical.com', 'itch.io',
  // Epic Games
  'epicgames.com',
  // Ubisoft
  'ubisoft.com', 'ubi.com',
  // PlayStation
  'playstation.com', 'sony.com',
  // EA
  'ea.com',
  // Battle.net
  'battle.net', 'blizzard.com',
  // Xbox / Microsoft
  'live.com', 'xbox.com', 'xboxlive.com', 'microsoft.com',
  // Amazon Games
  'amazon.com',
];

/** Whether a web address is a store's site (see above): never a Game Vault. */
export function isStoreSite(url) {
  let host;
  try {
    host = new URL(url).hostname;
  } catch {
    return false;
  }
  if (globalThis.gvOwnership.storeForUrl(url)) return true;
  return STORE_DOMAINS.some((d) => host === d || host.endsWith(`.${d}`));
}
