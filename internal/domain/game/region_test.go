package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
)

func TestRegion(t *testing.T) {
	t.Run("GIVEN the region ids and the ways people write them", func(t *testing.T) {
		for in, want := range map[string]Region{
			"pal": RegionPAL, "PAL": RegionPAL, "NTSC-U": RegionNTSCU, "ntsc u": RegionNTSCU, "NTSC-J": RegionNTSCJ,
			"ntsc_k": RegionNTSCK, "NTSC-C": RegionNTSCC, "Asia": RegionAsia, "Region free": RegionFree, "region_free": RegionFree,
		} {
			got, ok := ParseRegion(in)
			assert.True(t, ok, in)
			assert.Equal(t, want, got, in)
		}

		_, ok := ParseRegion("Europe")
		assert.False(t, ok)
	})

	t.Run("GIVEN a key copy with a region", func(t *testing.T) {
		d := CopyDetails{
			Kind:   KindKey,
			Region: RegionNTSCJ,
		}

		t.Run("WHEN it is normalized and turned into a physical copy", func(t *testing.T) {
			got, err := d.normalize()
			require.NoError(t, err)

			got.Kind, got.Status = KindPhysical, "" // a status belongs to its kind
			got, err = got.normalize()
			require.NoError(t, err)

			t.Run("THEN the region stays: it belongs to every kind", func(t *testing.T) {
				assert.Equal(t, RegionNTSCJ, got.Region)
			})
		})
	})

	t.Run("GIVEN an unknown region", func(t *testing.T) {
		_, err := CopyDetails{
			Kind:   KindPhysical,
			Region: "mars",
		}.normalize()
		assert.Equal(t, errkind.Invalid, errkind.Of(err))
	})
}

func TestCopy_applyImportRegion(t *testing.T) {
	t.Run("GIVEN a copy with a region", func(t *testing.T) {
		newCopy := func() *Copy {
			return &Copy{CopyDetails: CopyDetails{
				Kind:   KindPhysical,
				Region: RegionPAL,
			}}
		}

		t.Run("WHEN an import states another region", func(t *testing.T) {
			c := newCopy()
			changed := c.applyImport(CopyDetails{
				Kind:   KindPhysical,
				Region: RegionNTSCU,
			})

			t.Run("THEN the region is set", func(t *testing.T) {
				assert.True(t, changed)
				assert.Equal(t, RegionNTSCU, c.Region)
			})
		})

		t.Run("WHEN an import states none", func(t *testing.T) {
			c := newCopy()
			c.applyImport(CopyDetails{Kind: KindPhysical})

			t.Run("THEN the copy keeps its region", func(t *testing.T) {
				assert.Equal(t, RegionPAL, c.Region)
			})
		})
	})
}
