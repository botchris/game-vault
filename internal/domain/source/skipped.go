package source

import (
	"fmt"
	"log/slog"
)

// Skipped counts the items a scan leaves out on purpose, by reason. The expected skips (DLC,
// subscription games, what is not a game) happen on every scan, so they go to the log (Log), one
// line per reason with its count, where a missing game can be explained without a warning on every
// scan. A skip that loses the user a game they own (an item the source cannot give an id) goes to
// the scan's warnings instead (Warnings). Its zero value with Store set is ready to use.
type Skipped struct {
	// Store names the store in the log and the warnings ("Epic Games Store").
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

// Log writes one line per reason to log, with its count. A nil log writes nothing.
func (s *Skipped) Log(log *slog.Logger) {
	if log == nil {
		return
	}

	for _, r := range s.reasons {
		log.Info("scan skipped items", "store", s.Store, "reason", r, "count", s.counts[r])
	}
}

// Warnings returns one warning per reason, with its count: "3 EA items were not imported: …". None
// when nothing was skipped. Only for skips the user must hear about; expected ones go to Log.
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
