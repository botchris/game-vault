package media

import (
	"encoding/binary"
	"time"
)

// EXIF tags read to date a photo.
const (
	tagDateTime         = 0x0132
	tagExifIFD          = 0x8769
	tagDateTimeOriginal = 0x9003
)

// exifTakenAt reads when a JPEG photo was taken from its EXIF metadata: DateTimeOriginal, or the
// file's DateTime when the camera did not record it. EXIF dates have no zone (cameras write their
// local clock), so the reading is returned as UTC. Malformed metadata yields false, never a panic:
// every offset is checked against the segment.
func exifTakenAt(jpeg []byte) (time.Time, bool) {
	tiff, ok := exifSegment(jpeg)
	if !ok || len(tiff) < 8 {
		return time.Time{}, false
	}

	r := tiffReader{b: tiff}

	switch string(tiff[:2]) {
	case "II":
		r.bo = binary.LittleEndian
	case "MM":
		r.bo = binary.BigEndian
	default:
		return time.Time{}, false
	}

	ifd0, ok := r.u32(4)
	if !ok {
		return time.Time{}, false
	}

	if e, ok := r.entry(ifd0, tagExifIFD); ok {
		if sub, ok := r.u32(e + 8); ok {
			if t, ok := r.dateTime(sub, tagDateTimeOriginal); ok {
				return t, true
			}
		}
	}

	return r.dateTime(ifd0, tagDateTime)
}

// exifSegment returns the TIFF data of the JPEG's EXIF (APP1) segment.
func exifSegment(b []byte) ([]byte, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil, false
	}

	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return nil, false
		}

		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data or end: metadata comes before
			return nil, false
		}

		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return nil, false
		}

		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return seg[6:], true
		}

		i += 2 + n
	}

	return nil, false
}

// tiffReader reads the TIFF structure inside an EXIF segment with bounds checks.
type tiffReader struct {
	b  []byte
	bo binary.ByteOrder
}

func (r tiffReader) u16(off int) (int, bool) {
	if off < 0 || off+2 > len(r.b) {
		return 0, false
	}

	return int(r.bo.Uint16(r.b[off:])), true
}

func (r tiffReader) u32(off int) (int, bool) {
	if off < 0 || off+4 > len(r.b) {
		return 0, false
	}

	v := r.bo.Uint32(r.b[off:])
	if v > uint32(len(r.b)) {
		return 0, false
	}

	return int(v), true
}

// entry returns the position of tag's 12-byte entry in the IFD at ifd.
func (r tiffReader) entry(ifd, tag int) (int, bool) {
	n, ok := r.u16(ifd)
	if !ok {
		return 0, false
	}

	for i := range n {
		e := ifd + 2 + 12*i

		t, ok := r.u16(e)
		if !ok {
			return 0, false
		}

		if t == tag {
			return e, true
		}
	}

	return 0, false
}

// dateTime reads an ASCII date ("2006:01:02 15:04:05") stored out of line by tag in the IFD.
func (r tiffReader) dateTime(ifd, tag int) (time.Time, bool) {
	e, ok := r.entry(ifd, tag)
	if !ok {
		return time.Time{}, false
	}

	off, ok := r.u32(e + 8)
	if !ok || off+19 > len(r.b) {
		return time.Time{}, false
	}

	t, err := time.Parse("2006:01:02 15:04:05", string(r.b[off:off+19]))
	if err != nil {
		return time.Time{}, false
	}

	return t, true
}
