import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CACHE_MS, MAX_ITEMS, STORE_ORIGINS, createLookup, nextOwnership, removableOrigins, scriptFor, tokenInClear } from '../lib/ownership.js';

const GV = 'http://127.0.0.1:8093';
const json = (body, status = 200) => ({ ok: status >= 200 && status < 300, status, json: async () => body });

/** A lookup over fake storage and a fake Game Vault that owns "Lantern Keep" only. */
function setup(opts = {}) {
  const { origins = [GV], answer } = opts;
  // Read by hand: a default parameter would replace `ownership: undefined` (never set up).
  const ownership = 'ownership' in opts ? opts.ownership : { enabled: true, origin: GV, token: 'gvt_test' };
  const data = { ownership, origins };
  const calls = [];
  let clock = 1_000_000;
  const lookup = createLookup({
    storage: { get: async (k) => ({ [k]: data[k] }) },
    fetch: async (url, init) => {
      const body = JSON.parse(init.body);
      calls.push({ url, init, body });
      if (answer) return answer(body);
      return json({ results: body.items.map((it) => (it.title === 'Lantern Keep'
        ? { status: 'owned', game: { id: 'g1', title: 'Lantern Keep', path: '#/game/g1', copies: [{ platform: 'Steam', system: 'PC', kind: 'library' }] } }
        : { status: 'not_owned' })) });
    },
    now: () => clock,
  });
  return { lookup, calls, data, tick: (ms) => { clock += ms; } };
}

test('a lookup goes only to the chosen Game Vault, with the token, no cookies, and only what the page showed', async () => {
  const { lookup, calls } = setup();
  const out = await lookup.check([{ store: 'Steam', id: ' 4410 ', title: '  Lantern Keep ', price: '$19.99', cookie: 'x' }]);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, `${GV}/gamevault.v1.OwnershipService/CheckOwnership`);
  assert.equal(calls[0].init.method, 'POST');
  assert.equal(calls[0].init.credentials, 'omit');
  assert.equal(calls[0].init.headers.Authorization, 'Bearer gvt_test');
  assert.deepEqual(calls[0].body, { items: [{ store: 'steam', id: '4410', title: 'Lantern Keep' }] });
  assert.deepEqual(out, { status: 'ok', results: [{
    status: 'owned',
    game: { title: 'Lantern Keep', url: `${GV}/#/game/g1`, copies: [{ platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false }] },
    candidates: [],
  }] });
});

test('without a token no Authorization header is sent', async () => {
  const { lookup, calls } = setup({ ownership: { enabled: true, origin: GV, token: '' } });
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls[0].init.headers.Authorization, undefined);
});

test('a bundle with more than 200 games is asked in parts of at most 200, and the answers keep their order', async () => {
  const { lookup, calls } = setup();
  const items = Array.from({ length: 450 }, (_, i) => ({ title: i === 449 ? 'Lantern Keep' : `Invented Game ${i}` }));
  const out = await lookup.check(items);
  assert.deepEqual(calls.map((c) => c.body.items.length), [MAX_ITEMS, MAX_ITEMS, 50]);
  assert.equal(out.results.length, 450);
  assert.equal(out.results[0].status, 'not_owned');
  assert.equal(out.results[449].status, 'owned');
});

test('answers are reused for a minute, and forgotten when the settings change', async () => {
  const { lookup, calls, tick } = setup();
  await lookup.check([{ title: 'Lantern Keep' }]);
  await lookup.check([{ title: 'lantern keep' }]);
  assert.equal(calls.length, 1);
  tick(CACHE_MS);
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 2);
  lookup.clear();
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 3);
});

test('Game Vault not answering, or answering wrong, is unavailable — never not owned', async () => {
  for (const [name, answer, reason] of [
    ['network error', () => { throw new TypeError('Failed to fetch'); }, 'unreachable'],
    ['server error', () => json({ code: 'internal' }, 500), 'unreachable'],
    ['throttled', () => json({ code: 'resource_exhausted' }, 429), 'unreachable'],
    ['not JSON', () => ({ ok: true, status: 200, json: async () => { throw new SyntaxError('Unexpected token <'); } }), 'unreachable'],
    ['wrong number of results', () => json({ results: [] }), 'unreachable'],
    ['token refused', () => json({ code: 'unauthenticated' }, 401), 'token'],
  ]) {
    const { lookup } = setup({ answer });
    assert.deepEqual(await lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason }, name);
  }
});

test('without a token, a refusal asks for one', async () => {
  const { lookup } = setup({ ownership: { enabled: true, origin: GV }, answer: () => json({}, 401) });
  assert.deepEqual(await lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason: 'signin' });
});

