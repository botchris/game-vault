package source

import "fmt"

// Skipped counts the items a scan leaves out on purpose (DLC, subscription games, what is not a
// game), by reason, so the scan says how many and why: a game the user owns that is missing from
// the catalog then has an explanation. Its zero value with Store set is ready to use.
type Skipped struct {
	// Store names the store in the warnings ("Epic").
	Store string

	// counts holds the number of items skipped for each reason.
	counts map[string]int

	// reasons are the reasons in the order they were first met.
	reasons []string
}

// Add counts one item skipped for reason, a plural noun phrase ("DLC and add-ons").
func (s *Skipped) Add(reason string) {
	if s.counts == nil {
		s.counts = map[string]int{}
	}

	if s.counts[reason] == 0 {
		s.reasons = append(s.reasons, reason)
	}

	s.counts[reason]++
}

// Warnings returns one warning per reason, with its count: "3 Epic items were not imported: DLC
// and add-ons". None when nothing was skipped.
func (s *Skipped) Warnings() []string {
	out := make([]string, 0, len(s.reasons))

	for _, r := range s.reasons {
		n := s.counts[r]
		if n == 1 {
			out = append(out, fmt.Sprintf("1 %s item was not imported: %s", s.Store, r))
			continue
		}

		out = append(out, fmt.Sprintf("%d %s items were not imported: %s", n, s.Store, r))
	}

	return out
}
