import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseHash } from '../src/lib/route.ts';

test('an address names a page and what to open on it', () => {
  assert.deepEqual(parseHash('#/sources/s%201'), { page: 'sources', target: 's 1', game: '' });
  assert.deepEqual(parseHash('#/system/backups'), { page: 'system', target: 'backups', game: '' });
  assert.deepEqual(parseHash('#/sources'), { page: 'sources', target: '', game: '' });
  assert.deepEqual(parseHash('#/scan'), { page: 'scan', target: '', game: '' });
  assert.deepEqual(parseHash('#/library/pending'), { page: 'library', target: 'pending', game: '' });
  assert.deepEqual(parseHash('#/home'), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/notifications'), { page: 'notifications', target: '', game: '' });
  assert.deepEqual(parseHash('#/notifications/c%201'), { page: 'notifications', target: 'c 1', game: '' });
});

test("a game's address is the library with that game's sheet", () => {
  assert.deepEqual(parseHash('#/game/abc-1'), { page: 'library', target: '', game: 'abc-1' });
  assert.deepEqual(parseHash('#/game/<script>'), { page: 'home', target: '', game: '' }, 'not a game id: an unknown address');
});

test('the start address and unknown ones open the home page, and a broken escape opens nothing', () => {
  assert.deepEqual(parseHash(''), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/'), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/nowhere/x'), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/sources/%E0%A4%A'), { page: 'sources', target: '', game: '' });
});
