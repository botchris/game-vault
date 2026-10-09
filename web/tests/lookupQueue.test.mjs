import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createLookupQueue } from '../src/features/scan/lookupQueue.ts';

const flush = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setImmediate(r)); };

function fakeLookups() {
  const calls = [];
  let running = 0;
  let most = 0;
  const lookup = (code) => new Promise((resolve, reject) => {
    running++;
    most = Math.max(most, running);
    calls.push({ code, resolve: (v) => { running--; resolve(v); }, reject: (e) => { running--; reject(e); } });
  });
  return { calls, lookup, most: () => most };
}

test('codes are looked up one at a time, in order, and each answer reaches its row', async () => {
  const f = fakeLookups();
  const done = [];
  const q = createLookupQueue(f.lookup, (id, answer, error) => done.push([id, answer, error]));
  q.push('r1', '1');
  q.push('r2', '2');
  q.push('r3', '3');
  await flush();
  assert.deepEqual(f.calls.map((c) => c.code), ['1']);
  f.calls[0].resolve('a1');
  await flush();
  f.calls[1].reject(new Error('down'));
  await flush();
  f.calls[2].resolve('a3');
  await flush();
  assert.equal(f.most(), 1);
  assert.deepEqual(done.map(([id, a, e]) => [id, a, e?.message]), [['r1', 'a1', undefined], ['r2', undefined, 'down'], ['r3', 'a3', undefined]]);
});

test('a row pushed twice is looked up once; a dropped row is not looked up', async () => {
  const f = fakeLookups();
  const q = createLookupQueue(f.lookup, () => {});
  q.push('r1', '1');
  q.push('r2', '2');
  q.push('r2', '2');
  q.push('r3', '3');
  q.drop('r3');
  await flush();
  f.calls[0].resolve('a');
  await flush();
  f.calls[1].resolve('b');
  await flush();
  assert.deepEqual(f.calls.map((c) => c.code), ['1', '2']);
});
