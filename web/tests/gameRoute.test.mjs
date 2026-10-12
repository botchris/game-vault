import assert from 'node:assert/strict';
import { test } from 'node:test';
import { gameHash, gameIdFromHash } from '../src/lib/gameRoute.ts';

test('a game address names its game; any other address names none', () => {
  assert.equal(gameIdFromHash('#/game/0192c3a4-5b6c-7d8e-9f00-112233445566'), '0192c3a4-5b6c-7d8e-9f00-112233445566');
  assert.equal(gameIdFromHash('#/game/abc/'), 'abc');
  assert.equal(gameIdFromHash('#/library'), '');
  assert.equal(gameIdFromHash('#/game/'), '');
  assert.equal(gameIdFromHash('#/game/<script>'), '');
  assert.equal(gameIdFromHash('#/game/a/b'), '');
  assert.equal(gameIdFromHash(''), '');
});

test('a game address round-trips', () => {
  assert.equal(gameHash('abc-1'), '#/game/abc-1');
  assert.equal(gameIdFromHash(gameHash('abc-1')), 'abc-1');
});
