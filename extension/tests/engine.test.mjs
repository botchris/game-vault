import assert from 'node:assert/strict';
import { test } from 'node:test';
import { startRun } from '../lib/engine.js';

const tick = () => new Promise((r) => setTimeout(r, 0));

// fakeBrowser stands in for the extension APIs: tests change its cookies, storage and fetches,
// then fire the tab events a real browser would.
function fakeBrowser({ url = 'about:blank' } = {}) {
  const b = {
    cookies: {}, storage: {}, fetches: {}, url, opened: [], closed: [], handler: null, timeout: null,
    openTab: async (u, priv) => { b.opened.push([u, priv]); return 7; },
    watchTab: (id, h) => { b.handler = h; return () => { b.handler = null; }; },
    tabURL: async () => b.url,
    getCookie: async (u, name) => b.cookies[name] ?? '',
    getCookies: async () => Object.entries(b.cookies).map(([name, value]) => ({ name, value })),
    storageOrigins: [],
    readStorage: async (id, key, origin) => { b.storageOrigins.push(origin); return b.storage[key] ?? ''; },
    pollFn: null, polling: false,
    setInterval: (ms, fn) => { b.pollFn = fn; b.polling = true; return () => { b.polling = false; b.pollFn = null; }; },
    poll: async () => { b.pollFn?.(); await tick(); await tick(); },
    fetchText: async (u) => b.fetches[u] ?? '{}',
    closeTab: async (id) => { b.closed.push(id); },
    setTimer: (ms, fn) => { b.timeout = fn; return () => { b.timeout = null; }; },
    fire: async (kind, u) => { b.url = u ?? b.url; b.handler?.({ kind, url: u }); await tick(); await tick(); },
  };
  return b;
}

const humble = {
  version: 1, open: 'https://www.humblebundle.com/home/keys',
  when: { urlPrefix: 'https://www.humblebundle.com/home/keys' },
  capture: { cookie: { url: 'https://www.humblebundle.com', name: '_simpleauth_sess' } },
};

test('a cookie is captured only once the tab reaches the signed-in page, then the tab closes', async () => {
  const b = fakeBrowser();
  const run = startRun(humble, b);
  await tick();
  b.cookies._simpleauth_sess = 'anonymous';
  await b.fire('loaded', 'https://www.humblebundle.com/login?goto=/home/keys');
  assert.equal(b.closed.length, 0, 'not signed in yet');
  b.cookies._simpleauth_sess = 'signed-in';
  await b.fire('loaded', 'https://www.humblebundle.com/home/keys');
  assert.equal(await run.result, 'signed-in');
  assert.deepEqual(b.closed, [7]);
  assert.deepEqual(b.opened, [['https://www.humblebundle.com/home/keys', false]]);
});

test('a user already signed in is captured right after opening', async () => {
  const b = fakeBrowser({ url: 'https://www.humblebundle.com/home/keys' });
  b.cookies._simpleauth_sess = 'sess';
  assert.equal(await startRun(humble, b).result, 'sess');
});

test('a redirect code is taken when the store redirects', async () => {
  const b = fakeBrowser();
  const run = startRun({ version: 1, open: 'https://login.live.com/oauth20_authorize.srf?x=1',
    capture: { redirect: { prefix: 'https://login.live.com/oauth20_desktop.srf', param: 'code' } } }, b);
  await tick();
  await b.fire('committed', 'https://login.live.com/login.srf');
  await b.fire('committed', 'https://login.live.com/oauth20_desktop.srf?code=M.C1_abc&lc=1033');
  assert.equal(await run.result, 'M.C1_abc');
});

test('storage is read only on its page, and a value must contain the condition', async () => {
  const b = fakeBrowser();
  const run = startRun({ version: 1, open: 'https://www.fanatical.com/en/', when: { contains: '"authenticated":true' },
    capture: { storage: { origin: 'https://www.fanatical.com', key: 'bsauth' } } }, b);
  await tick();
  b.storage.bsauth = '{"authenticated":false}';
  await b.fire('loaded', 'https://www.fanatical.com/en/');
  assert.equal(b.closed.length, 0);
  b.storage.bsauth = '{"authenticated":true,"token":"t"}';
  await b.fire('loaded', 'https://www.fanatical.com/en/account');
  assert.equal(await run.result, '{"authenticated":true,"token":"t"}');

  const u = fakeBrowser();
  const ubi = startRun({ version: 1, open: 'https://connect.ubisoft.com/login?x=1', private: true,
    capture: { storage: { origin: 'https://connect.ubisoft.com', key: 'PRODrememberMe', path: '/ready' } } }, u);
  await tick();
  u.storage.PRODrememberMe = 'ticket';
  await u.fire('loaded', 'https://connect.ubisoft.com/login?x=1');
  assert.equal(u.closed.length, 0, 'not on /ready yet');
  await u.fire('loaded', 'https://connect.ubisoft.com/ready');
  assert.equal(await ubi.result, 'ticket');
  assert.deepEqual(u.opened[0], ['https://connect.ubisoft.com/login?x=1', true]);
});

