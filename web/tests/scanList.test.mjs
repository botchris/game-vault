import assert from 'node:assert/strict';
import { test } from 'node:test';
import { normalizeBarcode, validBarcode } from '../src/lib/barcode.ts';
import {
  addCode, amend, applyResults, chunkItems, choose, loadRows, plusOne, remove, resolved, restore, retry, saveRows, sendItems, settle, status, summary,
} from '../src/features/scan/scanList.ts';

const DS3 = '5030934110075';
const answer = (over = {}) => ({
  barcode: DS3, owned: [], warnings: [], existing: [],
  match: { raw: 'Dead Space 3 X360', title: 'Dead Space 3', platform: 'Xbox 360', edition: '', providerId: 'cex' },
  suggestions: [{ title: 'Dead Space 3', platform: 'Xbox 360', coverUrl: 'https://c/ds3.jpg', thumbUrl: 'https://c/ds3-t.jpg', label: '' }],
  ...over,
});
const scanned = (code = DS3, id = 'r1') => addCode([], code, id).rows;

test('barcodes: check digit, and UPC-A as EAN-13', () => {
  assert.equal(validBarcode('5030934110075'), true);
  assert.equal(validBarcode('5030934110076'), false);
  assert.equal(normalizeBarcode('0-45496-59024-6'), '0045496590246');
  assert.equal(normalizeBarcode('5030934110075'), '5030934110075');
});

test('a new code joins the top of the list looking up; a repeat or an invalid code changes nothing', () => {
  let r = addCode([], DS3, 'r1');
  assert.equal(r.outcome, 'added');
  assert.deepEqual(r.rows.map((x) => [x.id, x.code, x.count, x.phase]), [['r1', DS3, 1, 'looking']]);
  r = addCode(r.rows, '045496590246', 'r2');
  assert.equal(r.rows[0].id, 'r2', 'newest first');
  const again = addCode(r.rows, '0045496590246', 'r3');
  assert.equal(again.outcome, 'repeat', 'UPC-A and its EAN-13 form are the same box');
  assert.equal(again.rows, r.rows);
  assert.equal(addCode(r.rows, '123', 'r4').outcome, 'invalid');
});

test('a row resolves by itself from the answer, the batch platform winning', () => {
  const rows = settle(scanned(), 'r1', answer());
  assert.equal(status(rows[0], ''), 'ready');
  assert.deepEqual(resolved(rows[0], ''), { title: 'Dead Space 3', platform: 'Xbox 360', edition: '', coverUrl: 'https://c/ds3.jpg', thumbUrl: 'https://c/ds3-t.jpg', gameId: '' });
  assert.equal(resolved(rows[0], 'PS3').platform, 'PS3');
});

test('one game of yours with that title makes it a copy of it; several make it a row to review', () => {
  const one = settle(scanned(), 'r1', answer({ existing: [{ id: 'g1', title: 'Dead Space 3' }] }));
  assert.equal(resolved(one[0], '').gameId, 'g1');
  assert.equal(status(one[0], ''), 'ready');
  const two = settle(scanned(), 'r1', answer({ existing: [{ id: 'g1', title: 'Dead Space 3' }, { id: 'g2', title: 'Dead Space 3' }] }));
  assert.equal(status(two[0], ''), 'review');
});

test('unknown codes, products without platform and no suggestion are to review until chosen', () => {
  const unknown = settle(scanned(), 'r1', answer({ match: null, suggestions: [] }));
  assert.equal(status(unknown[0], ''), 'review');
  const noPlatform = settle(scanned(), 'r1', answer({ match: { raw: 'x', title: 'x', platform: '', edition: '', providerId: 'cex' }, suggestions: [] }));
  assert.equal(status(noPlatform[0], ''), 'review');
  const chosen = choose(unknown, 'r1', { title: 'Halo 3', platform: 'Xbox 360', edition: '', coverUrl: '', thumbUrl: '', gameId: '' });
  assert.equal(status(chosen[0], ''), 'ready');
  const noPlatformChosen = choose(unknown, 'r1', { title: 'Halo 3', platform: '', edition: '', coverUrl: '', thumbUrl: '', gameId: '' });
  assert.equal(status(noPlatformChosen[0], ''), 'review');
  assert.equal(status(noPlatformChosen[0], 'PS3'), 'ready', 'the batch platform fills it');
});

