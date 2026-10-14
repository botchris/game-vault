// The bridge content script registered per enabled Game Vault address.
import { isStoreSite } from './storesites.js';

/** The registration id of an address's bridge. */
export function bridgeId(origin) { return `bridge-${origin.replace(/[^a-z0-9]/gi, '_')}`; }

/** Registers the bridge on an address (a match pattern has no port: the background checks it). */
export async function registerBridge(origin) {
  const { protocol, hostname } = new URL(origin);
  const id = bridgeId(origin);
  const existing = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
  if (existing.length) return;
  await chrome.scripting.registerContentScripts([{ id, matches: [`${protocol}//${hostname}/*`], js: ['bridge.js'], runAt: 'document_start', persistAcrossSessions: true }]);
}

/**
 * Enables the extension on a Game Vault address once its permission is granted: remembers it,
 * registers its bridge and puts the bridge in the tab already open there. Running it twice is
 * harmless (the popup and the background may both finish the same enable).
 */
export async function enableOrigin(origin, tabId) {
  // A store's page with the bridge could ask for sign-ins, and become the token's destination.
  if (isStoreSite(origin)) throw new Error('a store site is never a Game Vault address');
  const { origins = [] } = await chrome.storage.local.get('origins');
  if (!origins.includes(origin)) await chrome.storage.local.set({ origins: [...origins, origin] });
  await registerBridge(origin);
  if (tabId !== undefined) await chrome.scripting.executeScript({ target: { tabId }, files: ['bridge.js'] }).catch(() => {});
}

/**
 * Forgets the store sites an older version let the user enable as Game Vault addresses: their
 * place in the enabled addresses and in the remembered sign-in confirmations, and their bridges.
 * When the store-page option points at one, its token is cleared and the option turned off.
 */
export async function forgetStoreSites() {
  const { origins = [] } = await chrome.storage.local.get('origins');
  const { grants = {} } = await chrome.storage.local.get('grants');
  const { ownership } = await chrome.storage.local.get('ownership');
  const stores = [...new Set([...origins, ...Object.keys(grants)])].filter(isStoreSite);
  if (stores.length) {
    const kept = Object.fromEntries(Object.entries(grants).filter(([o]) => !isStoreSite(o)));
    await chrome.storage.local.set({ origins: origins.filter((o) => !isStoreSite(o)), grants: kept });
    await chrome.scripting.unregisterContentScripts({ ids: stores.map(bridgeId) }).catch(() => {});
  }
  if (ownership?.origin && isStoreSite(ownership.origin) && (ownership.enabled || ownership.token)) {
    await chrome.storage.local.set({ ownership: { ...ownership, enabled: false, token: '' } });
  }
}
