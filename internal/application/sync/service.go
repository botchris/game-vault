// Package sync implements the source use cases: configuring accounts and scanning them to
// consolidate their copies into the catalog.
package sync

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	gosync "sync"
	"time"

	"gamevault/internal/application/catalog"
	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Provider is the port each source type implements (Humble, Steam...).
type Provider interface {
	// Descriptor describes the source type: its id, name and settings.
	Descriptor() source.TypeDescriptor

	// Fetch returns every copy the account currently holds. Warnings are non-fatal issues.
	Fetch(ctx context.Context, settings schema.Settings) (copies []game.ImportedCopy, warnings []string, err error)

	// Test checks the settings against the real service as cheaply as it can (e.g. Humble lists
	// the orders without downloading them). A nil error means they work; an error says why not.
	Test(ctx context.Context, settings schema.Settings) error
}

// Preparer is an optional port for providers that turn what the user typed into stored
// credentials before saving, e.g. Epic: a one-time authorization code becomes a session.
// It returns the settings to store.
type Preparer interface {
	// Prepare turns what the user typed into the settings to store.
	Prepare(ctx context.Context, settings schema.Settings) (schema.Settings, error)
}

// KeepAliver is an optional port for providers that reuse a website session copied from the
// browser (Battle.net, EA, Ubisoft). Such sessions expire when idle, so the service calls KeepAlive
// every KeepAliveInterval, like an open browser tab would, and persists what it renews.
type KeepAliver interface {
	// KeepAliveInterval is how often KeepAlive should run, before the service adds jitter.
	KeepAliveInterval() time.Duration

	// KeepAlive renews the session, writing anything renewed into settings for the service to persist.
	KeepAlive(ctx context.Context, settings schema.Settings) error
}

// StoreLinker is an optional port for providers that import a store's library and link each game
// to it (game.ImportedCopy.Links), so the game page can name and show that store.
type StoreLinker interface {
	// LinkStore describes the store the provider links games to.
	LinkStore() game.Store
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
	catalog   *catalog.Service
	covers    catalog.CoverCache
	now       port.Clock
	providers map[source.Type]Provider
	log       *slog.Logger
	running   gosync.Mutex // one scan at a time keeps consolidation consistent

	// warnedSystems holds "<source id>\x00<system>" for each invalid system a source sent that a scan
	// already warned about in this process: a source sends the same one on every scan, so repeats
	// are only logged at debug level. Guarded by running.
	warnedSystems map[string]bool

	// aliveMu guards aliveFailures.
	aliveMu gosync.Mutex

	// aliveFailures holds the sources whose last keep-alive failed, for the health check; a
	// successful one removes the entry. In memory: a restart retries within minutes.
	aliveFailures map[source.ID]keepAliveFailure

	// rhythm draws the random moments of keep-alives and scheduled scans; tests replace it.
	rhythm unattended.Rhythm
}

// keepAliveFailure is a keep-alive that failed.
type keepAliveFailure struct {
	message   string
	signedOut bool
	since     time.Time
}