test('a code you already have is not sent unless +1', () => {
  let rows = settle(scanned(), 'r1', answer({ owned: [{ gameId: 'g1', title: 'Dead Space 3', platform: 'Xbox 360' }] }));
  assert.equal(status(rows[0], ''), 'owned');
  assert.equal(rows[0].count, 0);
  assert.deepEqual(sendItems(rows, ''), []);
  rows = plusOne(rows, 'r1');
  assert.deepEqual(sendItems(rows, '').map((i) => [i.clientId, i.gameId]), [['r1:0', 'g1']]);
});

test('+1 pressed while looking up survives an owned answer', () => {
  const rows = settle(plusOne(scanned(), 'r1'), 'r1', answer({ owned: [{ gameId: 'g1', title: 'Dead Space 3', platform: 'Xbox 360' }] }));
  assert.equal(rows[0].count, 1);
});

test('failed lookups can be retried; send items carry one item per copy', () => {
  let rows = settle(scanned(), 'r1', undefined, 'network down');
  assert.equal(status(rows[0], ''), 'error');
  assert.equal(rows[0].error, 'network down');
  rows = retry(rows, 'r1');
  assert.equal(status(rows[0], ''), 'looking');
  rows = plusOne(settle(rows, 'r1', answer()), 'r1');
  assert.deepEqual(sendItems(rows, 'PS3'), [
    { clientId: 'r1:0', rowId: 'r1', gameId: '', title: 'Dead Space 3', coverUrl: 'https://c/ds3.jpg', platform: 'PS3', edition: '', barcode: DS3 },
    { clientId: 'r1:1', rowId: 'r1', gameId: '', title: 'Dead Space 3', coverUrl: 'https://c/ds3.jpg', platform: 'PS3', edition: '', barcode: DS3 },
  ]);
});

test('remove and restore put a row back where it was', () => {
  const rows = addCode(scanned(), '045496590246', 'r2').rows;
  const { rows: left, removed } = remove(rows, 'r1');
  assert.deepEqual(left.map((r) => r.id), ['r2']);
  assert.deepEqual(restore(left, removed).map((r) => r.id), ['r2', 'r1']);
});

test('results remove saved rows and keep failed ones with their error', () => {
  let rows = settle(scanned(), 'r1', answer());
  rows = settle(addCode(rows, '045496590246', 'r2').rows, 'r2', answer({ barcode: '0045496590246' }));
  rows = plusOne(rows, 'r2');
  const out = applyResults(rows, [
    { clientId: 'r1:0', gameId: 'g9', error: '' },
    { clientId: 'r2:0', gameId: 'g9', error: '' },
    { clientId: 'r2:1', gameId: '', error: 'the game no longer exists; choose another one' },
  ]);
  assert.deepEqual(out.rows.map((r) => [r.id, r.count, r.error]), [['r2', 1, 'the game no longer exists; choose another one']]);
  assert.deepEqual(out.saved.map((s) => [s.row.id, s.gameId]), [['r2', 'g9'], ['r1', 'g9']]);
});

test('summary counts rows by state and the copies Send would add', () => {
  let rows = settle(scanned(), 'r1', answer());
  rows = addCode(rows, '045496590246', 'r2').rows;
  assert.deepEqual(summary(rows, ''), { ready: 1, review: 0, owned: 0, looking: 1, error: 0, copies: 1 });
});

test('the list round-trips through storage; anything else loads empty', () => {
  const rows = settle(scanned(), 'r1', answer());
  assert.deepEqual(loadRows(saveRows(rows)), rows);
  assert.deepEqual(loadRows(null), []);
  assert.deepEqual(loadRows('{oops'), []);
  assert.deepEqual(loadRows(JSON.stringify({ v: 2, rows })), []);
  assert.deepEqual(loadRows(JSON.stringify({ v: 1, rows: [{ id: 'x' }, ...rows] })), rows, 'broken rows are dropped');
});

