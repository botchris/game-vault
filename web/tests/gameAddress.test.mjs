import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import { Window } from 'happy-dom';
import { createGameAddress } from '../src/lib/gameAddress.ts';

// The library's sheet and its "#/game/<id>" address, on a real (happy-dom) history.
let windows = [];
afterEach(async () => {
  await Promise.all(windows.map((w) => w.happyDOM.close()));
  windows = [];
});

function setup(hash) {
  const win = new Window({ url: `http://127.0.0.1:8093/${hash}` });
  windows.push(win);
  const address = createGameAddress(win, (h) => win.history.replaceState(null, '', h));
  return { win, address };
}

const settle = () => new Promise((r) => setTimeout(r, 20));

test('a sheet opened from its address keeps the address on the game shown, and closing replaces it with the library', () => {
  const { win, address } = setup('#/game/a');
  const entries = win.history.length;
  assert.equal(address.follow('a'), 'open');
  address.track('b');
  assert.equal(win.location.hash, '#/game/b', 'moved to the next game');
  address.close();
  assert.equal(win.location.hash, '#/library', 'a reload opens no sheet');
  assert.equal(win.history.length, entries, 'replaced, never pushed: Back leaves the app as it came');
});

test('going back from an address-opened sheet closes it; a sheet opened from the grid leaves the address alone', () => {
  const { win, address } = setup('#/library');
  assert.equal(address.follow('a'), 'open');
  assert.equal(address.follow(''), 'close', 'Back to #/library');
  address.track('b');
  assert.equal(win.location.hash, '#/library', 'opened from the grid: the address stays the library');
  address.close();
  assert.equal(win.location.hash, '#/library');
});

test('an address naming a game that no longer exists is cleared; a grid sheet is never cleared this way', () => {
  const exists = (id) => id === 'a';
  const { win, address } = setup('#/game/gone');
  address.follow('gone');
  assert.equal(address.missing('gone', exists), true);
  address.close();
  assert.equal(win.location.hash, '#/library');
  assert.equal(address.missing('a', exists), false, 'it exists');
  assert.equal(address.missing('gone', exists), false, 'no longer opened by its address');
});
