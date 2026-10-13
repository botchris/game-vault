import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import { forget, recall, recallChoice, remember } from '../src/lib/storage.ts';

const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
const useStorage = (storage) => Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true, writable: true });
afterEach(() => (original ? Object.defineProperty(globalThis, 'localStorage', original) : delete globalThis.localStorage));

const memory = () => {
  const m = new Map();
  return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) };
};

test('a remembered value is recalled on this device', () => {
  useStorage(memory());
  assert.equal(remember('k', 'v'), true);
  assert.equal(recall('k'), 'v');
  assert.equal(recall('missing'), null);
  forget('k');
  assert.equal(recall('k'), null);
});

test('blocked storage (private mode, cleared site data) forgets instead of breaking the page', () => {
  const fail = () => { throw new Error('SecurityError'); };
  useStorage({ getItem: fail, setItem: fail, removeItem: fail });
  assert.equal(remember('k', 'v'), false);
  assert.equal(recall('k'), null);
  assert.doesNotThrow(() => forget('k'));
  delete globalThis.localStorage;
  assert.equal(recall('k'), null, 'no storage at all');
});

test('a remembered choice is used only while it is still one of the allowed ones', () => {
  useStorage(memory());
  assert.equal(recallChoice('view', 'posters', ['list', 'posters']), 'posters', 'nothing saved: the default');
  remember('view', 'list');
  assert.equal(recallChoice('view', 'posters', ['list', 'posters']), 'list');
  remember('view', 'table');
  assert.equal(recallChoice('view', 'posters', ['list', 'posters']), 'posters', 'a value an older version saved');
});
