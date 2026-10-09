// The bridge content script registered per enabled Game Vault address.

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
  const { origins = [] } = await chrome.storage.local.get('origins');
  if (!origins.includes(origin)) await chrome.storage.local.set({ origins: [...origins, origin] });
  await registerBridge(origin);
  if (tabId !== undefined) await chrome.scripting.executeScript({ target: { tabId }, files: ['bridge.js'] }).catch(() => {});
}
