// The background's side of "what you own on store pages": which store sites get the content
// scripts, and the lookups they ask for, sent only to the user's own Game Vault. Pages never see
// the token: only this module talks to Game Vault.
import '../ownership/stores.js';
import { isPrivateTransport } from './origin.js';
import { isStoreSite } from './storesites.js';

const { STORES } = globalThis.gvOwnership;

/** The store a page address belongs to, or null. */
export const storeForUrl = (url) => globalThis.gvOwnership.storeForUrl(url);

/** Most items in one request (the server's limit). */
export const MAX_ITEMS = 200;

/** Most items one page may ask about; the rest read as unavailable. */
export const MAX_PAGE_ITEMS = 2000;

/** How long an answer is reused for the same item, in milliseconds. */
export const CACHE_MS = 60_000;

const SCRIPTS = ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js'];
const PROCEDURE = '/gamevault.v1.OwnershipService/CheckOwnership';
const SHEET = /^#\/game\/[A-Za-z0-9-]{1,64}$/;

/** The host permission a store needs. */
export const storeOrigin = (store) => (store.domain ? `https://*.${store.domain}/*` : `https://${store.host}/*`);

/** Every store's host permission, asked for together when the option is turned on. */
export const STORE_ORIGINS = STORES.map(storeOrigin);

/** The id of a store's registered content script. */
export const scriptId = (store) => `ownership-${store.store}`;

/** The content script registration of a store. */
export function scriptFor(store) {
  return { id: scriptId(store), matches: [storeOrigin(store)], js: SCRIPTS, runAt: 'document_idle', persistAcrossSessions: true };
}

/** The names of the stores the browser allows the extension on. */
export async function grantedStores(api) {
  const out = [];
  for (const s of STORES) if (await api.permissions.contains({ origins: [storeOrigin(s)] })) out.push(s.name);
  return out;
}

/** The store host permissions no sign-in needs: those no Game Vault address has a confirmed sign-in on. */
export function removableOrigins(grants) {
  const used = Object.values(grants ?? {}).flat();
  return STORES.filter((s) => !used.some((host) => globalThis.gvOwnership.storeHasHost(s, host))).map(storeOrigin);
}

// The syncs run one after the other: turning the option on fires the settings change, the
// permission grant and more at almost the same time, and two syncs reading the registrations
// together would both register the same script (the browser refuses the second: "Duplicate
// script ID").
let syncing = Promise.resolve();

/**
 * Registers the content script of each store the browser allows while the option is on for an
 * address that is still enabled, puts it in that store's open tabs that do not run it yet, and
 * unregisters the rest. Each sync starts after the previous
 * one has finished, and a store the browser refuses does not stop the others: its failure is
 * logged (the store only, never a setting).
 */
export function syncStoreScripts(api, { tabTimeoutMs = TAB_TIMEOUT_MS } = {}) {
  const run = syncing.then(() => syncOnce(api, tabTimeoutMs));
  syncing = run.catch(() => {});
  return run;
}

/**
 * The store-page option's settings while it is on, for an address still enabled in the extension:
 * {origin, token} (the token trimmed, '' when none), else null. Both the content scripts'
 * registration and the lookups follow it, so they never disagree about whether the option is on.
 */
export async function activeSettings(storage) {
  const { ownership } = await storage.get('ownership');
  const { origins = [] } = await storage.get('origins');
  if (!ownership?.enabled || !ownership.origin || !origins.includes(ownership.origin)) return null;
  // A store's site is never a Game Vault: the token never goes there.
  if (isStoreSite(ownership.origin)) return null;
  return { origin: ownership.origin, token: typeof ownership.token === 'string' ? ownership.token.trim() : '' };
}

/**
 * Puts the store scripts in the open tabs of a store where they do not run yet. The browser only
 * gives a registered content script to pages loaded after the registration: without this, the
 * store page the user turns the option on from, every store tab already open, and the tabs whose
 * scripts belong to a version of the extension that was since reloaded or updated (those can no
 * longer reach it) would show nothing until they are reloaded. A short probe in the extension's
 * own world of the tab (it reads nothing from the page) skips the tabs where the scripts already
 * run; a tab the browser does not let the extension into (an error page) is skipped too.
 */