test('changes to a row apply to its current choice, so two quick edits both stay', () => {
  let rows = settle(scanned(), 'r1', answer({ match: null, suggestions: [] }));
  rows = amend(rows, 'r1', { title: 'Halo 3' });
  rows = amend(rows, 'r1', { platform: 'Xbox 360' });
  assert.equal(resolved(rows[0], '').title, 'Halo 3');
  assert.equal(resolved(rows[0], '').platform, 'Xbox 360');
  assert.equal(status(rows[0], ''), 'ready');
  // The batch platform shown in the detail is not written into the row.
  const resolvedRow = settle(scanned(), 'r1', answer());
  assert.equal(amend(resolvedRow, 'r1', { edition: 'GOTY' })[0].choice.platform, 'Xbox 360');
});

test('a stored list with rows of the right format but broken insides drops those rows', () => {
  const good = settle(scanned(), 'r1', answer())[0];
  const broken = [
    { ...good, id: 'a', answer: { barcode: 'x' } },
    { ...good, id: 'b', choice: { gameId: '' } },
    { ...good, id: 'c', count: Infinity },
    { ...good, id: 'd', count: -1 },
    { ...good, id: 'e', answer: { ...good.answer, match: 'nope' } },
  ];
  const rows = loadRows(JSON.stringify({ v: 1, rows: [...broken, good] }));
  assert.deepEqual(rows.map((r) => r.id), ['r1']);
  assert.doesNotThrow(() => summary(rows, ''));
});

test('copies added with +1 while a send was running are kept', () => {
  let rows = settle(scanned(), 'r1', answer());
  const sent = sendItems(rows, '');
  rows = plusOne(rows, 'r1'); // pressed while the request was in flight
  const out = applyResults(rows, sent.map((i) => ({ clientId: i.clientId, gameId: 'g1', error: '' })));
  assert.deepEqual(out.rows.map((r) => [r.id, r.count, r.error]), [['r1', 1, undefined]]);
});

test('a row whose copy failed to save is to review until it is changed', () => {
  let rows = settle(scanned(), 'r1', answer({ existing: [{ id: 'g1', title: 'Dead Space 3' }] }));
  rows = applyResults(rows, [{ clientId: 'r1:0', gameId: '', error: 'the game no longer exists; choose another one' }]).rows;
  assert.equal(status(rows[0], ''), 'review');
  assert.deepEqual(sendItems(rows, ''), [], 'not sent again as it is');
  rows = amend(rows, 'r1', { gameId: '' });
  assert.equal(rows[0].error, undefined);
  assert.equal(status(rows[0], ''), 'ready');
});

test('send items are split into requests at row boundaries, keeping one new game together', () => {
  let rows = [];
  for (let i = 0; i < 3; i++) {
    rows = addCode(rows, ['5030934110075', '5026555255042', '3307215643006'][i], `r${i}`).rows;
    rows = settle(rows, `r${i}`, answer({ barcode: rows[0].code }));
  }
  rows = amend(rows, 'r0', { title: 'Halo 3' });
  rows = amend(rows, 'r1', { title: 'Gears' });
  rows = amend(rows, 'r2', { title: 'halo 3!' });
  for (let n = 0; n < 2; n++) rows = plusOne(rows, 'r1'); // r1: 3 copies
  const chunks = chunkItems(sendItems(rows, ''), 3);
  for (const c of chunks) assert.ok(c.length <= 3 || new Set(c.map((i) => i.rowId)).size === 1);
  const where = (rowId) => chunks.findIndex((c) => c.some((i) => i.rowId === rowId));
  assert.equal(new Set(chunks[where('r1')].map((i) => i.rowId)).size, 1, 'a row is never split');
  assert.equal(where('r0'), where('r2'), 'rows for the same new game go together');
});
