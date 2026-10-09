// EXIF helpers for photo uploads. The browser re-encodes photos on a canvas, which drops their
// metadata; these copy the original's EXIF segment into the reduced JPEG. Pure functions without
// imports, so Node runs their test directly (web/tests/exif.test.mjs).

const EXIF_ID = [0x45, 0x78, 0x69, 0x66, 0, 0]; // "Exif\0\0"

/** Returns the JPEG's EXIF segment (APP1, marker and length included), or null when it has none. */
export function readExifSegment(jpeg: Uint8Array): Uint8Array | null {
  if (jpeg.length < 4 || jpeg[0] !== 0xff || jpeg[1] !== 0xd8) return null;
  let i = 2;
  while (i + 4 <= jpeg.length) {
    if (jpeg[i] !== 0xff) return null;
    const marker = jpeg[i + 1]!;
    if (marker === 0xda || marker === 0xd9) return null; // image data: the metadata came before
    const len = (jpeg[i + 2]! << 8) | jpeg[i + 3]!;
    if (len < 2 || i + 2 + len > jpeg.length) return null;
    if (marker === 0xe1 && len >= 8 && EXIF_ID.every((b, k) => jpeg[i + 4 + k] === b)) {
      return jpeg.slice(i, i + 2 + len);
    }
    i += 2 + len;
  }
  return null;
}

/**
 * Returns a copy of an EXIF segment whose orientation (tag 0x0112 of IFD0) says upright: the
 * canvas already drew the pixels rotated, so keeping the old value would rotate them twice.
 */
export function resetOrientation(segment: Uint8Array): Uint8Array {
  const out = segment.slice();
  const tiff = 10; // FF E1, length (2 bytes), "Exif\0\0"
  if (out.length < tiff + 8) return out;
  const little = out[tiff] === 0x49;
  const view = new DataView(out.buffer, out.byteOffset, out.byteLength);
  const ifd0 = tiff + view.getUint32(tiff + 4, little);
  if (ifd0 + 2 > out.length) return out;
  const entries = view.getUint16(ifd0, little);
  for (let k = 0; k < entries; k++) {
    const e = ifd0 + 2 + 12 * k;
    if (e + 12 > out.length) break;
    if (view.getUint16(e, little) === 0x0112) {
      view.setUint16(e + 8, 1, little);
      break;
    }
  }
  return out;
}

/** Returns the JPEG with the segment inserted after its JFIF header (APP0), or after SOI. */
export function insertSegment(jpeg: Uint8Array, segment: Uint8Array): Uint8Array<ArrayBuffer> {
  let at = 2;
  if (jpeg.length >= 6 && jpeg[2] === 0xff && jpeg[3] === 0xe0) at = 4 + ((jpeg[4]! << 8) | jpeg[5]!);
  const out = new Uint8Array(jpeg.length + segment.length);
  out.set(jpeg.subarray(0, at), 0);
  out.set(segment, at);
  out.set(jpeg.subarray(at), at + segment.length);
  return out;
}
