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

test('a store script registered by an older version is replaced by the current one', async () => {
  const HUMBLE = 'https://*.humblebundle.com/*';
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [HUMBLE] });
  chrome.registered.set('ownership-humble', { id: 'ownership-humble', matches: ['https://www.humblebundle.com/*'], js: ['ownership/content.js'] });
  chrome.runtime.onInstalled.fire({ reason: 'update' });
  await flush();
  assert.deepEqual(chrome.registered.get('ownership-humble').matches, [HUMBLE]);
  assert.deepEqual(chrome.registered.get('ownership-humble').js, ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js']);
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

/** Makes the fake's scripting behave like the browser's: a registration takes a moment, and an id
 * that is registered (or being registered) is refused, as is unregistering one that is not. */
function strictScripting(chrome, { failFor = [] } = {}) {
  const pending = new Set();
  chrome.scripting.registerContentScripts = async (list) => {
    for (const s of list) {
      if (chrome.registered.has(s.id) || pending.has(s.id)) throw new Error(`Duplicate script ID '${s.id}'`);
      if (failFor.includes(s.id)) throw new Error(`Could not register '${s.id}'`);
      pending.add(s.id);
    }
    await new Promise((r) => setImmediate(r));
    for (const s of list) {
      pending.delete(s.id);
      chrome.registered.set(s.id, s);
    }
  };
  chrome.scripting.unregisterContentScripts = async ({ ids }) => {
    for (const id of ids) if (!chrome.registered.has(id)) throw new Error(`Nonexistent script ID '${id}'`);
    await new Promise((r) => setImmediate(r));
    for (const id of ids) chrome.registered.delete(id);
  };
}

/** Collects console.warn calls for the length of a test. */
function warnings(t) {
  const out = [];
  const real = console.warn;
  console.warn = (...args) => out.push(args.map(String).join(' '));
  t.after(() => { console.warn = real; });
  return out;
}

test('syncs started together run one after the other, so none trips over the other and every allowed store gets its script', async (t) => {
  const { syncStoreScripts } = await import('../lib/ownership.js');
  const HUMBLE = 'https://*.humblebundle.com/*';
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV, token: 'gvt_secret' } }, granted: [STEAM, GOG, HUMBLE] });
  strictScripting(chrome);
  const warned = warnings(t);
  // Turning the option on fires the settings change, the permission grant and more at once.
  await Promise.all([syncStoreScripts(chrome), syncStoreScripts(chrome), syncStoreScripts(chrome)]);
  assert.deepEqual(ownershipScripts(chrome), ['ownership-gog', 'ownership-humble', 'ownership-steam']);
  assert.deepEqual(warned, []);

  chrome.data.ownership = { ...chrome.data.ownership, enabled: false };
  await Promise.all([syncStoreScripts(chrome), syncStoreScripts(chrome)]);
  assert.deepEqual(ownershipScripts(chrome), []);
  assert.deepEqual(warned, []);
});

test('a store whose script the browser refuses does not stop the others, and the failure is logged without settings', async (t) => {
  const { syncStoreScripts } = await import('../lib/ownership.js');
  const HUMBLE = 'https://*.humblebundle.com/*';
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV, token: 'gvt_secret' } }, granted: [STEAM, GOG, HUMBLE] });
  strictScripting(chrome, { failFor: ['ownership-steam'] });
  const warned = warnings(t);
  await syncStoreScripts(chrome);
  assert.deepEqual(ownershipScripts(chrome), ['ownership-gog', 'ownership-humble']);
  assert.equal(warned.length, 1);
  assert.match(warned[0], /steam/);
  assert.doesNotMatch(warned[0], /gvt_secret|127\.0\.0\.1/);
});

/** Store tabs open in the fake browser: which already run the store scripts, and what was put in them. */
function openTabs(chrome, tabs) {
  const injected = [];
  const pattern = (p) => new RegExp(`^${p.replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*')}$`);
  chrome.tabs.query = async ({ url }) => tabs.filter((tab) => pattern(url).test(tab.url));
  chrome.scripting.executeScript = async ({ target, func, files }) => {
    const tab = tabs.find((x) => x.id === target.tabId);
    if (tab.refused) throw new Error('Cannot access contents of the page');
    if (func) return [{ result: Boolean(tab.running) }];
    injected.push({ tabId: target.tabId, files });
    tab.running = true;
    return [{}];
  };
  return injected;
}

test('the store tabs already open when the option is turned on get the scripts, once, without waiting for a reload', async () => {
  const HUMBLE = 'https://*.humblebundle.com/*';
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM, HUMBLE] });
  const injected = openTabs(chrome, [
    { id: 1, url: 'https://es.humblebundle.com/membership/home#choice' },
    { id: 2, url: 'https://store.steampowered.com/app/4410/' },
    { id: 3, url: 'https://www.gog.com/en/game/echo_valley' }, // a store the browser did not allow
    { id: 4, url: 'https://store.steampowered.com/app/10/', running: true }, // loaded after the registration
    { id: 5, url: 'https://store.steampowered.com/app/20/', refused: true }, // an error page
  ]);
  changed(chrome);
  await flush();
  const files = ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js'];
  assert.deepEqual(injected.sort((a, b) => a.tabId - b.tabId), [{ tabId: 1, files }, { tabId: 2, files }]);

  // Another sync (the token edited, the extension updated…) leaves the tabs that run them alone.
  changed(chrome);
  await flush();
  assert.equal(injected.length, 2);
});

test('after an update or a reload of the extension, the open store tabs get the new scripts', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM] });
  chrome.registered.set('ownership-steam', { id: 'ownership-steam', matches: [STEAM], js: ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js'] });
  // The scripts of the previous version still sit in the tab, but they no longer reach the
  // extension; the new version's world in that tab has nothing yet.
  const injected = openTabs(chrome, [{ id: 7, url: 'https://store.steampowered.com/app/4410/' }]);
  chrome.runtime.onInstalled.fire({ reason: 'update' });
  await flush();
  assert.deepEqual(injected.map((i) => i.tabId), [7]);
});

test('with the option off, no tab gets the scripts', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: false, origin: GV } }, granted: [STEAM] });
  const injected = openTabs(chrome, [{ id: 1, url: 'https://store.steampowered.com/app/4410/' }]);
  changed(chrome);
  chrome.runtime.onInstalled.fire({ reason: 'update' });
  await flush();
  assert.deepEqual(injected, []);
});
