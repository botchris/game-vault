// Game Vault Connector: runs sign-in recipes for the Game Vault addresses the user enabled. It
// knows no store: recipes come from Game Vault as data and are validated before anything opens.
// Captured values go only to the tab that asked, in the reply; nothing is stored or logged.

import { startRun } from './lib/engine.js';
import { RECIPE_VERSION, validate } from './lib/recipe.js';
import { hostsGranted, originAllowed, rememberAllowed } from './lib/capture.js';
import { enableOrigin, registerBridge } from './lib/bridges.js';

const running = new Map(); // request id → { cancel, tab, port }
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
    // Single-page sign-ins change the address without loading a page.
    chrome.webNavigation.onHistoryStateUpdated.addListener(completed);
    chrome.tabs.onRemoved.addListener(removed);
    return () => {
      chrome.webNavigation.onCommitted.removeListener(committed);
      chrome.webNavigation.onCompleted.removeListener(completed);
      chrome.webNavigation.onHistoryStateUpdated.removeListener(completed);
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
  async readStorage(tabId, key, origin) {
    // The tab may have moved to another site since its address was checked: the page itself
    // confirms its origin before giving anything away.
    const [{ result }] = await chrome.scripting.executeScript({
      target: { tabId },
      func: (k, o) => (location.origin === o ? localStorage.getItem(k) ?? '' : ''),
      args: [key, origin],
    });
    return result ?? '';
  },
  async fetchText(url) { return (await fetch(url, { credentials: 'include' })).text(); },
  async closeTab(tabId) { try { await chrome.tabs.remove(tabId); } catch { /* already closed */ } },
  setTimer(ms, fn) { const t = setTimeout(fn, ms); return () => clearTimeout(t); },
  setInterval(ms, fn) { const t = setInterval(fn, ms); return () => clearInterval(t); },
};

// Asks the user, in the extension's own window, to let this Game Vault address read sessions on
// these hosts; the browser's host permission is requested from that window (a user gesture).
// onWindow receives the window's id, so a cancelled request can close it.
async function ensureGranted(origin, hosts, onWindow) {
  const grants = (await chrome.storage.local.get('grants')).grants ?? {};
  const known = grants[origin] ?? [];
  const origins = hosts.map((h) => `https://${h}/*`);
  if (hostsGranted(hosts, known) && await chrome.permissions.contains({ origins })) return;
  const id = crypto.randomUUID();
  const query = new URLSearchParams({ id, origin, hosts: hosts.join(',') });
  const answer = await new Promise((resolve, reject) => {
    chrome.windows.create({ url: chrome.runtime.getURL(`confirm.html?${query}`), type: 'popup', width: 460, height: 320 })
      .then((w) => { pending.set(id, { resolve, windowId: w.id }); onWindow(w.id); }, reject);
  });
  if (!answer.allow) throw coded('denied', 'access was not allowed');
  // Plain http on the network can be impersonated by anyone on it: such an address asks every time.
  if (answer.remember && rememberAllowed(origin)) {
    grants[origin] = [...new Set([...known, ...hosts])];
    await chrome.storage.local.set({ grants });
  }
}

chrome.windows.onRemoved.addListener((windowId) => {
  for (const [id, p] of pending) if (p.windowId === windowId) { pending.delete(id); p.resolve({ allow: false }); }
});

async function handle(msg, port) {
  const sender = port.sender;
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
      // Registered before anything opens, so a cancel during the confirmation is not lost.
      let run;
      let confirmWindow;
      let abort;
      const aborted = new Promise((_, rej) => { abort = rej; });
      aborted.catch(() => {});
      running.set(msg.id, {
        tab: sender.tab.id,
        port,
        cancel: () => {
          run?.cancel();
          if (confirmWindow !== undefined) chrome.windows.remove(confirmWindow).catch(() => {});
          abort(coded('cancelled', 'cancelled'));
        },
      });
      try {
        await Promise.race([ensureGranted(origin, hosts, (id) => { confirmWindow = id; }), aborted]);
        confirmWindow = undefined;
        run = startRun(msg.recipe, browserApi);
        return { op: 'result', value: await Promise.race([run.result, aborted]) };
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
  // The page may be gone by the time a sign-in ends.
  const send = (reply) => { try { port.postMessage(reply); } catch { /* the page went away */ } };
  port.onMessage.addListener((msg) => {
    if (msg?.op === 'ping') return; // the bridge keeping this worker awake
    handle(msg, port).then(send, (e) => send({ op: 'error', code: e.code ?? 'failed', message: e.message }));
  });
  // A page closed or reloaded mid sign-in: nobody is waiting for the value any more.
  port.onDisconnect.addListener(() => {
    for (const r of running.values()) if (r.port === port) r.cancel();
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

// Registered bridges survive restarts; re-register them in case the browser dropped them (it
// does on an update or a reload of the extension).
const reregister = async () => {
  for (const origin of await enabledOrigins()) await registerBridge(origin).catch(() => {});
};
chrome.runtime.onStartup?.addListener(reregister);
chrome.runtime.onInstalled?.addListener(reregister);

// Firefox closes the popup when the browser asks for a permission, so the popup leaves the
// address it was enabling here and the background finishes the job once it is granted.
chrome.permissions.onAdded?.addListener(async (added) => {
  const { pendingEnable } = await chrome.storage.local.get('pendingEnable');
  if (!pendingEnable) return;
  const { protocol, hostname } = new URL(pendingEnable.origin);
  if (!(added.origins ?? []).includes(`${protocol}//${hostname}/*`)) return;
  await chrome.storage.local.remove('pendingEnable');
  await enableOrigin(pendingEnable.origin, pendingEnable.tabId).catch(() => {});
});
