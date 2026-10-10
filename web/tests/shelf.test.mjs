import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_BOX_ASPECT, boxShape } from '../src/lib/shelf.ts';

test('a box as tall as the cell fills it', () => {
  assert.deepEqual(boxShape(200, 300), { width: 100, height: 100 });
});

test('a wider box (a DS case) keeps the full width and stands shorter', () => {
  const s = boxShape(300, 270); // 10 / 9
  assert.equal(s.width, 100);
  assert.ok(Math.abs(s.height - 60) < 1e-9, `height ${s.height}`);
});

test('a narrower box (a Switch case) keeps the full height and stands narrower', () => {
  const s = boxShape(105, 170);
  assert.equal(s.height, 100);
  assert.ok(Math.abs(s.width - (105 / 170) / (2 / 3) * 100) < 1e-9, `width ${s.width}`);
});

test('an image without a size fills the cell', () => {
  assert.deepEqual(boxShape(0, 0), { width: 100, height: 100 });
});

test('a landscape banner stands no wider than the widest box when capped', () => {
  const s = boxShape(1920, 1080, MAX_BOX_ASPECT);
  assert.equal(s.width, 100);
  assert.ok(Math.abs(s.height - (2 / 3) / MAX_BOX_ASPECT * 100) < 1e-9, `height ${s.height}`);
  assert.ok(boxShape(1920, 1080).height < s.height, 'uncapped, it is shown whole');
});
