// Package sync implements the source use cases: configuring accounts and scanning them to
// consolidate their copies into the catalog.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"sort"
	gosync "sync"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Provider is the port each source type implements (Humble, Steam...).
type Provider interface {
	// Descriptor describes the source type: its id, name and settings.
	Descriptor() source.TypeDescriptor

	// Fetch returns every copy the account currently holds. Warnings are non-fatal issues.
	Fetch(ctx context.Context, settings source.Settings) (copies []game.ImportedCopy, warnings []string, err error)

	// Test checks the settings against the real service as cheaply as it can (e.g. Humble lists
	// the orders without downloading them). A nil error means they work; an error says why not.
	Test(ctx context.Context, settings source.Settings) error
}

// Preparer is an optional port for providers that turn what the user typed into stored
// credentials before saving, e.g. Epic: a one-time authorization code becomes a session.
// It returns the settings to store.
type Preparer interface {
	// Prepare turns what the user typed into the settings to store.
	Prepare(ctx context.Context, settings source.Settings) (source.Settings, error)
}

// KeepAliver is an optional port for providers that reuse a website session copied from the
// browser (Battle.net, EA, Ubisoft). Such sessions expire when idle, so the service calls KeepAlive
// every KeepAliveInterval, like an open browser tab would, and persists what it renews.
type KeepAliver interface {
	// KeepAliveInterval is how often KeepAlive should run, before the service adds jitter.
	KeepAliveInterval() time.Duration

	// KeepAlive renews the session, writing anything renewed into settings for the service to persist.
	KeepAlive(ctx context.Context, settings source.Settings) error
}

// Providers may also update their state fields (schema.FieldState) in the settings passed to
// Fetch or Test, e.g. a refresh token that rotates on every use; the service persists them.

// testTimeout bounds a connection test so the UI never waits forever.
const testTimeout = 30 * time.Second

// Service exposes the source use cases.
type Service struct {
	sources   source.Repository
	games     game.Repository
	tx        port.TxManager
	now       port.Clock
	providers map[source.Type]Provider
	log       *slog.Logger
	running   gosync.Mutex // one scan at a time keeps consolidation consistent
}

// NewService builds the service. Each provider is registered under its descriptor's type.
func NewService(sources source.Repository, games game.Repository, tx port.TxManager, now port.Clock, log *slog.Logger, providers ...Provider) *Service {
	m := map[source.Type]Provider{}
	for _, p := range providers {
		m[p.Descriptor().Type] = p
	}

	return &Service{sources: sources, games: games, tx: tx, now: now, providers: m, log: log}
}

// Types lists the available source types, sorted by name.
func (s *Service) Types() []source.TypeDescriptor {
	out := make([]source.TypeDescriptor, 0, len(s.providers))
	for _, p := range s.providers {
		out = append(out, p.Descriptor())
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// Descriptor returns the descriptor of a source type.
func (s *Service) Descriptor(t source.Type) (source.TypeDescriptor, error) {
	p, ok := s.providers[t]
	if !ok {
		return source.TypeDescriptor{}, fmt.Errorf("%w: %q", source.ErrUnknownType, t)
	}

	return p.Descriptor(), nil
}

// SourceView is a source plus the number of copies it manages.
type SourceView struct {
	*source.Source
	CopyCount int
}

// List returns every source with the number of copies it manages.
func (s *Service) List(ctx context.Context) ([]SourceView, error) {
	sources, err := s.sources.List(ctx)
	if err != nil {
		return nil, err
	}

	counts, err := s.copyCounts(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]SourceView, len(sources))
	for i, src := range sources {
		out[i] = SourceView{src, counts[string(src.ID())]}
	}

	return out, nil
}

func (s *Service) view(ctx context.Context, src *source.Source) (SourceView, error) {
	counts, err := s.copyCounts(ctx)
	return SourceView{src, counts[string(src.ID())]}, err
}

func (s *Service) copyCounts(ctx context.Context) (map[string]int, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}

	for _, g := range games {
		for _, c := range g.Copies() {
			if c.SourceID != "" {
				counts[c.SourceID]++
			}
		}
	}

	return counts, nil
}

// Create validates the configuration, prepares the provider and saves a new source of type t.
func (s *Service) Create(ctx context.Context, t source.Type, cfg source.Config) (SourceView, error) {
	d, err := s.Descriptor(t)
	if err != nil {
		return SourceView{}, err
	}

	src, err := source.New(d, cfg, s.now())
	if err != nil {
		return SourceView{}, err
	}

	if err := s.prepare(ctx, d, src); err != nil {
		return SourceView{}, err
	}

	if err := s.sources.Save(ctx, src); err != nil {
		return SourceView{}, err
	}

	return SourceView{Source: src}, nil
}

