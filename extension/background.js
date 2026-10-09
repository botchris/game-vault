// Game Vault Connector: runs sign-in recipes for the Game Vault addresses the user enabled. It
// knows no store: recipes come from Game Vault as data and are validated before anything opens.
// Captured values go only to the tab that asked, in the reply; nothing is stored or logged.

import { startRun } from './lib/engine.js';
import { RECIPE_VERSION, validate } from './lib/recipe.js';
import { hostsGranted, originAllowed } from './lib/capture.js';
import { registerBridge } from './lib/bridges.js';

const running = new Map(); // request id → { cancel, tab }
const pending = new Map(); // confirmation id → { resolve, windowId }

const coded = (code, message) => Object.assign(new Error(message ?? code), { code });

async function enabledOrigins() {
  return (await chrome.storage.local.get('origins')).origins ?? [];
}

async function cookieStore(tabId) {
  const stores = await chrome.cookies.getAllCookieStores();
  return stores.find((s) => s.tabIds.includes(tabId))?.id;
}

const browserApi = {
  async openTab(url, privateWindow) {
    if (privateWindow && await chrome.extension.isAllowedIncognitoAccess()) {
      const w = await chrome.windows.create({ url, incognito: true });
      return w.tabs[0].id;
    }
    return (await chrome.tabs.create({ url })).id;
  },
  watchTab(tabId, handler) {
    const committed = (d) => d.tabId === tabId && d.frameId === 0 && handler({ kind: 'committed', url: d.url });
    const completed = (d) => d.tabId === tabId && d.frameId === 0 && handler({ kind: 'loaded', url: d.url });
    const removed = (id) => id === tabId && handler({ kind: 'removed' });
    chrome.webNavigation.onCommitted.addListener(committed);
    chrome.webNavigation.onCompleted.addListener(completed);
    chrome.tabs.onRemoved.addListener(removed);
    return () => {
      chrome.webNavigation.onCommitted.removeListener(committed);
      chrome.webNavigation.onCompleted.removeListener(completed);
      chrome.tabs.onRemoved.removeListener(removed);
    };
  },
  async tabURL(tabId) { return (await chrome.tabs.get(tabId)).url ?? ''; },
  async getCookie(url, name, tabId) {
    const storeId = await cookieStore(tabId);
    return (await chrome.cookies.get({ url, name, ...(storeId ? { storeId } : {}) }))?.value ?? '';
  },
  async getCookies(url, tabId) {
    const storeId = await cookieStore(tabId);
    return chrome.cookies.getAll({ url, ...(storeId ? { storeId } : {}) });
  },
  async readStorage(tabId, key) {
    const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: (k) => localStorage.getItem(k) ?? '', args: [key] });
    return result ?? '';
  },
  async fetchText(url) { return (await fetch(url, { credentials: 'include' })).text(); },
  async closeTab(tabId) { try { await chrome.tabs.remove(tabId); } catch { /* already closed */ } },
  setTimer(ms, fn) { const t = setTimeout(fn, ms); return () => clearTimeout(t); },
};

// Asks the user, in the extension's own window, to let this Game Vault address read sessions on
// these hosts; the browser's host permission is requested from that window (a user gesture).
async function ensureGranted(origin, hosts) {
  const grants = (await chrome.storage.local.get('grants')).grants ?? {};
  const known = grants[origin] ?? [];
  const origins = hosts.map((h) => `https://${h}/*`);
  if (hostsGranted(hosts, known) && await chrome.permissions.contains({ origins })) return;
  const id = crypto.randomUUID();
  const query = new URLSearchParams({ id, origin, hosts: hosts.join(',') });
  const answer = await new Promise((resolve, reject) => {
    chrome.windows.create({ url: chrome.runtime.getURL(`confirm.html?${query}`), type: 'popup', width: 460, height: 320 })
      .then((w) => pending.set(id, { resolve, windowId: w.id }), reject);
  });
  if (!answer.allow) throw coded('denied', 'access was not allowed');
  if (answer.remember) {
    grants[origin] = [...new Set([...known, ...hosts])];
    await chrome.storage.local.set({ grants });
  }
}

chrome.windows.onRemoved.addListener((windowId) => {
  for (const [id, p] of pending) if (p.windowId === windowId) { pending.delete(id); p.resolve({ allow: false }); }
});

async function handle(msg, sender) {
  const origin = sender.origin ?? new URL(sender.url).origin;
  if (!sender.tab || !originAllowed(origin, await enabledOrigins())) throw coded('denied', 'this address is not enabled in the extension');
  switch (msg.op) {
    case 'hello':
      return { op: 'hello', version: chrome.runtime.getManifest().version, recipeVersion: RECIPE_VERSION };
    case 'cancel':
      running.get(msg.cancelId ?? msg.id)?.cancel();
      return { op: 'cancelled' };
    case 'connect': {
      if ([...running.values()].some((r) => r.tab === sender.tab.id)) throw coded('busy', 'a sign-in is already running in this tab');
      const { hosts } = validate(msg.recipe);
      await ensureGranted(origin, hosts);
      const run = startRun(msg.recipe, browserApi);
      running.set(msg.id, { cancel: run.cancel, tab: sender.tab.id });
      try {
        return { op: 'result', value: await run.result };
      } finally {
        running.delete(msg.id);
      }
    }
    default:
      throw coded('invalid', `unknown request ${msg.op}`);
  }
}

chrome.runtime.onConnect.addListener((port) => {
  if (port.name !== 'gamevault') return;
  port.onMessage.addListener((msg) => {
    handle(msg, port.sender).then(
      (reply) => port.postMessage(reply),
      (e) => port.postMessage({ op: 'error', code: e.code ?? 'failed', message: e.message }),
    );
  });
});

// Answers of the confirmation window (an extension page).
chrome.runtime.onMessage.addListener((msg, sender) => {
  if (msg?.op !== 'confirm' || !sender.url?.startsWith(chrome.runtime.getURL('confirm.html'))) return;
  const p = pending.get(msg.id);
  if (!p) return;
  pending.delete(msg.id);
  p.resolve({ allow: Boolean(msg.allow), remember: Boolean(msg.remember) });
});

// Registered bridges survive restarts; re-register them in case the browser dropped them.
chrome.runtime.onStartup?.addListener(async () => {
  for (const origin of await enabledOrigins()) await registerBridge(origin).catch(() => {});
});
