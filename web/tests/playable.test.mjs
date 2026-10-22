import assert from 'node:assert/strict';
import { test } from 'node:test';
import { isPlayable, onlyKeysToRedeem, playableCopy } from '../src/lib/playable.ts';

// Copies as the API sends them. Kinds: 1 key, 2 library, 3 physical. Statuses: 1 unrevealed,
// 2 revealed, 3 redeemed, 4 gifted, 5 expired, 6 owned, 7 lent, 8 sold.
const copy = (kind, status) => ({ details: { kind, status } });
const game = (...copies) => ({ copies });

test('a copy you can play now: in a store library, a redeemed key, a disc at home', () => {
  assert.equal(playableCopy(copy(2, 6)), true);
  assert.equal(playableCopy(copy(1, 3)), true, 'a redeemed key is yours on its store, connected or not');
  assert.equal(playableCopy(copy(3, 6)), true);
  assert.equal(playableCopy(copy(3, 0)), true, 'a disc without a status is at home');
});

test('a copy you cannot play now: a key to redeem, a lent disc, anything given away or gone', () => {
  for (const [kind, status] of [[1, 1], [1, 2], [3, 7], [1, 4], [1, 5], [3, 8], [3, 4], [2, 8]]) {
    assert.equal(playableCopy(copy(kind, status)), false, `kind ${kind}, status ${status}`);
  }
  assert.equal(playableCopy({}), false);
});

test('a game is playable when any copy is: a Steam key to redeem and an Epic library copy is', () => {
  assert.equal(isPlayable(game(copy(1, 1), copy(2, 6))), true);
  assert.equal(onlyKeysToRedeem(game(copy(1, 1), copy(2, 6))), false);
});

test('a game with only keys to redeem is not playable, and is counted as such', () => {
  const g = game(copy(1, 1), copy(1, 2));
  assert.equal(isPlayable(g), false);
  assert.equal(onlyKeysToRedeem(g), true);
});

test('a game with only a lent disc, or with nothing left, is neither', () => {
  for (const g of [game(copy(3, 7)), game(copy(1, 4)), game()]) {
    assert.equal(isPlayable(g), false);
    assert.equal(onlyKeysToRedeem(g), false);
  }
});

test('a game with a key to redeem and a lent disc has only keys to redeem it can play with', () => {
  assert.equal(onlyKeysToRedeem(game(copy(1, 1), copy(3, 7))), true);
});
