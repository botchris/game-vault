import assert from 'node:assert/strict';
import { test } from 'node:test';
import { cachedRequest } from '../src/lib/requestCache.ts';

test('asking twice for the same key sends one request (StrictMode, switching back to an edition)', async () => {
  const cache = new Map();
  let sent = 0;
  const load = () => { sent++; return Promise.resolve(['candidate']); };
  const a = cachedRequest(cache, 'game\u0000PS4', load);
  const b = cachedRequest(cache, 'game\u0000PS4', load);
  assert.equal(a, b);
  assert.deepEqual(await b, ['candidate']);
  await cachedRequest(cache, 'game\u0000PC', load);
  await cachedRequest(cache, 'game\u0000PS4', load);
  assert.equal(sent, 2);
});

test('a failed request is forgotten, so the next ask tries again', async () => {
  const cache = new Map();
  let sent = 0;
  const failing = () => { sent++; return Promise.reject(new Error('down')); };
  await assert.rejects(cachedRequest(cache, 'k', failing));
  await new Promise((r) => setTimeout(r, 0));
  await assert.rejects(cachedRequest(cache, 'k', failing));
  assert.equal(sent, 2);
});
