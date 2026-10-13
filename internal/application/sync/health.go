package sync

import (
	"cmp"
	"context"
	"errors"

	"gamevault/internal/application/health"
	"gamevault/internal/domain/source"
)

// noteKeepAlive records a keep-alive's result for the health check.
func (s *Service) noteKeepAlive(id source.ID, err error) {
	s.aliveMu.Lock()
	defer s.aliveMu.Unlock()

	if err == nil {
		delete(s.aliveFailures, id)
		return
	}

	f := keepAliveFailure{
		message:   err.Error(),
		signedOut: errors.Is(err, source.ErrSignedOut),
		since:     s.now(),
	}
	if prev, ok := s.aliveFailures[id]; ok {
		f.since = prev.since // the same trouble going on
	}

	s.aliveFailures[id] = f
}

// forgetKeepAlive drops a source's keep-alive failure: its settings changed, a scan worked, or it
// was deleted.
func (s *Service) forgetKeepAlive(id source.ID) {
	s.aliveMu.Lock()
	defer s.aliveMu.Unlock()

	delete(s.aliveFailures, id)
}

// signInAgain is what a signed-out issue says to do, before the store's own explanation.
const signInAgain = "the store no longer accepts the saved session: sign in again in Sources"

// Issues implements health.Check: enabled sources whose last scan failed (an error when the store
// signed them out, a warning otherwise) or whose last keep-alive failed. It reads the sources from
// the local database, never the stores.
func (s *Service) Issues(ctx context.Context) []health.Issue {
	sources, err := s.sources.List(ctx)
	if err != nil {
		s.log.Warn("health: reading the sources", "error", err)

		return []health.Issue{{
			Key:     "sources",
			Level:   health.LevelWarning,
			Kind:    health.KindSource,
			Reason:  health.ReasonSourceListFailed,
			Message: "the sources could not be read to check them: " + err.Error() + "; if it persists, report this message",
			Target:  health.TargetSources,
		}}
	}

	s.aliveMu.Lock()

	alive := make(map[source.ID]keepAliveFailure, len(s.aliveFailures))
	for id, f := range s.aliveFailures {
		alive[id] = f
	}
	s.aliveMu.Unlock()

	var out []health.Issue

	for _, src := range sources {
		if !src.Enabled() {
			continue
		}

		// A signed-out source says to sign in again, then the store's own explanation (raw).
		issue := func(key string, reason health.Reason, signedOut bool, warning, raw string) health.Issue {
			i := health.Issue{
				Key:      "source:" + string(src.ID()) + key,
				Level:    health.LevelWarning,
				Kind:     health.KindSource,
				Source:   src.Name(),
				Reason:   reason,
				Message:  warning,
				Target:   health.TargetSources,
				TargetID: string(src.ID()),
			}
			if signedOut {
				i.Level, i.Reason, i.Message = health.LevelError, health.ReasonSourceSignedOut, signInAgain+" ("+raw+")"
			}

			return i
		}

		// A report saved before SignedOut existed stays a warning until the next scan.
		if r := src.LastSync(); r != nil && !r.Success() {
			i := issue(":sync", health.ReasonSourceSyncFailed, r.SignedOut, "the last scan failed: "+r.Err, r.Err)
			i.Since = cmp.Or(r.FailingSince, r.FinishedAt)
			out = append(out, i)
		}

		if f, ok := alive[src.ID()]; ok {
			i := issue(":keepalive", health.ReasonSourceKeepAliveFailed, f.signedOut, "keeping the session alive failed: "+f.message, f.message)
			i.Since = f.since
			out = append(out, i)
		}
	}

	return out
}
