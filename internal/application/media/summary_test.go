package media

import "testing"

func TestSummaryYear(t *testing.T) {
	for in, want := range map[string]int{"2010-05-18": 2010, "14 MAY 2019": 2019, "18 ABR 2011": 2011, "Q3 2026": 2026, "": 0, "Coming soon": 0} {
		if got := (Summary{ReleaseDate: in}).Year(); got != want {
			t.Errorf("Year(%q) = %d, want %d", in, got, want)
		}
	}
}
