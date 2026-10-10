import assert from 'node:assert/strict';
import { test } from 'node:test';
import { coverPath, mainSystem } from '../src/lib/editions.ts';

const edition = (system, over = {}) => ({ system, coverUrl: '', coverPhotoId: '', main: false, ...over });

test('coverPath escapes the system, and without one is the main edition address', () => {
  assert.equal(coverPath('g1', 'Xbox 360'), '/media/covers/g1/Xbox%20360');
  assert.equal(coverPath('g1', 'Xbox 360/S Slim'), '/media/covers/g1/Xbox%20360%2FS%20Slim');
  assert.equal(coverPath('g1', ''), '/media/covers/g1');
});

test('mainSystem is the edition marked main, or none without editions', () => {
  assert.equal(mainSystem({ editions: [edition('PS3', { main: true }), edition('PC')] }), 'PS3');
  assert.equal(mainSystem({ editions: [] }), '');
});
