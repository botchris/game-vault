import assert from 'node:assert/strict';
import { test } from 'node:test';
import { closeRequest, draftFor, keepDraft } from '../src/lib/editDrafts.ts';
import { rebaseInfo } from '../src/lib/links.ts';

const info = (title, links = {}) => ({ title, links, notes: '', fields: {} });
const exists = (...ids) => (id) => ids.includes(id);

test('a changed form is kept per game, and an unchanged one (saved, discarded) is dropped', () => {
  const empty = new Map();
  const a = { info: info('Edited A'), base: { id: 'a' } };
  const one = keepDraft(empty, 'a', a);
  assert.equal(draftFor(one, 'a'), a);
  assert.equal(draftFor(one, 'b'), undefined);
  assert.equal(empty.size, 0, 'the drafts are never changed in place');

  const b = { info: info('Edited B'), base: { id: 'b' } };
  const two = keepDraft(one, 'b', b);
  assert.equal(draftFor(two, 'a'), a, 'moving to another game keeps the first one');
  assert.equal(draftFor(two, 'b'), b);

  const saved = keepDraft(two, 'a', undefined);
  assert.equal(draftFor(saved, 'a'), undefined);
  assert.equal(draftFor(saved, 'b'), b);
  assert.equal(keepDraft(empty, 'a', undefined), empty, 'nothing to drop: the same drafts');
});

test('closing asks first only while a game still in the catalog has a draft', () => {
  const draft = { info: info('Edited'), base: {} };
  assert.deepEqual(closeRequest(new Map(), exists('a')), { confirm: false, count: 0 });
  assert.deepEqual(closeRequest(new Map([['a', draft]]), exists('a')), { confirm: true, count: 1 });
  assert.deepEqual(closeRequest(new Map([['a', draft], ['b', draft]]), exists('a', 'b')), { confirm: true, count: 2 });
  // A game merged into another or deleted has nothing left to save.
  assert.deepEqual(closeRequest(new Map([['a', draft], ['gone', draft]]), exists('a')), { confirm: true, count: 1 });
  assert.deepEqual(closeRequest(new Map([['gone', draft]]), exists('a')), { confirm: false, count: 0 });
});

test('a draft taken back after the game changed keeps the typing and the new links', () => {
  // The draft was left with the game as it was; a link was found automatically meanwhile.
  const stored = { info: info('My title', { steam: '10' }), base: info('Old title', { steam: '10' }) };
  const now = info('Old title', { steam: '10', gog: 'g1' });
  assert.deepEqual(rebaseInfo(stored.base, stored.info, now, (x, y) => x === y), info('My title', { steam: '10', gog: 'g1' }));
});
