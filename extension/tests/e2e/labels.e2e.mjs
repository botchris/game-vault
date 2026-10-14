// The store labels end to end, in a real Chromium with the unpacked extension (task extension:e2e;
// it runs in the Playwright image, never on the host). Store sites are a local HTTPS server serving
// the saved samples (invented titles), reached through --host-resolver-rules; Game Vault is a fake
// answering OwnershipService/CheckOwnership. The option is turned on in the extension's own popup,
// the background registers the store scripts, and the labels are read in the extension's world of
// each page (where DevTools lists "Game Vault Connector"): they live in closed shadow roots the
// page cannot open.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer as createHttpServer } from 'node:http';
import { createServer as createHttpsServer } from 'node:https';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, before, test } from 'node:test';
import { chromium } from 'playwright-core';

const EXTENSION = process.env.EXTENSION_DIR ?? new URL('../../', import.meta.url).pathname;
const SAMPLES = new URL('../samples/', import.meta.url).pathname;
const STORE_PORT = 8443;
const STORE_HOSTS = ['store.steampowered.com', '*.humblebundle.com', 'www.gog.com', 'www.fanatical.com'];
const TOKEN = 'gvt_e2e-not-a-real-token';
const WAIT_MS = 15_000;

const STEAM_APP = 'https://store.steampowered.com/app/4410/Lantern_Keep/';
const HUMBLE_STORE = 'https://es.humblebundle.com/store/harbor-lights';
const HUMBLE_BUNDLE = 'https://www.humblebundle.com/games/invented-games-bundle';
const HUMBLE_CHOICE = 'https://es.humblebundle.com/membership/home#october-2026';

// The fake store sites: host name and path → saved sample.
const PAGES = {
  'store.steampowered.com/app/4410/Lantern_Keep/': 'steam-app.html',
  'es.humblebundle.com/store/harbor-lights': 'humble-store.html',
  'www.humblebundle.com/games/invented-games-bundle': 'humble-bundle.html',
  'es.humblebundle.com/membership/home': 'humble-choice.html',
};

// What the fake Game Vault owns: Lantern Keep (also by its Steam AppID), maybe Paper Comet.
function answer(item) {
  const title = (item.title ?? '').toLowerCase();
  if ((item.store === 'steam' && item.id === '4410') || title === 'lantern keep') {
    return {
      status: 'owned',
      game: { title: 'Lantern Keep', path: '#/game/lantern-keep', copies: [{ platform: 'Steam', system: 'PC', kind: 'library' }] },
    };
  }
  if (title === 'paper comet') return { status: 'possible', candidates: [{ title: 'Paper Comet Deluxe', path: '#/game/paper-comet' }] };
  return { status: 'not_owned' };
}

const work = mkdtempSync(join(tmpdir(), 'gv-e2e-'));
const requests = [];
let stores;
let gamevault;
let gamevaultOrigin;
let extensionDir;
let extensionId;
let context;
let worker;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitFor(what, fn) {
  const until = Date.now() + WAIT_MS;
  while (Date.now() < until) {
    const got = await fn();
    if (got) return got;
    await sleep(200);
  }
  throw new Error(`timed out waiting for ${what}`);
}

function listen(server, port = 0) {
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server.address().port)));
}

/** A self-signed certificate for the fake stores (Chromium runs with --ignore-certificate-errors). */
function certificate() {
  const key = join(work, 'store.key');
  const cert = join(work, 'store.crt');
  execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=stores.test', '-keyout', key, '-out', cert], { stdio: 'ignore' });
  return { key: readFileSync(key), cert: readFileSync(cert) };
}

/**
 * The extension as the browser loads it, without the tests. A permission prompt cannot be answered
 * in an automated run, so the copy declares as granted the host permissions the popup asks for (the
 * store sites, and the Game Vault address it enables); everything else is unchanged.
 */
function extensionCopy(storeOrigins) {
  const dir = join(work, 'extension');
  cpSync(EXTENSION, dir, { recursive: true, filter: (src) => !src.startsWith(join(EXTENSION, 'tests')) });
  const manifest = JSON.parse(readFileSync(join(dir, 'manifest.json'), 'utf8'));
  manifest.host_permissions = [...storeOrigins, 'http://127.0.0.1/*'];
  writeFileSync(join(dir, 'manifest.json'), JSON.stringify(manifest, null, 2));
  return dir;
}

/** Runs fn(arg) in the extension's service worker. */
const inWorker = (fn, arg) => worker.evaluate(fn, arg);

/** The extension's service worker other than `not`, once the browser has given it the extension APIs. */
async function findWorker(not) {
  const found = await waitFor("the extension's service worker", async () => {
    const sw = context.serviceWorkers().find((w) => w !== not && w.url().startsWith(`chrome-extension://${extensionId}/`));
    if (sw) return sw;
    // A worker that is not running is woken by one of the extension's pages.
    const page = await context.newPage();
    await page.goto(`chrome-extension://${extensionId}/popup.html`).catch(() => {});
    await page.close();
    return null;
  });
  await waitFor('the extension APIs in the worker', () => found.evaluate(() => Boolean(globalThis.chrome?.storage)).catch(() => false));
  return found;
}

