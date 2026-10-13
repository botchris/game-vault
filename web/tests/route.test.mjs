import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseHash } from '../src/lib/route.ts';

test('an address names a page and what to open on it', () => {
  assert.deepEqual(parseHash('#/sources/s%201'), { page: 'sources', target: 's 1', game: '' });
  assert.deepEqual(parseHash('#/system/backups'), { page: 'system', target: 'backups', game: '' });
  assert.deepEqual(parseHash('#/sources'), { page: 'sources', target: '', game: '' });
  assert.deepEqual(parseHash('#/scan'), { page: 'scan', target: '', game: '' });
});

test("a game's address is the library with that game's sheet", () => {
  assert.deepEqual(parseHash('#/game/abc-1'), { page: 'library', target: '', game: 'abc-1' });
  assert.deepEqual(parseHash('#/game/<script>'), { page: 'library', target: '', game: '' }, 'not a game id');
});

test('an unknown or empty address is the library, and a broken escape opens nothing', () => {
  assert.deepEqual(parseHash(''), { page: 'library', target: '', game: '' });
  assert.deepEqual(parseHash('#/nowhere/x'), { page: 'library', target: '', game: '' });
  assert.deepEqual(parseHash('#/sources/%E0%A4%A'), { page: 'sources', target: '', game: '' });
});
