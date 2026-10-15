import assert from 'node:assert/strict';
import { test } from 'node:test';

// Each scenario loads its own copy of the module (a different query string), since the state is
// the session's.
const fresh = (tag) => import(`../src/features/home/entrance.ts?${tag}`);

test('Home fades in once when the app opens on it, and not when the user comes back', async () => {
  const e = await fresh('opens-on-home');
  e.noteOpeningPage('home');
  e.noteOpeningPage('library'); // later pages do not change where the app opened
  assert.equal(e.homeEntrance(), true);
  e.homeEntranceTaken();
  assert.equal(e.homeEntrance(), false);
});

test('Home never fades when the app opened on another page', async () => {
  const e = await fresh('opens-on-library');
  e.noteOpeningPage('library');
  e.noteOpeningPage('home');
  assert.equal(e.homeEntrance(), false);
});
