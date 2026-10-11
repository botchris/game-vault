import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  CELL_ASPECT, ColorCache, MAX_LIGHTNESS, MAX_SATURATION, MIN_LIGHTNESS, averageColor, backdropColor, dominantColor, edgePixels, hslToRgb,
  rgbToHsl, toCss,
} from '../src/lib/dominantColor.ts';

const px = (...pixels) => pixels.flat();

test('the average is the mean of the opaque pixels', () => {
  assert.deepEqual(averageColor(px([200, 0, 0, 255], [0, 100, 50, 255])), { r: 100, g: 50, b: 25 });
});

test('near-transparent pixels are skipped', () => {
  assert.deepEqual(averageColor(px([10, 20, 30, 255], [255, 255, 255, 0], [255, 255, 255, 100])), { r: 10, g: 20, b: 30 });
});

test('a fully transparent image has no colour', () => {
  assert.equal(averageColor(px([255, 255, 255, 0], [0, 0, 0, 10])), null);
  assert.equal(averageColor([]), null);
  assert.equal(dominantColor(px([1, 2, 3, 0]), 1, 1, CELL_ASPECT), null);
});

test('HSL conversions round-trip', () => {
  for (const c of [{ r: 0, g: 0, b: 0 }, { r: 255, g: 255, b: 255 }, { r: 200, g: 30, b: 90 }, { r: 12, g: 180, b: 77 }, { r: 40, g: 60, b: 230 }]) {
    assert.deepEqual(hslToRgb(rgbToHsl(c)), c);
  }
});

test('a white cover gets a band no lighter than the maximum', () => {
  const { l } = rgbToHsl(backdropColor({ r: 255, g: 255, b: 255 }));
  assert.ok(Math.abs(l - MAX_LIGHTNESS) < 0.01, `lightness ${l}`);
});

test('a black cover gets a band no darker than the minimum', () => {
  const { l } = rgbToHsl(backdropColor({ r: 0, g: 0, b: 0 }));
  assert.ok(Math.abs(l - MIN_LIGHTNESS) < 0.01, `lightness ${l}`);
});

// A width × height image whose pixel colour comes from color(x, y).
function image(width, height, color) {
  const data = [];
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) data.push(...color(x, y), 255);
  return data;
}
const RED = [200, 30, 30];
const BLUE = [30, 30, 200];
const GREEN = [30, 160, 60];
const hue = (css) => {
  const [r, g, b] = css.match(/\d+/g).map(Number);
  return rgbToHsl({ r, g, b }).h;
};

test('a tall image (bands left and right) takes its colour from its sides', () => {
  const data = image(16, 16, (x) => (x < 2 || x >= 14 ? RED : BLUE));
  const color = dominantColor(data, 16, 16, 105 / 170);
  assert.ok(hue(color) < 5 || hue(color) > 355, color);
});

test('a wide image (bands above and below) takes its colour from its top and bottom', () => {
  const data = image(16, 16, (_x, y) => (y < 2 || y >= 14 ? GREEN : RED));
  const color = dominantColor(data, 16, 16, 10 / 9);
  assert.ok(Math.abs(hue(color) - rgbToHsl({ r: GREEN[0], g: GREEN[1], b: GREEN[2] }).h) < 3, color);
});

test('the edge strips hold the right pixels', () => {
  const data = image(4, 4, (x, y) => [x, y, 0]);
  const cols = edgePixels(data, 4, 4, 0.5, CELL_ASPECT, 1);
  assert.equal(cols.length, 8 * 4); // the first and last column, 4 rows each
  for (let i = 0; i < cols.length; i += 4) assert.ok(cols[i] === 0 || cols[i] === 3);
  const rows = edgePixels(data, 4, 4, 2, CELL_ASPECT, 1);
  assert.equal(rows.length, 8 * 4);
  for (let i = 0; i < rows.length; i += 4) assert.ok(rows[i + 1] === 0 || rows[i + 1] === 3);
  assert.equal(edgePixels(data, 4, 4, CELL_ASPECT).length, data.length); // fills the cell: everything
  assert.equal(edgePixels(px([1, 1, 1, 255]), 1, 1, 0.3, CELL_ASPECT, 2).length, 4); // never twice
});

test('a colour already in range keeps its hue and lightness, and a vivid one is toned down', () => {
  const mid = { r: 40, g: 70, b: 110 };
  const before = rgbToHsl(mid);
  const after = rgbToHsl(backdropColor(mid));
  assert.ok(Math.abs(before.h - after.h) < 2 && Math.abs(before.l - after.l) < 0.01);
  const vivid = rgbToHsl(backdropColor({ r: 255, g: 0, b: 0 }));
  assert.ok(vivid.s <= MAX_SATURATION + 0.01, `saturation ${vivid.s}`);
  assert.ok(vivid.h < 2 || vivid.h > 358, `hue ${vivid.h}`);
});

test('the colour is CSS', () => {
  assert.equal(toCss({ r: 1, g: 2, b: 3 }), 'rgb(1 2 3)');
  assert.match(dominantColor(px([120, 40, 40, 255]), 1, 1, CELL_ASPECT), /^rgb\(\d+ \d+ \d+\)$/);
});

test('the cache forgets the oldest colour beyond its cap', () => {
  const c = new ColorCache(2);
  c.set('a', 'rgb(1 1 1)');
  c.set('b', 'rgb(2 2 2)');
  c.set('a', 'rgb(3 3 3)'); // refreshed: now the newest
  c.set('c', 'rgb(4 4 4)');
  assert.equal(c.size, 2);
  assert.equal(c.get('b'), undefined);
  assert.equal(c.get('a'), 'rgb(3 3 3)');
  assert.deepEqual(c.toJSON(), [['a', 'rgb(3 3 3)'], ['c', 'rgb(4 4 4)']]);
});

test('the cache survives JSON and keeps its cap when read back', () => {
  const c = new ColorCache(3);
  for (const k of ['a', 'b', 'c']) c.set(k, `rgb(0 0 ${k.charCodeAt(0)})`);
  const back = ColorCache.parse(2, JSON.stringify(c));
  assert.deepEqual(back.toJSON(), [['b', 'rgb(0 0 98)'], ['c', 'rgb(0 0 99)']]);
});

test('unreadable stored data gives an empty cache', () => {
  for (const s of [null, '', 'not json', '{"a":1}', '[["a"],[1,2],"x"]']) {
    assert.equal(ColorCache.parse(5, s).size, 0, String(s));
  }
});
