import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_CHECK_ITEMS, ShortAnswerError, checkAll, chunk, cleanTitles, notOwnedText, summarize, toRows } from '../src/lib/ownership.ts';

test('bullets, numbering, prices, discounts and currency signs go; the title stays whole', () => {
  const { titles, skipped } = cleanTitles([
    '• Lantern Keep',
    '- Echo Valley $19.99',
    '1. Harbor Lights',
    '2) Copper Coast — 4,99 €',
    '(3) Mirror Run -75% $4.99',
    '#4. Night Ferry',
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

test('table lines: rank, number and price cells before the title are passed over', () => {
  const { titles } = cleanTitles(['1\tGlass Orchard\t$14.99', '#2\t$4.99\tPaper Comet\tSteam', '\t\tNight Ferry', '1983\t'].join('\n'));
  assert.deepEqual(titles, ['Glass Orchard', 'Paper Comet', 'Night Ferry', '1983']);
});

test('titles that only look like markers are kept, real markers still go', () => {
  const kept = ['DLC-Quest', '#1 Fan', '#7 Days', 'Soundtrack-Hero'];
  assert.deepEqual(cleanTitles(kept.join('\n')).titles, kept);
  const { titles, skipped } = cleanTitles(['DLC - Map Pack', 'DLC: Map Pack', '#3. Night Ferry'].join('\n'));
  assert.deepEqual(titles, ['Night Ferry']);
  assert.equal(skipped, 2);
});

test('overlong lines are counted', () => {
  const { titles, tooLong } = cleanTitles(['A', 'x'.repeat(201), 'y'.repeat(300)].join('\n'));
  assert.deepEqual(titles, ['A']);
  assert.equal(tooLong, 2);
});

const notOwned = (n) => Array.from({ length: n }, () => ({ status: 'not_owned', candidates: [] }));
const names = (n, from = 0) => Array.from({ length: n }, (_, i) => `G${from + i}`);

test('checkAll joins the answers of every request in order', async () => {
  const sizes = [];
  const { rows, error } = await checkAll(names(450), async (part) => { sizes.push(part.length); return notOwned(part.length); });
  assert.equal(error, undefined);
  assert.deepEqual(sizes, [200, 200, 50]);
  assert.deepEqual(rows.map((r) => r.title), names(450));
});

test('a failing later request keeps the earlier results and reports the error', async () => {
  let calls = 0;
  const boom = new Error('boom');
  const { rows, error } = await checkAll(names(450), async (part) => {
    if (++calls === 2) throw boom;
    return notOwned(part.length);
  });
  assert.equal(error, boom);
  assert.deepEqual(rows.map((r) => r.title), names(200));
});

test('an answer shorter than its request is an error, not silent not-owned', async () => {
  const { rows, error } = await checkAll(names(5), async () => notOwned(3));
  assert.ok(error instanceof ShortAnswerError);
  assert.deepEqual(rows, []);
});