/** Starts Chromium (its new headless mode runs extensions) on the test profile, kept between launches. */
async function launch() {
  context = await chromium.launchPersistentContext(join(work, 'profile'), {
    channel: 'chromium',
    headless: true,
    locale: 'en-US',
    args: [
      `--disable-extensions-except=${extensionDir}`,
      `--load-extension=${extensionDir}`,
      `--host-resolver-rules=${STORE_HOSTS.map((h) => `MAP ${h} 127.0.0.1:${STORE_PORT}`).join(', ')}`,
      '--ignore-certificate-errors',
      '--lang=en-US',
    ],
  });
  const first = context.serviceWorkers()[0] ?? await context.waitForEvent('serviceworker');
  extensionId = new URL(first.url()).host;
  worker = await findWorker();
}

/** Turns the store-page option on in the extension's popup, with the token. */
async function turnOptionOn() {
  const popup = await context.newPage();
  try {
    await popup.goto(`chrome-extension://${extensionId}/popup.html`);
    const enabled = popup.locator('#own-enabled');
    await waitFor('the popup', () => enabled.isEnabled());
    await popup.locator('#own-token').fill(TOKEN);
    await popup.locator('#own-token').dispatchEvent('change');
    await enabled.check();
    await waitFor('the option to be on', async () => /Steam/.test(await popup.locator('#own-status').textContent()));
  } finally {
    await popup.close();
  }
}

/** Points the option at another Game Vault address, the way the popup leaves it. */
async function useGameVault(origin) {
  await inWorker(async ({ origin, token }) => {
    await chrome.storage.local.set({ origins: [origin], ownership: { enabled: true, origin, token } });
  }, { origin, token: TOKEN });
}

/**
 * Reads a page's labels in the extension's world: each host's kind and text, or {missing} when
 * the store scripts of the running extension do not run there.
 */
async function labelsOn(page) {
  const cdp = await page.context().newCDPSession(page);
  const worlds = [];
  cdp.on('Runtime.executionContextCreated', ({ context: c }) => {
    if (c.name === 'Game Vault Connector') worlds.push(c.id);
  });
  cdp.on('Runtime.executionContextsCleared', () => worlds.splice(0));
  await cdp.send('Runtime.enable');
  const read = async () => {
    // A tab that outlived a reload of the extension holds the old copy's world and the new one's.
    for (const id of [...worlds].reverse()) {
      const { result } = await cdp.send('Runtime.evaluate', {
        contextId: id,
        returnByValue: true,
        expression: `(() => {
          const ns = globalThis.gvOwnership;
          if (!ns?.rootOf || !globalThis.chrome?.runtime?.id) return null;
          return { labels: [...document.querySelectorAll('[data-gamevault]')].map((h) => ({
            kind: ns.kindOf(h) ?? 'unknown',
            text: ns.rootOf(h)?.textContent ?? '',
          })) };
        })()`,
      }).catch(() => ({ result: {} }));
      if (result?.value) return result.value;
    }
    return { missing: `the store scripts do not run here (${worlds.length} extension worlds)` };
  };
  return { read, detach: () => cdp.detach() };
}

/** Waits until a page shows `count` labels and returns them; the error says what the page holds. */
async function labelsShown(page, count) {
  const labels = await labelsOn(page);
  let last;
  try {
    return await waitFor(`${count} labels on ${page.url()}`, async () => {
      last = await labels.read();
      return last.labels?.length >= count ? last.labels : null;
    });
  } catch (err) {
    throw new Error(`${err.message}: ${JSON.stringify(last)}`);
  } finally {
    await labels.detach();
  }
}

async function open(url) {
  const page = await context.newPage();
  await page.goto(url);
  return page;
}

before(async () => {
  stores = createHttpsServer(certificate(), (req, res) => {
    const file = PAGES[`${req.headers.host.replace(/:\d+$/, '')}${req.url.replace(/[?#].*$/, '')}`];
    if (!file) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end(readFileSync(join(SAMPLES, file)));
  });
  await listen(stores, STORE_PORT);

  gamevault = createHttpServer((req, res) => {
    let body = '';
    req.on('data', (c) => { body += c; });
    req.on('end', () => {
      requests.push({ method: req.method, path: req.url, authorization: req.headers.authorization, cookie: req.headers.cookie });
      if (req.method !== 'POST' || req.url !== '/gamevault.v1.OwnershipService/CheckOwnership') {
        res.writeHead(404).end();
        return;
      }
      const { items } = JSON.parse(body);
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ results: items.map(answer) }));
    });
  });
  gamevaultOrigin = `http://127.0.0.1:${await listen(gamevault)}`;

  // The store sites come from the same data the background uses.
  await import('../../ownership/stores.js');
  const { STORE_ORIGINS } = await import('../../lib/ownership.js');
  extensionDir = extensionCopy(STORE_ORIGINS);
  await launch();
  // Game Vault's address enabled in the extension (what "Enable on" in the popup leaves).
  await inWorker((origin) => chrome.storage.local.set({ origins: [origin] }), gamevaultOrigin);
});

