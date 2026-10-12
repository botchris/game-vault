// The background's store-page side against a fake `chrome`: which content scripts it registers,
// and who gets lookups. Each test imports a fresh copy of background.js after installing its fake.
import assert from 'node:assert/strict';
import { test } from 'node:test';

const GV = 'http://127.0.0.1:8093';
const STEAM = 'https://store.steampowered.com/*';
const GOG = 'https://www.gog.com/*';
const flush = async () => { for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r)); };

function ev() {
  const listeners = [];
  return {
    listeners,
    addListener: (fn) => listeners.push(fn),
    removeListener: (fn) => { const i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1); },
    fire: (...args) => listeners.slice().forEach((fn) => fn(...args)),
  };
}

function fakeChrome({ data = {}, granted = [], setAccessLevel } = {}) {
  const c = {
    data,
    granted: new Set(granted),
    registered: new Map(),
    runtime: {
      id: 'ext',
      onConnect: ev(), onMessage: ev(), onStartup: ev(), onInstalled: ev(),
      getManifest: () => ({ version: '1.1.0' }),
      getURL: (p) => `chrome-extension://ext/${p}`,
    },
    storage: {
      local: {
        get: async (k) => ({ [k]: data[k] }),
        set: async (o) => { Object.assign(data, o); },
        remove: async (k) => { delete data[k]; },
        ...(setAccessLevel ? { setAccessLevel } : {}),
      },
      onChanged: ev(),
    },
    windows: { onRemoved: ev(), create: async () => ({ id: 1, tabs: [{ id: 2 }] }), remove: async () => {} },
    permissions: {
      onAdded: ev(),
      onRemoved: ev(),
      contains: async ({ origins }) => origins.every((o) => c.granted.has(o)),
    },
    tabs: { onRemoved: ev() },
    webNavigation: { onCommitted: ev(), onCompleted: ev(), onHistoryStateUpdated: ev() },
    scripting: {
      registerContentScripts: async (list) => { for (const s of list) c.registered.set(s.id, s); },
      unregisterContentScripts: async ({ ids }) => { for (const id of ids) c.registered.delete(id); },
      getRegisteredContentScripts: async ({ ids } = {}) => [...c.registered.values()].filter((s) => !ids || ids.includes(s.id)),
      executeScript: async () => [],
    },
    cookies: { getAllCookieStores: async () => [] },
    extension: { isAllowedIncognitoAccess: async () => false },
  };
  return c;
}

let loaded = 0;
async function load(options) {
  const chrome = fakeChrome(options);
  globalThis.chrome = chrome;
  await import(`../background.js?ownership=${loaded++}`);
  return chrome;
}

const ownershipScripts = (chrome) => [...chrome.registered.keys()].filter((id) => id.startsWith('ownership-')).sort();
const changed = (chrome) => chrome.storage.onChanged.fire({ ownership: { newValue: chrome.data.ownership } }, 'local');

/** Sends a message to the background as a content script would and waits for the answer. */
function ask(chrome, msg, sender) {
  return new Promise((resolve) => {
    const async = chrome.runtime.onMessage.listeners.map((fn) => fn(msg, sender, resolve));
    if (!async.includes(true)) setImmediate(() => resolve(undefined));
  });
}

test('turning the option on registers the store scripts only where the browser allowed them', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM, GOG] });
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-gog', 'ownership-steam']);
  assert.deepEqual(chrome.registered.get('ownership-steam').matches, [STEAM]);
});

test('a permission taken back in the browser, or the option turned off, removes the scripts', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM] });
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-steam']);

  chrome.granted.delete(STEAM);
  chrome.permissions.onRemoved.fire({ origins: [STEAM] });
  await flush();
  assert.deepEqual(ownershipScripts(chrome), []);

  chrome.granted.add(STEAM);
  chrome.permissions.onAdded.fire({ origins: [STEAM] });
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-steam']);

  chrome.data.ownership = { ...chrome.data.ownership, enabled: false };
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), []);
});

test('a store page gets its answer from the chosen Game Vault through the background', async (t) => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV, token: 'gvt_test' } } });
  const sent = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    sent.push({ url, auth: init.headers.Authorization });
    return { ok: true, status: 200, json: async () => ({ results: JSON.parse(init.body).items.map(() => ({ status: 'not_owned' })) }) };
  };
  t.after(() => { globalThis.fetch = realFetch; });
  const answer = await ask(chrome, { op: 'ownership', items: [{ title: 'Lantern Keep' }] },
    { id: 'ext', url: 'https://store.steampowered.com/app/4410/', tab: { id: 3 } });
  assert.deepEqual(answer, { status: 'ok', results: [{ status: 'not_owned', game: null, candidates: [] }] });
  assert.deepEqual(sent, [{ url: `${GV}/gamevault.v1.OwnershipService/CheckOwnership`, auth: 'Bearer gvt_test' }]);
});

test('other pages, other extensions and non-tabs get no lookup', async (t) => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV, token: 'gvt_test' } } });
  let fetched = 0;
  const realFetch = globalThis.fetch;
  globalThis.fetch = async () => { fetched++; return { ok: true, status: 200, json: async () => ({ results: [] }) }; };
  t.after(() => { globalThis.fetch = realFetch; });
  for (const sender of [
    { id: 'ext', url: `${GV}/#/library`, tab: { id: 3 } },
    { id: 'ext', url: 'https://evil.example/app/4410/', tab: { id: 3 } },
    { id: 'other', url: 'https://store.steampowered.com/app/4410/', tab: { id: 3 } },
    { id: 'ext', url: 'https://store.steampowered.com/app/4410/' },
  ]) {
    const answer = await ask(chrome, { op: 'ownership', items: [{ title: 'Lantern Keep' }] }, sender);
    assert.deepEqual(answer, { status: 'unavailable', reason: 'denied' }, sender.url);
  }
  assert.equal(fetched, 0);
});

test('a store page asking for anything but a lookup is denied', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } } });
  for (const msg of [{ op: 'connect', recipe: {} }, { op: 'confirm', id: 'x', allow: true }, { op: 'hello' }, null]) {
    const answer = await ask(chrome, msg, { id: 'ext', url: 'https://store.steampowered.com/app/4410/', tab: { id: 3 } });
    assert.deepEqual(answer, { status: 'unavailable', reason: 'denied' }, JSON.stringify(msg));
  }
});

test('the store scripts are registered only while the chosen address is still enabled', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM] });
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-steam']);
  chrome.data.origins = [];
  chrome.storage.onChanged.fire({ origins: { newValue: [] } }, 'local');
  await flush();
  assert.deepEqual(ownershipScripts(chrome), []);
});

test("only the extension's own pages may read its storage, where the browser supports it", async () => {
  const levels = [];
  await load({ setAccessLevel: async (o) => { levels.push(o); } });
  assert.deepEqual(levels, [{ accessLevel: 'TRUSTED_CONTEXTS' }]);
  await load({ setAccessLevel: async () => { throw new Error('not supported'); } });
  await load({ setAccessLevel: () => { throw new Error('not supported'); } });
  await flush();
});
