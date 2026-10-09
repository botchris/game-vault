// The background service worker against a fake `chrome`: requests over the bridge's port, the
// confirmation window, cancelling, enabling from the popup. Each test imports a fresh copy of the
// module after installing its own fake.
import { test, mock } from 'node:test';
import assert from 'node:assert/strict';

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

const GV = 'http://127.0.0.1:8093';

function fakeChrome({ origins = [GV], grants = {}, granted = true, extra = {} } = {}) {
  const data = { origins, grants, ...extra };
  const page = { location: { origin: '' }, storage: {} };
  let nextWindow = 1;
  let nextTab = 100;
  const c = {
    data,
    page,
    tabURLs: {},
    jar: {},
    windowsCreated: [],
    windowsRemoved: [],
    tabsRemoved: [],
    executed: [],
    registered: [],
    runtime: {
      onConnect: ev(), onMessage: ev(), onStartup: ev(), onInstalled: ev(),
      getManifest: () => ({ version: '1.0.0' }),
      getURL: (p) => `chrome-extension://x/${p}`,
    },
    storage: {
      local: {
        get: async (k) => ({ [k]: data[k] }),
        set: async (o) => { Object.assign(data, o); },
        remove: async (k) => { delete data[k]; },
      },
    },
    windows: {
      onRemoved: ev(),
      create: async (o) => { const id = nextWindow++; c.windowsCreated.push({ id, ...o }); return { id, tabs: [{ id: nextTab++ }] }; },
      remove: async (id) => { c.windowsRemoved.push(id); c.windows.onRemoved.fire(id); },
    },
    permissions: { onAdded: ev(), contains: async () => granted },
    tabs: {
      onRemoved: ev(),
      create: async ({ url }) => { const id = nextTab++; c.tabURLs[id] = url; return { id }; },
      get: async (id) => ({ url: c.tabURLs[id] }),
      remove: async (id) => { c.tabsRemoved.push(id); },
    },
    webNavigation: { onCommitted: ev(), onCompleted: ev(), onHistoryStateUpdated: ev() },
    scripting: {
      executeScript: async ({ target, func, args, files }) => {
        if (files) { c.executed.push({ tabId: target.tabId, files }); return []; }
        const saved = [globalThis.location, globalThis.localStorage];
        const set = (k, v) => Object.defineProperty(globalThis, k, { value: v, configurable: true, writable: true });
        set('location', page.location);
        set('localStorage', { getItem: (k) => page.storage[k] ?? null });
        try { return [{ result: func(...args) }]; } finally { set('location', saved[0]); set('localStorage', saved[1]); }
      },
      registerContentScripts: async (list) => { c.registered.push(...list.map((s) => s.id)); },
      getRegisteredContentScripts: async ({ ids }) => c.registered.filter((id) => ids.includes(id)).map((id) => ({ id })),
    },
    cookies: {
      getAllCookieStores: async () => [],
      get: async ({ name }) => (c.jar[name] ? { value: c.jar[name] } : null),
      getAll: async () => [],
    },
    extension: { isAllowedIncognitoAccess: async () => false },
  };
  return c;
}

let loaded = 0;
async function load(options) {
  const chrome = fakeChrome(options);
  globalThis.chrome = chrome;
  await import(`../background.js?copy=${loaded++}`);
  return chrome;
}

function connectPort(chrome, origin = GV, tabId = 7) {
  const port = {
    name: 'gamevault',
    sender: { origin, url: `${origin}/sources`, tab: { id: tabId } },
    onMessage: ev(),
    onDisconnect: ev(),
    sent: [],
    dead: false,
    postMessage(m) { if (this.dead) throw new Error('Attempting to use a disconnected port object'); this.sent.push(m); },
    disconnect() { this.dead = true; },
  };
  chrome.runtime.onConnect.fire(port);
  return port;
}

const humble = {
  version: 1,
  open: 'https://www.humblebundle.com/login',
  when: { urlPrefix: 'https://www.humblebundle.com/home' },
  capture: { cookie: { url: 'https://www.humblebundle.com', name: '_simpleauth_sess' } },
};

const fanatical = {
  version: 1,
  open: 'https://www.fanatical.com/en/',
  when: { contains: '"authenticated":true' },
  capture: { storage: { origin: 'https://www.fanatical.com', key: 'bsauth' } },
};

const grantedFor = (...hosts) => ({ grants: { [GV]: hosts } });

test.beforeEach(() => mock.timers.enable({ apis: ['setTimeout', 'setInterval'] }));
test.afterEach(() => mock.timers.reset());

test('a connect opens the sign-in, captures the value once the user is signed in and answers with it', async () => {
  const chrome = await load(grantedFor('www.humblebundle.com'));
  const port = connectPort(chrome);
  port.onMessage.fire({ op: 'connect', id: 'r1', recipe: humble });
  await flush();
  const [tabId] = Object.keys(chrome.tabURLs).map(Number);
  assert.ok(tabId, 'the sign-in tab opened');
  chrome.jar._simpleauth_sess = 'session';
  chrome.webNavigation.onCompleted.fire({ tabId, frameId: 0, url: 'https://www.humblebundle.com/home/library' });
  await flush();
  assert.deepEqual(port.sent, [{ op: 'result', value: 'session' }]);
  assert.deepEqual(chrome.tabsRemoved, [tabId]);
});