async function reachOpenTabs(api, store, timeoutMs) {
  await eachTab(api, store, timeoutMs, async (tab) => {
    // injectImmediately: a tab still loading (or never finishing) does not hold the probe up.
    const [probe] = await api.scripting.executeScript({
      target: { tabId: tab.id },
      injectImmediately: true,
      func: () => Boolean(globalThis.__gvOwnershipStarted),
    });
    if (probe?.result) return;
    // Into the document the probe looked at: if the tab moved on meanwhile, the new page gets the
    // registered scripts as it loads, and this injection is refused instead of doubling them.
    const target = probe?.documentId ? { tabId: tab.id, documentIds: [probe.documentId] } : { tabId: tab.id };
    await api.scripting.executeScript({ target, files: SCRIPTS });
  });
}

/** The message that tells the store scripts in a tab to stop and take their labels away. */
export const STOP_MESSAGE = { op: 'ownership-off' };

/**
 * Tells the scripts already running in a store's open tabs to stop and take their labels away:
 * the option was turned off, or the browser took the store site back. Unregistering a script
 * only keeps it out of pages loaded later. A tab without the scripts has no one to answer.
 */
async function stopOpenTabs(api, store, timeoutMs) {
  await eachTab(api, store, timeoutMs, (tab) => api.tabs.sendMessage(tab.id, STOP_MESSAGE));
}

/** The longest the work on one tab may take during a sync, in milliseconds. */
export const TAB_TIMEOUT_MS = 4000;

/**
 * Runs work(tab) on every open tab of a store, all at once, each given at most timeoutMs: the
 * syncs run one after the other, so one frozen tab must not hold up every later sync. A tab that
 * fails or takes too long is left as it is (an error page, a tab the extension may not script).
 */
async function eachTab(api, store, timeoutMs, work) {
  let tabs;
  try {
    tabs = await api.tabs.query({ url: storeOrigin(store) });
  } catch {
    return;
  }
  await Promise.allSettled(tabs.map((tab) => {
    let timer;
    const late = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('the tab did not answer')), timeoutMs); });
    return Promise.race([Promise.resolve().then(() => work(tab)), late]).finally(() => clearTimeout(timer));
  }));
}

async function syncOnce(api, tabTimeoutMs) {
  const on = (await activeSettings(api.storage.local)) !== null;
  const registered = new Map((await api.scripting.getRegisteredContentScripts({ ids: STORES.map(scriptId) })).map((s) => [s.id, s]));
  for (const store of STORES) {
    const id = scriptId(store);
    try {
      const want = scriptFor(store);
      const wanted = on && await api.permissions.contains({ origins: [storeOrigin(store)] });
      const old = registered.get(id);
      // Registrations outlive updates of the extension: one from an older version (other sites or
      // files) is replaced.
      const stale = old && (String(old.matches) !== String(want.matches) || String(old.js) !== String(want.js));
      if (old && (!wanted || stale)) await api.scripting.unregisterContentScripts({ ids: [id] });
      if (old && !wanted) await stopOpenTabs(api, store, tabTimeoutMs);
      if (wanted && (!old || stale)) await api.scripting.registerContentScripts([want]);
      if (wanted) await reachOpenTabs(api, store, tabTimeoutMs);
    } catch (err) {
      console.warn(`ownership: syncing the ${store.store} store script failed:`, String(err?.message ?? err));
    }
  }
}

/**
 * What the popup offers for the address of the tab it is opened on: {kind: 'store', store} on a
 * store page (its labels follow the option; the Game Vault bridge is never offered there: it would
 * let the store's page ask for sign-ins); {kind: 'signin'} on another store site (sign-in,
 * account, community: lib/storesites.js) or a host confirmed for a sign-in (`grants`), never a
 * Game Vault either; {kind: 'enabled'} on an enabled Game Vault address; {kind: 'offer'} on any
 * other web address; {kind: 'none'} off the web. Stores are checked first: a store site enabled by
 * an older version is still a store. Every kind but 'none' carries the address's origin.
 */
export function popupPlace(url, origins, grants) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return { kind: 'none' };
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) return { kind: 'none' };
  const { origin, hostname } = parsed;
  const store = storeForUrl(url);
  if (store) return { kind: 'store', origin, store: store.name };
  if (isStoreSite(url) || Object.values(grants ?? {}).flat().includes(hostname)) return { kind: 'signin', origin };
  if ((origins ?? []).includes(origin)) return { kind: 'enabled', origin };
  return { kind: 'offer', origin };
}

/**
 * Whether a token sent to this Game Vault address would travel unencrypted over a network: plain
 * http to anything but this computer (see isLoopback). The popup warns about it.
 */
export function tokenInClear(origin) {
  try {
    return new URL(origin).protocol === 'http:' && !isPrivateTransport(origin);
  } catch {
    return false;
  }
}

