import assert from 'node:assert/strict';
import { test } from 'node:test';
import { latestOnly } from '../src/lib/latest.ts';

test('only the newest of overlapping requests may apply its answer', () => {
  const latest = latestOnly();
  const first = latest();
  const second = latest();
  assert.equal(first(), false, 'an older answer arriving last is dropped');
  assert.equal(second(), true);
});

test('a request that started after the others finished still applies', () => {
  const latest = latestOnly();
  const first = latest();
  assert.equal(first(), true);
  const second = latest();
  assert.equal(second(), true);
  assert.equal(first(), false);
});

test('two counters do not interfere', () => {
  const a = latestOnly();
  const b = latestOnly();
  const fromA = a();
  b();
  assert.equal(fromA(), true);
});
