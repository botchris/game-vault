package media

import (
	"context"
	"slices"
	gosync "sync"
	"time"

	"gamevault/internal/domain/game"
)

// languages tracks the UI languages the catalog is browsed in, so the background scan fetches
// details (and genres) in the languages actually used.
type languages struct {
	mu   gosync.Mutex
	seen map[string]time.Time
}

func (l *languages) note(lang string, now time.Time) {
	if lang == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.seen == nil {
		l.seen = map[string]time.Time{}
	}

	l.seen[lang] = now
}

// active returns the languages used in the last month, most recent first ("en" if none yet).
func (l *languages) active(now time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	var out []string

	for lang, at := range l.seen {
		if now.Sub(at) < 30*24*time.Hour {
			out = append(out, lang)
		}
	}

	slices.SortFunc(out, func(a, b string) int { return l.seen[b].Compare(l.seen[a]) })

	if len(out) == 0 {
		out = []string{"en"}
	}

	return out
}

// CatalogSummaries returns the genres and release date of every game with cached details in the
// language, and remembers that language for the background scan.
func (s *Service) CatalogSummaries(ctx context.Context, language string) (map[game.ID]Summary, error) {
	if language == "" {
		language = "en"
	}

	s.langs.note(language, s.now())

	return s.details.Summaries(ctx, language)
}

// RunDetailsScanner downloads, in the background and slowly, the details of games linked to a store
// that have none yet, so the catalog can filter by genre and sort by year without opening each
// game. Store providers answer linked games without a quota; games without links would reach
// quota-limited providers, so they get their details when opened. Sheet images are not downloaded here (they come when a sheet is
// opened). Its requests are unattended, so no wait follows a fixed rhythm: games are pause to twice
// pause apart, rounds an hour ±10%, and a provider's complaint rests it a minute ±20%.
func (s *Service) RunDetailsScanner(ctx context.Context, pause time.Duration) {
	wait := func(d time.Duration) bool { return s.rhythm.Wait(ctx, d) }
	if !wait(s.rhythm.Between(scanSettle, 2*scanSettle)) { // let start-up settle
		return
	}

	for {
		s.autoLinkPass(ctx)

		fetched := 0

		for _, lang := range s.langs.active(s.now()) {
			n, ok := s.scanOnce(ctx, lang, pause, wait)
			fetched += n

			if !ok {
				return
			}
		}

		if fetched > 0 {
			s.log.Info("background details scan finished", "fetched", fetched)
		}

		if !wait(s.rhythm.Vary(scanRound, scanRoundJitter)) {
			return
		}
	}
}

// Timing of the background details scan (see RunDetailsScanner).
const (
	scanSettle        = 30 * time.Second // shortest wait after start-up
	scanRound         = time.Hour        // time between two rounds, before jitter
	scanRoundJitter   = 0.1
	scanBackOff       = time.Minute // rest after a provider complained, before jitter
	scanBackOffJitter = 0.2
)

// autoLinkPass queues for the automatic-links worker the games without a Steam link that are due
// (see AutoLink). It only queues, so the details scan that follows is never held back.
func (s *Service) autoLinkPass(ctx context.Context) {
	if !s.hasLinkFinders() {
		return
	}

	games, err := s.games.List(ctx)
	if err != nil {
		s.log.Warn("automatic links: listing games", "error", err)
		return
	}

	for _, g := range games {
		s.enqueueAutoLink(g)
	}
}

func (s *Service) scanOnce(ctx context.Context, lang string, pause time.Duration, wait func(time.Duration) bool) (int, bool) {
	games, err := s.games.List(ctx)
	if err != nil {
		s.log.Warn("details scan: listing games", "error", err)
		return 0, true
	}

	have, err := s.details.Summaries(ctx, lang)
	if err != nil {
		s.log.Warn("details scan: reading cache", "error", err)
		return 0, true
	}

	fetched := 0

	for _, g := range games {
		// Only linked games: the others would spend quota-limited providers' requests in the background.
		if len(g.Links()) == 0 {
			continue
		}

		if sum, ok := have[g.ID()]; ok && s.now().Sub(sum.FetchedAt) < detailsTTL {
			continue
		}

		_, warnings, err := s.fetchDetails(ctx, g.ID(), lang, false)
		if err == nil && len(warnings) == 0 {
			fetched++
		}
		// Back off when a provider complains (e.g. Steam rate limiting), otherwise go gently.
		delay := s.rhythm.Between(pause, 2*pause)
		if err != nil || len(warnings) > 0 {
			delay = s.rhythm.Vary(scanBackOff, scanBackOffJitter)
		}

		if !wait(delay) {
			return fetched, false
		}
	}

	return fetched, true
}
