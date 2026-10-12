// The background's side of "what you own on store pages": which store sites get the content
// scripts, and the lookups they ask for, sent only to the user's own Game Vault. Pages never see
// the token: only this module talks to Game Vault.
import '../ownership/stores.js';

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

/**
 * Registers the content script of each store the browser allows while the option is on for an
 * address that is still enabled, and unregisters the rest.
 */
export async function syncStoreScripts(api) {
  const { ownership } = await api.storage.local.get('ownership');
  const { origins = [] } = await api.storage.local.get('origins');
  const on = Boolean(ownership?.enabled) && Boolean(ownership.origin) && origins.includes(ownership.origin);
  const registered = new Set((await api.scripting.getRegisteredContentScripts({ ids: STORES.map(scriptId) })).map((s) => s.id));
  for (const store of STORES) {
    const id = scriptId(store);
    const wanted = on && await api.permissions.contains({ origins: [storeOrigin(store)] });
    if (wanted && !registered.has(id)) await api.scripting.registerContentScripts([scriptFor(store)]);
    if (!wanted && registered.has(id)) await api.scripting.unregisterContentScripts({ ids: [id] });
  }
}

/**
 * Whether a token sent to this Game Vault address would travel unencrypted over a network: plain
 * http to anything but this computer (127.0.0.0/8, ::1, localhost). The popup warns about it.
 */
export function tokenInClear(origin) {
  let url;
  try {
    url = new URL(origin);
  } catch {
    return false;
  }
  if (url.protocol !== 'http:') return false;
  const host = url.hostname;
  return !(host === 'localhost' || host === '[::1]' || /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(host));
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
      const { ownership } = await storage.get('ownership');
      const { origins = [] } = await storage.get('origins');
      if (!ownership?.enabled || !ownership.origin || !origins.includes(ownership.origin)) return unavailable('setup');
      const origin = ownership.origin;
      const token = typeof ownership.token === 'string' ? ownership.token.trim() : '';
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