after(async () => {
  await context?.close();
  stores?.close();
  gamevault?.close();
  rmSync(work, { recursive: true, force: true });
});

test('a store page already open when the option is turned on gets its labels without a reload', async () => {
  // The reported case: a member's Humble Choice page on a language site, open first, then the
  // option turned on from the popup.
  const page = await open(HUMBLE_CHOICE);
  await turnOptionOn();
  const labels = await labelsShown(page, 4);
  assert.ok(labels.some((l) => l.kind === 'summary'), JSON.stringify(labels));
  assert.ok(labels.some((l) => l.kind === 'owned' && l.text === 'Owned on PC'), JSON.stringify(labels));
  await page.close();
});

test('a Steam product page shows what Game Vault owns', async () => {
  const page = await open(STEAM_APP);
  const labels = await labelsShown(page, 1);
  assert.deepEqual(labels.map((l) => l.kind), ['owned']);
  assert.match(labels[0].text, /^In your collection: Steam · /);
  await page.close();
});

test('a Humble product page on a language site shows "not in your collection"', async () => {
  const page = await open(HUMBLE_STORE);
  assert.deepEqual(await labelsShown(page, 1), [{ kind: 'not-owned', text: 'Not in your collection' }]);
  await page.close();
});

test('a Humble bundle page marks every game and sums them up', async () => {
  const page = await open(HUMBLE_BUNDLE);
  // Five games (one listed twice) and the summary.
  const labels = await labelsShown(page, 6);
  assert.deepEqual(labels.filter((l) => l.kind !== 'summary').map((l) => l.kind), ['owned', 'possible', 'none', 'none', 'owned']);
  assert.equal(labels.find((l) => l.kind === 'summary').text, 'You own 1 of 4 · 1 possible');
  await page.close();
});

test('the page cannot open a label, nor see the extension', async () => {
  const page = await open(STEAM_APP);
  await labelsShown(page, 1);
  assert.equal(await page.evaluate(() => document.querySelector('[data-gamevault]').shadowRoot), null);
  assert.equal(await page.evaluate(() => typeof globalThis.gvOwnership), 'undefined');
  await page.close();
});

test('Game Vault is asked only for lookups, with the token and without cookies', () => {
  assert.ok(requests.length > 0);
  for (const r of requests) {
    assert.equal(`${r.method} ${r.path}`, 'POST /gamevault.v1.OwnershipService/CheckOwnership');
    assert.equal(r.authorization, `Bearer ${TOKEN}`);
    assert.equal(r.cookie, undefined);
  }
});

test('a store tab open while the extension is reloaded keeps its labels when the page moves on', async () => {
  const page = await open(HUMBLE_STORE);
  await labelsShown(page, 1);
  // The reload button of chrome://extensions (developer mode), as after pulling a new version.
  const extensions = await open('chrome://extensions');
  await extensions.evaluate(() => chrome.developerPrivate.updateProfileConfiguration({ inDeveloperMode: true }));
  const old = worker;
  await extensions.evaluate((id) => chrome.developerPrivate.reload(id, { failQuietly: false }), extensionId);
  await extensions.close();
  worker = await findWorker(old);
  // A single-page store moving on: the old copy's scripts must not wipe what the new ones draw.
  await page.evaluate(() => {
    history.pushState(null, '', '#reviews');
    document.body.append(document.createElement('div'));
  });
  await sleep(1500);
  assert.deepEqual(await labelsShown(page, 1), [{ kind: 'not-owned', text: 'Not in your collection' }]);
  await page.close();
});

test('after a browser restart, store pages still get their labels', async () => {
  await context.close();
  await launch();
  const page = await open(HUMBLE_STORE);
  assert.deepEqual((await labelsShown(page, 1)).map((l) => l.kind), ['not-owned']);
  await page.close();
});

test('when Game Vault is unreachable, the label says unavailable, never "not owned"', async () => {
  // Another address on which nothing listens; changing it drops the cached answers.
  const closed = createHttpServer();
  const port = await listen(closed);
  closed.close();
  await useGameVault(`http://127.0.0.1:${port}`);
  const page = await open(HUMBLE_STORE);
  assert.deepEqual(await labelsShown(page, 1), [{ kind: 'unavailable', text: 'Game Vault unavailable' }]);
  await page.close();
});