// Update applies a new configuration to an existing source.
func (s *Service) Update(ctx context.Context, id source.ID, cfg source.Config) (SourceView, error) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return SourceView{}, err
	}

	d, err := s.Descriptor(src.Type())
	if err != nil {
		return SourceView{}, err
	}

	if err := src.Reconfigure(d, cfg, s.now()); err != nil {
		return SourceView{}, err
	}

	if err := s.prepare(ctx, d, src); err != nil {
		return SourceView{}, err
	}

	if err := s.sources.Save(ctx, src); err != nil {
		return SourceView{}, err
	}

	return s.view(ctx, src)
}

// prepare lets the provider turn the typed settings into stored credentials (see Preparer).
func (s *Service) prepare(ctx context.Context, d source.TypeDescriptor, src *source.Source) error {
	p, ok := s.providers[src.Type()].(Preparer)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	settings, err := p.Prepare(ctx, src.Settings())
	if err != nil {
		return err
	}

	src.ReplaceSettings(d, settings, s.now())

	return nil
}

// Delete removes a source. Its copies become manual copies unless deleteCopies is set, in which
// case they are removed too, along with any game left without copies.
func (s *Service) Delete(ctx context.Context, id source.ID, deleteCopies bool) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.sources.Get(ctx, id); err != nil {
			return err
		}

		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		now := s.now()

		for _, g := range games {
			var n int
			if deleteCopies {
				n = g.RemoveCopiesFromSource(string(id), now)
			} else {
				n = g.ReleaseCopiesFromSource(string(id), now)
			}

			switch {
			case n == 0:
			case len(g.Copies()) == 0:
				err = s.games.Delete(ctx, g.ID())
			default:
				err = s.games.Save(ctx, g)
			}

			if err != nil {
				return err
			}
		}

		return s.sources.Delete(ctx, id)
	})
}

// Test checks a source's settings without saving anything. If id is set, cfg settings are merged
// over the stored ones (so unchanged secrets can be sent as placeholders).
func (s *Service) Test(ctx context.Context, id source.ID, t source.Type, cfg source.Config) error {
	var (
		stored source.Settings
		src    *source.Source
	)

	if id != "" {
		var err error
		if src, err = s.sources.Get(ctx, id); err != nil {
			return err
		}

		t, stored = src.Type(), src.Settings()
	}

	d, err := s.Descriptor(t)
	if err != nil {
		return err
	}
	// Stored values first, overridden by whatever the client sent (placeholders keep the stored secret).
	settings := source.Settings{}
	maps.Copy(settings, stored)
	maps.Copy(settings, d.MergeSettings(stored, cfg.Settings))

	if err := d.Validate(settings); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	if p, ok := s.providers[t].(Preparer); ok {
		if settings, err = p.Prepare(ctx, settings); err != nil {
			return err
		}
	}

	err = s.providers[t].Test(ctx, settings)
	// A saved source keeps any credentials the test rotated; nothing else of the test is stored.
	if src != nil && src.UpdateState(d, settings, s.now()) {
		err = errors.Join(err, s.sources.Save(ctx, src))
	}

	return err
}

// Sync scans one source and consolidates its copies into the catalog. A failed fetch is recorded
// in the source's report and also returned.
func (s *Service) Sync(ctx context.Context, id source.ID) (SourceView, error) {
	s.running.Lock()
	defer s.running.Unlock()

	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return SourceView{}, err
	}

	p, ok := s.providers[src.Type()]
	if !ok {
		return SourceView{}, fmt.Errorf("%w: %q", source.ErrUnknownType, src.Type())
	}

	report := source.SyncReport{StartedAt: s.now()}
	settings := src.Settings()

	copies, warnings, fetchErr := p.Fetch(ctx, settings)
	if d, err := s.Descriptor(src.Type()); err == nil {
		src.UpdateState(d, settings, s.now()) // saved below with the report
	}

	report.Warnings = warnings

	for _, c := range copies {
		if !c.Withdrawn {
			report.Fetched++
		}
	}

	var (
		syncErr error
		removed int
	)

	if fetchErr != nil {
		syncErr = fetchErr
	} else {
		syncErr = s.tx.WithinTx(ctx, func(ctx context.Context) error {
			games, err := s.games.List(ctx)
			if err != nil {
				return err
			}

			res := game.NewConsolidator(games).Apply(string(src.ID()), copies, s.now())
			for _, g := range res.Changed {
				if err := s.games.Save(ctx, g); err != nil {
					return err
				}
			}

			// Games left without copies after the source withdrew them (they were never games).
			for _, id := range res.Emptied {
				if err := s.games.Delete(ctx, id); err != nil {
					return err
				}
			}

			removed = res.CopiesRemoved

			report.CopiesAdded, report.CopiesUpdated = res.CopiesAdded, res.CopiesUpdated
			report.CopiesUnchanged, report.GamesCreated = res.CopiesUnchanged, res.GamesCreated
			report.Warnings = append(report.Warnings, res.Warnings...)

			return nil
		})
	}

	if syncErr != nil {
		report.Err = syncErr.Error()
	}

	report.FinishedAt = s.now()
	src.RecordSync(report)

	if err := s.sources.Save(ctx, src); err != nil {
		return SourceView{}, errors.Join(syncErr, err)
	}

	s.log.Info("source synced", "source", src.Name(), "fetched", report.Fetched, "added", report.CopiesAdded,
		"updated", report.CopiesUpdated, "removed", removed, "games_created", report.GamesCreated, "error", report.Err)

	v, err := s.view(ctx, src)
	if err != nil {
		return v, err
	}

	return v, syncErr
}