test('a fetched field and a fetch condition wait until the user is signed in', async () => {
  const b = fakeBrowser();
  const api = 'https://www.epicgames.com/id/api/redirect?clientId=x&responseType=code';
  const run = startRun({ version: 1, open: 'https://www.epicgames.com/id/login?x=1',
    capture: { fetch: { url: api, field: 'authorizationCode' } } }, b);
  await tick();
  b.fetches[api] = '{"authorizationCode":null}';
  await b.fire('loaded', 'https://www.epicgames.com/id/login');
  assert.equal(b.closed.length, 0);
  b.fetches[api] = '{"authorizationCode":"0123abcd"}';
  await b.fire('loaded', 'https://www.epicgames.com/account/personal');
  assert.equal(await run.result, '0123abcd');
});

test('closing the tab cancels without closing it again; the timeout and cancel close it', async () => {
  const b = fakeBrowser();
  const run = startRun(humble, b);
  await tick();
  await b.fire('removed');
  await assert.rejects(run.result, (e) => e.code === 'cancelled');
  assert.deepEqual(b.closed, []);

  const t = fakeBrowser();
  const timed = startRun(humble, t);
  await tick();
  t.timeout();
  await assert.rejects(timed.result, (e) => e.code === 'timeout');
  assert.deepEqual(t.closed, [7]);

  const c = fakeBrowser();
  const cancelled = startRun(humble, c);
  await tick();
  cancelled.cancel();
  await assert.rejects(cancelled.result, (e) => e.code === 'cancelled');
  assert.deepEqual(c.closed, [7]);
});

test('an invalid recipe is refused before anything opens', () => {
  const b = fakeBrowser();
  assert.throws(() => startRun({ ...humble, open: 'http://x.com/' }, b), (e) => e.code === 'invalid');
  assert.deepEqual(b.opened, []);
});

test('a value that appears without a page load (an SPA login) is caught by polling', async () => {
  const b = fakeBrowser();
  const run = startRun({ version: 1, open: 'https://www.fanatical.com/en/', when: { contains: '"authenticated":true' },
    capture: { storage: { origin: 'https://www.fanatical.com', key: 'bsauth' } } }, b);
  await tick();
  await b.fire('loaded', 'https://www.fanatical.com/en/');
  b.storage.bsauth = '{"authenticated":true,"token":"t"}';
  await b.poll();
  assert.equal(await run.result, '{"authenticated":true,"token":"t"}');
  assert.equal(b.polling, false, 'polling stops once captured');
});

test('a redirect that happened before watching started is caught from the tab address', async () => {
  const b = fakeBrowser({ url: 'https://embed.gog.com/on_login_success?origin=client&code=early' });
  const run = startRun({ version: 1, open: 'https://auth.gog.com/auth?x=1', hosts: ['embed.gog.com'],
    capture: { redirect: { prefix: 'https://embed.gog.com/on_login_success', param: 'code' } } }, b);
  assert.equal(await run.result, 'early');
});

test('storage is read only from the capture origin', async () => {
  const b = fakeBrowser();
  const run = startRun({ version: 1, open: 'https://connect.ubisoft.com/login?x=1',
    capture: { storage: { origin: 'https://connect.ubisoft.com', key: 'PRODrememberMe', path: '/ready' } } }, b);
  await tick();
  b.storage.PRODrememberMe = 'ticket';
  await b.fire('loaded', 'https://connect.ubisoft.com/ready');
  assert.equal(await run.result, 'ticket');
  assert.deepEqual(b.storageOrigins, ['https://connect.ubisoft.com']);
});

test('a cancel before the tab opened still closes it when it opens', async () => {
  const b = fakeBrowser();
  let open;
  b.openTab = () => new Promise((r) => { open = r; });
  const run = startRun(humble, b);
  run.cancel();
  open(9);
  await tick(); await tick();
  await assert.rejects(run.result, (e) => e.code === 'cancelled');
  assert.deepEqual(b.closed, [9]);
  assert.equal(b.handler, null, 'no listeners left behind');
});
