import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CELL_ASPECT, fillsCell } from '../src/lib/coverFit.ts';

test('a 2:3 cover fills the cell', () => {
  assert.equal(fillsCell(600, 900), true);
  assert.equal(fillsCell(460, 215 * 3.2), true); // 460×688: 0.3% off
});

test('a cover within about 2% of the cell fills it, one further off does not', () => {
  assert.equal(fillsCell(2 * 1.019, 3), true);
  assert.equal(fillsCell(2 * 0.981, 3), true);
  assert.equal(fillsCell(2 * 1.03, 3), false);
  assert.equal(fillsCell(2 * 0.97, 3), false);
});

test('a wide box (a DS case, a banner) and a tall one (a Switch case) leave gaps', () => {
  assert.equal(fillsCell(300, 270), false);
  assert.equal(fillsCell(460, 215), false);
  assert.equal(fillsCell(105, 170), false);
});

test('an image without a size counts as filling it', () => {
  assert.equal(fillsCell(0, 0), true);
  assert.equal(fillsCell(100, 0), true);
  assert.equal(fillsCell(Number.NaN, 10), true);
});

test('the cell aspect can be given', () => {
  assert.equal(fillsCell(100, 100, 1), true);
  assert.equal(fillsCell(200, 300, 1), false);
  assert.equal(CELL_ASPECT, 2 / 3);
});
