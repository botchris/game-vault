package home

import (
	"context"
	"errors"
	"time"
)

const (
	// startMin and startMax bound when an overdue block is gathered after start-up: a random
	// moment, so a restart never asks the services at once or at a fixed second.
	startMin = time.Minute
	startMax = 5 * time.Minute

	// jitter varies every interval by up to ±10 %.
	jitter = 0.1

	// idleCheck is how often a block without an enabled provider is looked at again; nothing is
	// sent meanwhile.
	idleCheck = time.Hour
)

// Run gathers the blocks on their own schedule until ctx ends: deals every 6 hours and releases
// every 24, each varied ±10 %, an overdue block 1–5 minutes after start-up. It is the only caller of
// the providers besides the user's Refresh, and cmd/gamevault does not start it with
// -no-unattended.
func (s *Service) Run(ctx context.Context) {
	next := map[Block]time.Time{}
	for _, b := range Blocks() {
		next[b] = s.firstTurn(ctx, b)
	}

	for {
		b := Blocks()[0]
		for _, c := range Blocks() {
			if next[c].Before(next[b]) {
				b = c
			}
		}

		if !s.rhythm.Wait(ctx, max(next[b].Sub(s.now()), 0)) {
			return
		}

		next[b] = s.now().Add(s.turn(ctx, b))
	}
}

// firstTurn returns when a block is first gathered: when its interval ends, varied, and never
// sooner than 1–5 minutes after start-up.
func (s *Service) firstTurn(ctx context.Context, b Block) time.Time {
	now := s.now()
	soon := now.Add(s.rhythm.Between(startMin, startMax))

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		s.log.Warn("reading the home page's snapshot", "block", b, "error", err)
		return soon
	}

	last := s.lastTry(b, snap, now)
	if last.IsZero() {
		return soon
	}

	at := last.Add(s.rhythm.Vary(every(b), jitter))
	if at.Before(now.Add(startMin)) {
		return soon
	}

	return at
}

// turn gathers a block, unless the user refreshed it recently, and returns how long to wait for
// its next turn.
func (s *Service) turn(ctx context.Context, b Block) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	// A refresh the user asked for in the meantime counts as this turn: the next one is a whole
	// interval after it. The job's own last attempt is about a whole interval old by now, so one
	// less than half an interval old can only be the user's (or an interrupted one, which reached
	// the service all the same).
	if snap, _, err := s.snaps.Snapshot(ctx, b); err == nil {
		now := s.now()
		if last := s.lastTry(b, snap, now); !last.IsZero() && now.Sub(last) < every(b)/2 {
			return last.Add(s.rhythm.Vary(every(b), jitter)).Sub(now)
		}
	}

	err := s.refresh(ctx, b)

	switch {
	case errors.Is(err, ErrNotConfigured):
		return s.rhythm.Vary(idleCheck, jitter)
	case err != nil && ctx.Err() == nil:
		s.log.Warn("refreshing the home page", "block", b, "error", err)
	}

	return s.rhythm.Vary(every(b), jitter)
}