// SyncAll scans every enabled source. Failures are recorded on each source, not returned.
func (s *Service) SyncAll(ctx context.Context) ([]SourceView, error) {
	sources, err := s.sources.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, src := range sources {
		if src.Enabled() {
			s.Sync(ctx, src.ID()) //nolint:errcheck // the error is stored in the source's report
		}
	}

	return s.List(ctx)
}

// keepAliveJitter spreads keep-alives by ±30% of their interval, so they never follow a fixed
// rhythm a site could tell apart from a person.
const keepAliveJitter = 0.3

// jittered returns d varied by up to ±keepAliveJitter at random.
func jittered(d time.Duration) time.Duration { return vary(d, keepAliveJitter) }

// vary returns d varied at random by up to ±fraction of it.
func vary(d time.Duration, fraction float64) time.Duration {
	return time.Duration(float64(d) * (1 + fraction*(2*rand.Float64()-1)))
}

// RunKeepAlive keeps browser sessions alive (see KeepAliver), checking every tick until ctx ends.
// Each source's first keep-alive comes at a random moment within firstWithin (not all at start-up),
// and each next one after its interval varied at random. Failures are logged: the next scan
// reports them on the source.
func (s *Service) RunKeepAlive(ctx context.Context, tick, firstWithin time.Duration) {
	next := map[source.ID]time.Time{}

	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		sources, err := s.sources.List(ctx)
		if err != nil {
			s.log.Error("keep-alive: listing sources", "error", err)
		}

		now := s.now()
		for _, src := range sources {
			k, ok := s.providers[src.Type()].(KeepAliver)
			if !ok || !src.Enabled() {
				continue
			}

			due, seen := next[src.ID()]
			if !seen {
				due = now.Add(time.Duration(rand.Int64N(int64(firstWithin) + 1)))
				next[src.ID()] = due
			}

			if now.Before(due) {
				continue
			}

			next[src.ID()] = now.Add(jittered(k.KeepAliveInterval()))
			s.keepAlive(ctx, src.ID(), k)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) keepAlive(ctx context.Context, id source.ID, k KeepAliver) {
	s.running.Lock() // not during a scan: both may rotate the same credentials
	defer s.running.Unlock()

	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return
	}

	d, err := s.Descriptor(src.Type())
	if err != nil {
		return
	}

	settings := src.Settings()

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	if err := k.KeepAlive(ctx, settings); err != nil {
		s.log.Warn("keep-alive failed", "source", src.Name(), "error", err)
	}

	if src.UpdateState(d, settings, s.now()) {
		if err := s.sources.Save(ctx, src); err != nil {
			s.log.Warn("keep-alive: saving renewed session", "source", src.Name(), "error", err)
		}
	}
}

// scanJitter spreads scheduled scans by ±10% of their interval (±2.4 h for a daily scan), so they
// do not happen at the same time every day.
const scanJitter = 0.1

// nextScan is when a source is due, computed once per (last scan, interval).
type nextScan struct {
	after    time.Time
	interval time.Duration
	at       time.Time
}

// RunScheduler scans sources whose sync interval has elapsed, checking every tick, until ctx ends.
// Each interval is varied at random (see scanJitter); a source never scanned, or overdue after the
// server was off, is scanned at a random moment within firstWithin.
func (s *Service) RunScheduler(ctx context.Context, tick, firstWithin time.Duration) {
	due := map[source.ID]nextScan{}

	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sources, err := s.sources.List(ctx)
			if err != nil {
				s.log.Error("scheduler: listing sources", "error", err)
				continue
			}

			now := s.now()

			for _, src := range sources {
				interval := src.SyncInterval()
				if !src.Enabled() || interval <= 0 {
					continue
				}

				var last time.Time
				if r := src.LastSync(); r != nil {
					last = r.StartedAt
				}

				n, ok := due[src.ID()]
				if !ok || !n.after.Equal(last) || n.interval != interval {
					n = nextScan{after: last, interval: interval}

					n.at = last.Add(vary(interval, scanJitter))
					if last.IsZero() || n.at.Before(now) {
						// Never scanned, or overdue (the server was off): at a random moment soon,
						// so overdue sources are not all scanned in the same minute.
						n.at = now.Add(time.Duration(rand.Int64N(int64(firstWithin) + 1)))
					}

					due[src.ID()] = n
				}

				if !now.Before(n.at) {
					s.Sync(ctx, src.ID()) //nolint:errcheck // recorded in the report
				}
			}
		}
	}
}