/**
 * The store-page settings after choosing a Game Vault address (by the user, or because the saved
 * one was removed). A token belongs to the address it was saved for: another address forgets it
 * and turns the option off, so it is never sent to another server.
 */
export function nextOwnership(prev, origin) {
  if (prev && prev.origin === origin) return prev;
  return { ...prev, enabled: false, origin, token: '' };
}

/** Only what a page shows is sent: a store's id for the game, its title. */
function cleanItem(it) {
  const text = (v, n) => (typeof v === 'string' ? v.replace(/\s+/g, ' ').trim().slice(0, n) : '');
  const out = {};
  const store = text(it?.store, 32).toLowerCase();
  const id = text(it?.id, 200);
  const title = text(it?.title, 300);
  if (store && id) {
    out.store = store;
    out.id = id;
  }
  if (title) out.title = title;
  return out;
}

// The token is part of the key (in memory only): an answer given to another token is never reused.
const keyOf = (origin, token, item) => [origin, token, item.store ?? '', item.id ?? '', (item.title ?? '').toLowerCase()].join('\u0000');

/** The sheet's address on the chosen Game Vault; anything else becomes its home page. */
const sheetUrl = (origin, path) => (SHEET.test(path ?? '') ? `${origin}/${path}` : `${origin}/`);

function gameOf(g, origin) {
  return {
    title: String(g?.title ?? ''),
    url: sheetUrl(origin, g?.path),
    copies: (g?.copies ?? []).map((c) => ({
      platform: String(c?.platform ?? ''),
      system: String(c?.system ?? ''),
      kind: String(c?.kind ?? ''),
      pendingKey: Boolean(c?.pendingKey),
    })),
  };
}

function resultOf(r, origin) {
  const status = ['owned', 'possible', 'not_owned'].includes(r?.status) ? r.status : 'not_owned';
  return {
    status,
    game: status === 'owned' && r.game ? gameOf(r.game, origin) : null,
    candidates: (r?.candidates ?? []).map((c) => gameOf(c, origin)),
  };
}

/**
 * The lookups store pages ask for. storage is chrome.storage.local (or a fake), fetch and now are
 * injected for tests. Answers are cached per item for CACHE_MS; clear() forgets them (settings
 * changed). A Game Vault that cannot answer is "unavailable", never "not owned".
 */
export function createLookup({ storage, fetch, now }) {
  const cache = new Map();
  // Bumped by clear(): a check that was waiting for Game Vault when the settings changed does not
  // cache what it gets.
  let generation = 0;
  const unavailable = (reason) => ({ status: 'unavailable', reason });
  const clear = () => {
    cache.clear();
    generation++;
  };

  return {
    clear,

    async check(raw) {
      const started = generation;
      const active = await activeSettings(storage);
      if (!active) return unavailable('setup');
      const { origin, token } = active;
      const items = (Array.isArray(raw) ? raw : []).slice(0, MAX_PAGE_ITEMS).map(cleanItem);
      const results = new Array(items.length);
      const missing = [];
      items.forEach((item, i) => {
        const hit = cache.get(keyOf(origin, token, item));
        if (hit && now() - hit.at < CACHE_MS) results[i] = hit.result;
        else missing.push(i);
      });

      for (let start = 0; start < missing.length; start += MAX_ITEMS) {
        const part = missing.slice(start, start + MAX_ITEMS);
        let res;
        try {
          res = await fetch(origin + PROCEDURE, {
            method: 'POST',
            credentials: 'omit',
            // A redirect (an authenticating proxy sending to its sign-in page) would carry the
            // token to another address: fetch rejects instead, and Game Vault reads as unreachable.
            redirect: 'error',
            headers: {
              'Content-Type': 'application/json',
              'Connect-Protocol-Version': '1',
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({ items: part.map((i) => items[i]) }),
          });
        } catch {
          return unavailable('unreachable');
        }
        if (res.status === 401) {
          clear();
          return unavailable(token ? 'token' : 'signin');
        }
        if (!res.ok) return unavailable('unreachable');
        let got;
        try {
          got = (await res.json())?.results;
        } catch {
          return unavailable('unreachable');
        }
        if (!Array.isArray(got) || got.length !== part.length) return unavailable('unreachable');
        part.forEach((idx, j) => {
          results[idx] = resultOf(got[j], origin);
          if (generation === started) cache.set(keyOf(origin, token, items[idx]), { result: results[idx], at: now() });
        });
      }
      return { status: 'ok', results };
    },
  };
}