test('the keep-alive pings of the bridge get no answer', async () => {
  const chrome = await load();
  const port = connectPort(chrome);
  port.onMessage.fire({ op: 'ping' });
  await flush();
  assert.deepEqual(port.sent, []);
});

test('a cancel while the confirmation window is open closes it and answers cancelled', async () => {
  const chrome = await load();
  const port = connectPort(chrome);
  port.onMessage.fire({ op: 'connect', id: 'r1', recipe: humble });
  await flush();
  assert.equal(chrome.windowsCreated.length, 1, 'the confirmation window opened');
  const other = connectPort(chrome);
  other.onMessage.fire({ op: 'cancel', id: 'c1', cancelId: 'r1' });
  await flush();
  assert.deepEqual(chrome.windowsRemoved, [chrome.windowsCreated[0].id]);
  assert.equal(port.sent[0]?.op, 'error');
  assert.equal(port.sent[0]?.code, 'cancelled');
  assert.deepEqual(chrome.tabURLs, {}, 'no sign-in tab opened');
});

test('when the page goes away its sign-in is cancelled and the tab closed, without errors', async () => {
  const chrome = await load(grantedFor('www.humblebundle.com'));
  const port = connectPort(chrome);
  port.onMessage.fire({ op: 'connect', id: 'r1', recipe: humble });
  await flush();
  const [tabId] = Object.keys(chrome.tabURLs).map(Number);
  port.dead = true;
  port.onDisconnect.fire(port);
  await flush();
  assert.deepEqual(chrome.tabsRemoved, [tabId]);
});

test('local storage is read only from a page on the recipe origin', async () => {
  const chrome = await load(grantedFor('www.fanatical.com'));
  const port = connectPort(chrome);
  port.onMessage.fire({ op: 'connect', id: 'r1', recipe: fanatical });
  await flush();
  const [tabId] = Object.keys(chrome.tabURLs).map(Number);
  chrome.page.storage.bsauth = '{"authenticated":true,"token":"t"}';
  chrome.page.location = { origin: 'https://elsewhere.example' }; // the tab moved on meanwhile
  chrome.webNavigation.onCompleted.fire({ tabId, frameId: 0, url: 'https://www.fanatical.com/en/' });
  await flush();
  assert.deepEqual(port.sent, []);
  chrome.page.location = { origin: 'https://www.fanatical.com' };
  chrome.webNavigation.onCompleted.fire({ tabId, frameId: 0, url: 'https://www.fanatical.com/en/' });
  await flush();
  assert.equal(port.sent[0]?.value, '{"authenticated":true,"token":"t"}');
});

test('a single-page sign-in that only changes the address is caught', async () => {
  const chrome = await load(grantedFor('www.fanatical.com'));
  const port = connectPort(chrome);
  port.onMessage.fire({ op: 'connect', id: 'r1', recipe: fanatical });
  await flush();
  const [tabId] = Object.keys(chrome.tabURLs).map(Number);
  chrome.page.location = { origin: 'https://www.fanatical.com' };
  chrome.page.storage.bsauth = '{"authenticated":true}';
  chrome.webNavigation.onHistoryStateUpdated.fire({ tabId, frameId: 0, url: 'https://www.fanatical.com/en/account' });
  await flush();
  assert.equal(port.sent[0]?.op, 'result');
});

test('an update or reinstall registers the bridges again', async () => {
  const chrome = await load();
  chrome.runtime.onInstalled.fire({ reason: 'update' });
  await flush();
  assert.equal(chrome.registered.length, 1);
});

test('enabling from the popup completes even if the popup closed for the permission prompt', async () => {
  const lan = 'http://192.168.1.10:8080';
  const chrome = await load({ origins: [], extra: { pendingEnable: { origin: lan, tabId: 3 } } });
  chrome.permissions.onAdded.fire({ origins: ['http://192.168.1.10/*'] });
  await flush();
  assert.deepEqual(chrome.data.origins, [lan]);
  assert.equal(chrome.registered.length, 1);
  assert.deepEqual(chrome.executed, [{ tabId: 3, files: ['bridge.js'] }]);
  assert.equal(chrome.data.pendingEnable, undefined);
});

test('a confirmation is not remembered for a plain-http address on the network', async () => {
  const lan = 'http://192.168.1.10:8080';
  const chrome = await load({ origins: [lan] });
  const port = connectPort(chrome, lan);
  port.onMessage.fire({ op: 'connect', id: 'r1', recipe: humble });
  await flush();
  const url = new URL(chrome.windowsCreated[0].url);
  chrome.runtime.onMessage.fire({ op: 'confirm', id: url.searchParams.get('id'), allow: true, remember: true }, { url: url.href });
  await flush();
  assert.deepEqual(chrome.data.grants, {});
  port.onMessage.fire({ op: 'cancel', id: 'c1', cancelId: 'r1' });
  await flush();
});
