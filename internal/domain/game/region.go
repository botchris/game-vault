package game

import (
	"slices"
	"strings"
)

// Region is the market a copy was made for (PAL, NTSC-J…), which decides whether it plays on a
// given console. Empty means not stated.
type Region string

// Values of Region, in display order.
const (
	RegionPAL   Region = "pal"
	RegionNTSCU Region = "ntsc_u"
	RegionNTSCJ Region = "ntsc_j"
	RegionNTSCK Region = "ntsc_k"
	RegionNTSCC Region = "ntsc_c"
	RegionAsia  Region = "asia"
	RegionFree  Region = "region_free"
)

// regions lists every region in display order.
var regions = []Region{RegionPAL, RegionNTSCU, RegionNTSCJ, RegionNTSCK, RegionNTSCC, RegionAsia, RegionFree}

// Regions returns every region in display order (a copy: callers cannot change the domain's list).
func Regions() []Region { return slices.Clone(regions) }

// Valid reports whether r is a known region or empty.
func (r Region) Valid() bool { return r == "" || slices.Contains(regions, r) }

// ParseRegion reads a region from its id or from the way people write it ("PAL", "ntsc-u",
// "NTSC U", "Region free"). The second result is false when s is not a region.
func ParseRegion(s string) (Region, bool) {
	r := Region(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(s))))

	if !slices.Contains(regions, r) {
		return "", false
	}

	return r, true
}
