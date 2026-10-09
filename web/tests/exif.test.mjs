import assert from 'node:assert/strict';
import { test } from 'node:test';
import { insertSegment, readExifSegment, resetOrientation } from '../src/lib/exif.ts';

/** A TIFF block with IFD0 holding Make (0x010F, a short string inline) and Orientation (0x0112). */
function tiff(little, orientation) {
  const b = new Uint8Array(8 + 2 + 2 * 12 + 4);
  const v = new DataView(b.buffer);
  b.set(little ? [0x49, 0x49] : [0x4d, 0x4d]);
  v.setUint16(2, 42, little);
  v.setUint32(4, 8, little);
  v.setUint16(8, 2, little);
  v.setUint16(10, 0x010f, little); v.setUint16(12, 2, little); v.setUint32(14, 4, little); b.set([0x41, 0x43, 0x4d, 0], 18);
  v.setUint16(22, 0x0112, little); v.setUint16(24, 3, little); v.setUint32(26, 1, little); v.setUint16(30, orientation, little);
  return b;
}

/** A JPEG-shaped byte string: SOI, optional APP0, optional APP1 Exif, SOS with one byte, EOI. */
function jpeg({ app0 = false, exif = null } = {}) {
  const parts = [[0xff, 0xd8]];
  if (app0) parts.push([0xff, 0xe0, 0, 7, 0x4a, 0x46, 0x49, 0x46, 0]);
  if (exif) {
    const body = [0x45, 0x78, 0x69, 0x66, 0, 0, ...exif];
    parts.push([0xff, 0xe1, (body.length + 2) >> 8, (body.length + 2) & 0xff, ...body]);
  }
  parts.push([0xff, 0xda, 0, 3, 0x00, 0x11, 0xff, 0xd9]);
  return new Uint8Array(parts.flat());
}

const orientationOf = (segment, little) => new DataView(segment.buffer, segment.byteOffset).getUint16(10 + 22 + 8, little);

for (const little of [true, false]) {
  test(`a ${little ? 'little' : 'big'}-endian EXIF segment is found and its orientation reset to upright`, () => {
    const original = jpeg({ exif: tiff(little, 6) });
    const segment = readExifSegment(original);
    assert.ok(segment);
    assert.equal(segment[0], 0xff);
    assert.equal(segment[1], 0xe1);
    assert.equal(orientationOf(segment, little), 6);

    const upright = resetOrientation(segment);
    assert.equal(orientationOf(upright, little), 1);
    assert.equal(orientationOf(segment, little), 6, 'the original segment is not changed');
    assert.deepEqual(upright.subarray(10 + 18, 10 + 22), new Uint8Array([0x41, 0x43, 0x4d, 0]), 'other tags are kept');
  });
}

test('the segment goes after the APP0 header of the encoded image, or right after SOI without one', () => {
  const segment = readExifSegment(jpeg({ exif: tiff(true, 1) }));
  const withApp0 = insertSegment(jpeg({ app0: true }), segment);
  assert.deepEqual([...withApp0.subarray(0, 2)], [0xff, 0xd8]);
  assert.deepEqual([...withApp0.subarray(2, 4)], [0xff, 0xe0]);
  assert.deepEqual([...withApp0.subarray(11, 13)], [0xff, 0xe1]);
  assert.ok(readExifSegment(withApp0));

  const bare = insertSegment(jpeg(), segment);
  assert.deepEqual([...bare.subarray(2, 4)], [0xff, 0xe1]);
});

test('files without EXIF, truncated or not JPEG give null and never throw', () => {
  assert.equal(readExifSegment(jpeg()), null);
  assert.equal(readExifSegment(new Uint8Array([0x89, 0x50, 0x4e, 0x47])), null);
  const full = jpeg({ exif: tiff(true, 6) });
  for (let n = 0; n < full.length; n++) {
    assert.doesNotThrow(() => readExifSegment(full.subarray(0, n)));
  }
  const broken = readExifSegment(full).slice();
  new DataView(broken.buffer).setUint32(10 + 4, 0xfffffff0, true); // IFD0 offset far outside
  assert.doesNotThrow(() => resetOrientation(broken));
});