// NewService builds the service. Each provider is registered under its descriptor's type; scans
// merge their copies into the catalog through cat. Options: WithClock, WithLogger, WithCoverCache.
func NewService(sources source.Repository, games game.Repository, tx port.TxManager, cat *catalog.Service,
	providers []Provider, opts ...Option) *Service {
	m := map[source.Type]Provider{}
	for _, p := range providers {
		m[p.Descriptor().Type] = p
	}

	s := &Service{
		sources:       sources,
		games:         games,
		tx:            tx,
		catalog:       cat,
		covers:        catalog.NoCoverCache{},
		now:           time.Now,
		providers:     m,
		log:           slog.New(slog.DiscardHandler),
		warnedSystems: map[string]bool{},
		aliveFailures: map[source.ID]keepAliveFailure{},
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Option configures NewService.
type Option func(*Service)

// WithClock sets the clock that dates scans and settings (time.Now by default).
func WithClock(now port.Clock) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

// WithCoverCache sets the cache whose covers deleting a source or removing one of its copies drops
// (none by default). Scans drop covers through the catalog, which has its own.
func WithCoverCache(covers catalog.CoverCache) Option { return func(s *Service) { s.covers = covers } }

// Types lists the available source types, sorted by name.
func (s *Service) Types() []source.TypeDescriptor {
	out := make([]source.TypeDescriptor, 0, len(s.providers))
	for _, p := range s.providers {
		out = append(out, p.Descriptor())
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// LinkStores returns the stores the source types link games to, sorted by name.
func (s *Service) LinkStores() []game.Store {
	var out []game.Store

	for _, p := range s.providers {
		if l, ok := p.(StoreLinker); ok {
			out = append(out, l.LinkStore())
		}
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

// Get returns one source with the number of copies it manages.
func (s *Service) Get(ctx context.Context, id source.ID) (SourceView, error) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return SourceView{}, err
	}

	return s.view(ctx, src)
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

	s.forgetKeepAlive(id) // new settings get their chance

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

// Delete removes a source. Its copies become manual copies.
func (s *Service) Delete(ctx context.Context, id source.ID) error {
	return s.delete(ctx, id, (*game.Game).ReleaseCopiesFromSource)
}

// DeleteWithCopies removes a source and its copies, along with any game left without copies.
func (s *Service) DeleteWithCopies(ctx context.Context, id source.ID) error {
	return s.delete(ctx, id, (*game.Game).RemoveCopiesFromSource)
}

// delete removes a source after detach took its copies out of every game; detach returns how many
// copies of the game it changed.
func (s *Service) delete(ctx context.Context, id source.ID, detach func(g *game.Game, sourceID string, now time.Time) int) error {
	defer s.forgetKeepAlive(id)

	var stale catalog.StaleCovers

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.sources.Get(ctx, id); err != nil {
			return err
		}

		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		now := s.now()

		for _, g := range games {
			before := g.CoverFingerprints()

			switch n := detach(g, string(id), now); {
			case n == 0:
			case len(g.Copies()) == 0:
				err = s.games.Delete(ctx, g.ID())
				stale.AddGame(g.ID())
			default:
				err = s.games.Save(ctx, g)
				stale.AddEditions(g.ID(), before, g.CoverFingerprints())
			}

			if err != nil {
				return err
			}
		}

		return s.sources.Delete(ctx, id)
	})
	if err == nil {
		stale.Drop(ctx, s.covers)
	}

	return err
}

// Test checks a source's settings without saving anything. If id is set, cfg settings are merged
// over the stored ones (so unchanged secrets can be sent as placeholders).
func (s *Service) Test(ctx context.Context, id source.ID, t source.Type, cfg source.Config) error {
	var (
		stored schema.Settings
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
	settings := schema.Settings{}
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
	settings := src.Settings() // the provider may write a rotated session into it

	removed, syncErr := s.scan(ctx, src, p, settings, &report)
	s.finishReport(&report, src.LastSync(), syncErr)

	src, saveErr := s.recordScan(ctx, src, settings, report)
	if saveErr != nil {
		// The report is lost, and with it any session the store rotated during the scan: the
		// next scan may find the source signed out.
		s.log.Error("saving the scan", "source", src.Name(), "error", saveErr)

		// The save failure decides the kind (usually none: Internal), so the scan error's kind (a
		// sign-out is a Precondition) cannot hide that the report was lost.
		return SourceView{}, errkind.Wrap(errkind.Of(saveErr), errors.Join(saveErr, syncErr))
	}

	s.log.Info("source synced", "source", src.Name(), "fetched", report.Fetched, "added", report.CopiesAdded,
		"updated", report.CopiesUpdated, "removed", removed, "games_created", report.GamesCreated, "error", report.Err)

	v, err := s.view(ctx, src)
	if err != nil {
		return v, err
	}

	return v, syncErr
}

// scan fetches the source's copies and consolidates them into the catalog, filling the report's
// counts and warnings. It returns how many copies the source withdrew.
func (s *Service) scan(ctx context.Context, src *source.Source, p Provider, settings schema.Settings,
	report *source.SyncReport) (int, error) {
	copies, warnings, err := p.Fetch(ctx, settings)
	report.Warnings = warnings

	for _, c := range copies {
		if !c.Withdrawn {
			report.Fetched++
		}
	}

	if err != nil {
		return 0, err
	}

	res, err := s.catalog.Consolidate(ctx, string(src.ID()), func(ctx context.Context) ([]game.ImportedCopy, error) {
		// The user may have removed items since the scan started: ask the source as it is now.
		fresh, err := s.sources.Get(ctx, src.ID())
		if err != nil {
			return nil, err
		}

		copies, report.Excluded = skipExcluded(fresh, copies)

		return copies, nil
	})
	if err != nil {
		return 0, err
	}

	res.AddTo(report)

	// The invalid systems count as warned only now that the scan is saved: a failed one warns again.
	warned, keys := s.firstInvalidSystems(src, res.InvalidSystems)
	report.Warnings = append(report.Warnings, warned...)

	for _, k := range keys {
		s.warnedSystems[k] = true
	}

	s.forgetKeepAlive(src.ID()) // the session works after all

	return res.CopiesRemoved, nil
}

// finishReport closes the report: its end, and for a failed scan the error and since when the
// scans fail, counted from the first failure of a streak (prev is the last scan's report).
func (s *Service) finishReport(report *source.SyncReport, prev *source.SyncReport, err error) {
	report.FinishedAt = s.now()

	if err == nil {
		return
	}

	report.Err = err.Error()
	report.SignedOut = errors.Is(err, source.ErrSignedOut)

	report.FailingSince = report.FinishedAt
	if prev != nil && !prev.Success() {
		report.FailingSince = cmp.Or(prev.FailingSince, prev.FinishedAt) // the same trouble going on
	}
}

// recordScan saves the report on the source as it is now, with the session the store rotated
// during the scan (settings, as the provider left them; src is the source as the scan read it).
// It returns the source as it saved it, or as it last read it when saving failed.
func (s *Service) recordScan(ctx context.Context, src *source.Source, settings schema.Settings,
	report source.SyncReport) (*source.Source, error) {
	saved := src

	// Saved on the source as it is now: the user may have removed items or changed its settings
	// while the store was answering. Only the scan's own results (its report and any session the
	// store rotated) are written over it.
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		latest, err := s.sources.Get(ctx, src.ID())
		if err != nil {
			return err
		}

		// Only a session the store rotated during this scan is written: one the user renewed
		// meanwhile (a new sign-in) must not be replaced by the scan's older copy.
		if d, err := s.Descriptor(latest.Type()); err == nil && !maps.Equal(d.Fields.State(settings), d.Fields.State(src.Settings())) {
			latest.UpdateState(d, settings, s.now())
		}

		latest.RecordSync(report)
		saved = latest

		return s.sources.Save(ctx, latest)
	})

	return saved, err
}

// firstInvalidSystems returns the warnings of the invalid systems src sent that no successful scan
// of it has warned about since the server started, with their keys in s.warnedSystems, and logs
// them; repeats are logged at debug level only. The caller records the keys once its scan is saved,
// so a failed scan warns again next time. It holds s.running.
func (s *Service) firstInvalidSystems(src *source.Source, invalid []game.InvalidSystem) ([]string, []string) {
	var warnings, keys []string

	for _, inv := range invalid {
		key := string(src.ID()) + "\x00" + inv.System
		if s.warnedSystems[key] {
			s.log.Debug("source sent an invalid system again", "source", src.Name(), "system", inv.System)
			continue
		}

		s.log.Warn("source sent an invalid system", "source", src.Name(), "warning", inv.Warning)

		warnings = append(warnings, inv.Warning)
		keys = append(keys, key)
	}

	return warnings, keys
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
func (s *Service) jittered(d time.Duration) time.Duration { return s.rhythm.Vary(d, keepAliveJitter) }

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
				due = now.Add(s.rhythm.Between(0, firstWithin))
				next[src.ID()] = due
			}

			if now.Before(due) {
				continue
			}

			next[src.ID()] = now.Add(s.jittered(k.KeepAliveInterval()))
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

	err = k.KeepAlive(ctx, settings)
	if err != nil {
		s.log.Warn("keep-alive failed", "source", src.Name(), "error", err)
	}

	s.noteKeepAlive(id, err)

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
					n = nextScan{
						after:    last,
						interval: interval,
					}

					n.at = last.Add(s.rhythm.Vary(interval, scanJitter))
					if last.IsZero() || n.at.Before(now) {
						// Never scanned, or overdue (the server was off): at a random moment soon,
						// so overdue sources are not all scanned in the same minute.
						n.at = now.Add(s.rhythm.Between(0, firstWithin))
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
