package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tiffWithDates builds the TIFF part of an EXIF segment: IFD0 with DateTime (0x0132) and a pointer
// to the Exif IFD (0x8769), which holds DateTimeOriginal (0x9003). An empty original leaves that
// field zeroed, as some cameras do.
//
//	0  header | 8 IFD0 (2 entries) | 38 Exif IFD (1 entry) | 56 DateTime | 76 DateTimeOriginal
func tiffWithDates(bo binary.ByteOrder, dateTime, original string) []byte {
	b := make([]byte, 96)
	if bo == binary.LittleEndian {
		copy(b, "II")
	} else {
		copy(b, "MM")
	}

	bo.PutUint16(b[2:], 42)
	bo.PutUint32(b[4:], 8)

	entry := func(at int, tag, typ uint16, count, value uint32) {
		bo.PutUint16(b[at:], tag)
		bo.PutUint16(b[at+2:], typ)
		bo.PutUint32(b[at+4:], count)
		bo.PutUint32(b[at+8:], value)
	}

	bo.PutUint16(b[8:], 2)
	entry(10, 0x0132, 2, 20, 56)
	entry(22, 0x8769, 4, 1, 38)
	bo.PutUint16(b[38:], 1)
	entry(40, 0x9003, 2, 20, 76)
	copy(b[56:], dateTime)
	copy(b[76:], original)

	return b
}

// jpegWithExif encodes a small image and inserts an EXIF segment holding tiff after its SOI.
func jpegWithExif(t *testing.T, tiff []byte) []byte {
	t.Helper()

	var enc bytes.Buffer
	require.NoError(t, jpeg.Encode(&enc, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil))

	if tiff == nil {
		return enc.Bytes()
	}

	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	out = append(out, seg...)

	return append(out, enc.Bytes()[2:]...)
}

func TestExifTakenAt(t *testing.T) {
	want := time.Date(2024, 3, 9, 18, 4, 5, 0, time.UTC)

	cases := []struct {
		name string
		tiff []byte
		want time.Time
		ok   bool
	}{
		{"little-endian, with DateTimeOriginal", tiffWithDates(binary.LittleEndian, "2001:01:01 00:00:00", "2024:03:09 18:04:05"), want, true},
		{"big-endian, with DateTimeOriginal", tiffWithDates(binary.BigEndian, "2001:01:01 00:00:00", "2024:03:09 18:04:05"), want, true},
		{"no DateTimeOriginal: falls back to DateTime", tiffWithDates(binary.LittleEndian, "2024:03:09 18:04:05", ""), want, true},
		{"dates unset by the camera", tiffWithDates(binary.BigEndian, "0000:00:00 00:00:00", "0000:00:00 00:00:00"), time.Time{}, false},
		{"no EXIF at all", nil, time.Time{}, false},
	}

	for _, c := range cases {
		t.Run("GIVEN a JPEG with "+c.name+" THEN the date is read as expected", func(t *testing.T) {
			got, ok := exifTakenAt(jpegWithExif(t, c.tiff))
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("GIVEN truncated or corrupted files THEN reading never panics", func(t *testing.T) {
		full := jpegWithExif(t, tiffWithDates(binary.LittleEndian, "2024:03:09 18:04:05", "2024:03:09 18:04:05"))
		for n := 0; n < 200 && n < len(full); n++ {
			assert.NotPanics(t, func() { exifTakenAt(full[:n]) })
		}

		bad := bytes.Clone(full)
		binary.LittleEndian.PutUint32(bad[12+4:], 0xFFFFFFF0) // IFD0 offset far outside
		assert.NotPanics(t, func() { exifTakenAt(bad) })

		huge := bytes.Clone(full)
		binary.LittleEndian.PutUint16(huge[12+8:], 0xFFFF) // 65535 IFD0 entries
		assert.NotPanics(t, func() { exifTakenAt(huge) })
	})
}