test('a refused token forgets the cached answers', async () => {
  let refuse = false;
  const { lookup, calls } = setup({ answer: (body) => (refuse ? json({}, 401) : json({ results: body.items.map(() => ({ status: 'not_owned' })) })) });
  await lookup.check([{ title: 'Lantern Keep' }]);
  refuse = true;
  assert.equal((await lookup.check([{ title: 'Echo Valley' }])).reason, 'token');
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 3, 'asked again: the answer cached with the old token was dropped');
});

test('nothing is sent unless the option is on and its address is still enabled', async () => {
  for (const [name, opts] of [
    ['off', { ownership: { enabled: false, origin: GV } }],
    ['no address', { ownership: { enabled: true } }],
    ['address removed', { ownership: { enabled: true, origin: GV }, origins: [] }],
    ['never set up', { ownership: undefined }],
  ]) {
    const { lookup, calls } = setup(opts);
    assert.deepEqual(await lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason: 'setup' }, name);
    assert.equal(calls.length, 0, name);
  }
});

test('a sheet link is only ever an address on the chosen Game Vault', async () => {
  for (const path of ['javascript:alert(1)', '//evil.example/#/game/g1', '#/game/../../x', undefined]) {
    const { lookup } = setup({ answer: () => json({ results: [{ status: 'owned', game: { title: 'Lantern Keep', path } }] }) });
    const out = await lookup.check([{ title: 'Lantern Keep' }]);
    assert.equal(out.results[0].game.url, `${GV}/`, String(path));
  }
});

test('turning the option off gives back only the store sites no sign-in uses', () => {
  assert.deepEqual(removableOrigins({ [GV]: ['www.humblebundle.com'] }), STORE_ORIGINS.filter((o) => o !== 'https://*.humblebundle.com/*'));
  assert.deepEqual(removableOrigins(undefined), STORE_ORIGINS);
});

test('each store gets its own content script, made of the four ownership files', () => {
  assert.deepEqual(STORE_ORIGINS, ['https://store.steampowered.com/*', 'https://www.gog.com/*', 'https://*.humblebundle.com/*', 'https://www.fanatical.com/*']);
  assert.deepEqual(scriptFor({ store: 'gog', host: 'www.gog.com' }), {
    id: 'ownership-gog',
    matches: ['https://www.gog.com/*'],
    js: ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js'],
    runAt: 'document_idle',
    persistAcrossSessions: true,
  });
});

test('a token travels unencrypted only to a plain http address off this computer', () => {
  for (const origin of ['http://192.168.1.20:8080', 'http://gamevault.lan', 'http://10.0.0.5', 'http://[fd00::1]:8080', 'http://127.0.0.1.example']) {
    assert.equal(tokenInClear(origin), true, origin);
  }
  for (const origin of ['https://gamevault.lan', 'https://192.168.1.20', 'http://127.0.0.1:8080', 'http://127.4.5.6', 'http://localhost:8080', 'http://[::1]:8080', '', 'not an address', undefined]) {
    assert.equal(tokenInClear(origin), false, String(origin));
  }
});

test('another Game Vault address forgets the token and turns the option off', () => {
  const saved = { enabled: true, origin: GV, token: 'gvt_test' };
  assert.equal(nextOwnership(saved, GV), saved, 'same address: unchanged');
  assert.deepEqual(nextOwnership(saved, 'http://192.168.1.20:8080'), { enabled: false, origin: 'http://192.168.1.20:8080', token: '' });
  assert.deepEqual(nextOwnership(saved, ''), { enabled: false, origin: '', token: '' }, 'its address was removed');
  assert.deepEqual(nextOwnership(undefined, GV), { enabled: false, origin: GV, token: '' }, 'never set up');
});

test('a token changed without clear() is not answered from the old token\'s cache', async () => {
  const { lookup, calls, data } = setup();
  await lookup.check([{ title: 'Lantern Keep' }]);
  data.ownership = { ...data.ownership, token: 'gvt_other' };
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 2);
  assert.equal(calls[1].init.headers.Authorization, 'Bearer gvt_other');
});

test('an answer that arrives after clear() is not cached', async () => {
  let release;
  const gate = new Promise((r) => { release = r; });
  let first = true;
  const { lookup, calls } = setup({ answer: async (body) => {
    if (first) { first = false; await gate; }
    return json({ results: body.items.map(() => ({ status: 'not_owned' })) });
  } });
  const pending = lookup.check([{ title: 'Lantern Keep' }]);
  await new Promise((r) => setImmediate(r));
  lookup.clear(); // settings changed while Game Vault was answering
  release();
  await pending;
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 2, 'asked again: the answer from before clear() was not kept');
});

test('a redirect is never followed, so the token is not re-sent elsewhere, and reads as unreachable', async () => {
  const { lookup, calls } = setup();
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls[0].init.redirect, 'error', 'an SSO proxy sending to /login would get the token');
  // With redirect: 'error', fetch rejects on a 3xx answer.
  const refused = setup({ answer: () => { throw new TypeError('Failed to fetch: unexpected redirect'); } });
  assert.deepEqual(await refused.lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason: 'unreachable' });
});
