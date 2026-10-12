import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_CHECK_ITEMS, chunk, cleanTitles, notOwnedText, summarize, toRows } from '../src/lib/ownership.ts';

test('bullets, numbering, prices, discounts and currency signs go; the title stays whole', () => {
  const { titles, skipped } = cleanTitles([
    '• Lantern Keep',
    '- Echo Valley $19.99',
    '1. Harbor Lights',
    '2) Copper Coast — 4,99 €',
    '(3) Mirror Run -75% $4.99',
    '#4 Night Ferry',
    '€9.99 Paper Comet',
    'Glass Orchard\t$14.99\tSteam',
    '',
    '   ',
  ].join('\n'));
  assert.deepEqual(titles, ['Lantern Keep', 'Echo Valley', 'Harbor Lights', 'Copper Coast', 'Mirror Run', 'Night Ferry', 'Paper Comet', 'Glass Orchard']);
  assert.equal(skipped, 0);
});

test('words that belong to a title are kept', () => {
  const lines = [
    'DLC Hunters',
    'Soundtrack Hero',
    '1983 Revival: Night Shift',
    '7 Nights at Sea',
    'The Price of Silence',
    'Free Runner 2',
    'Ca$h Rally',
    'Mirror Run 2: 50% More Mirrors',
    'Year 2049: 3.5 Moons',
  ];
  assert.deepEqual(cleanTitles(lines.join('\n')).titles, lines);
});

test('add-ons are skipped and counted; duplicates, blank and overlong lines are dropped', () => {
  const { titles, skipped } = cleanTitles([
    'Lantern Keep',
    'lantern  keep',
    'Lantern Keep [DLC]',
    'Lantern Keep - Original Soundtrack',
    'DLC: Lantern Keep Map Pack',
    'Lantern Keep Soundtrack',
    'Lantern Keep OST',
    'Echo Valley (Soundtrack)',
    'Echo Valley',
    'x'.repeat(201),
    '',
  ].join('\r\n'));
  assert.deepEqual(titles, ['Lantern Keep', 'Echo Valley']);
  assert.equal(skipped, 6);
});

test('a long list is split into requests of at most 200 titles, in order', () => {
  const titles = Array.from({ length: 450 }, (_, i) => `Game ${i}`);
  const parts = chunk(titles);
  assert.deepEqual(parts.map((p) => p.length), [MAX_CHECK_ITEMS, MAX_CHECK_ITEMS, 50]);
  assert.deepEqual(parts.flat(), titles);
  assert.deepEqual(chunk([]), []);
});

test('rows follow the pasted order and count what is owned', () => {
  const game = (id) => ({ id, title: id, copies: [], path: `#/game/${id}` });
  const rows = toRows(['A', 'B', 'C', 'D'], [
    { status: 'owned', game: game('a'), candidates: [] },
    { status: 'possible', candidates: [game('b1'), game('b2')] },
    { status: 'not_owned', candidates: [] },
  ]);
  assert.deepEqual(rows.map((r) => r.status), ['owned', 'possible', 'not_owned', 'not_owned']);
  assert.equal(rows[0].game.id, 'a');
  assert.equal(rows[1].candidates.length, 2);
  assert.deepEqual(summarize(rows), { owned: 1, possible: 1, total: 4 });
  assert.equal(notOwnedText(rows), 'C\nD', 'possible ones may be owned: they are not copied');
});

test('an owned answer without a game, or an unknown status, is not owned', () => {
  const rows = toRows(['A', 'B'], [{ status: 'owned', candidates: [] }, { status: 'maybe', candidates: [] }]);
  assert.deepEqual(rows.map((r) => r.status), ['not_owned', 'not_owned']);
});
