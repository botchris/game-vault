import assert from 'node:assert/strict';
import { test } from 'node:test';
import { addressFor, gameHash, gameIdFromHash } from '../src/lib/gameRoute.ts';

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

test("a sheet opened from a game's address keeps the address on the game it shows", () => {
  assert.equal(addressFor('#/game/a', true, 'b'), '#/game/b', 'moved to the next game: a reload opens that one');
  assert.equal(addressFor('#/game/a', true, 'a'), null, 'already its address');
  assert.equal(addressFor('#/library', false, 'b'), null, 'opened from the grid: the address stays the library');
  assert.equal(addressFor('#/game/a', true, null), null, 'closed: closing clears the address itself');
});
