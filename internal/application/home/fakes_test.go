package home_test

import (
	"context"
	"errors"
	"time"

	"gamevault/internal/application/home"
	"gamevault/internal/application/ownership"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
)

var start = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// clock is a settable clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// fakeDeals is a deals provider with canned lists that records what it was asked.
type fakeDeals struct {
	featured []game.Deal
	lows     []game.Deal
	err      error
	calls    []home.DealQuery

	// asked, when set, runs once the provider has been asked, before it answers (to cancel the
	// caller's context mid-refresh).
	asked func()
}

func (f *fakeDeals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:           "deals-co",
		Kind:         provider.KindDeals,
		Name:         "Deals Co",
		DefaultOrder: 10,
	}
}

func (f *fakeDeals) Test(context.Context, schema.Settings) error { return nil }

func (f *fakeDeals) Deals(ctx context.Context, _ schema.Settings, q home.DealQuery) ([]game.Deal, error) {
	f.calls = append(f.calls, q)
	if f.asked != nil {
		f.asked()
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if f.err != nil {
		return nil, f.err
	}

	if q.List == home.DealsFeatured {
		return f.featured, nil
	}

	return f.lows, nil
}

// fakeReleases is a releases provider with canned lists that records what it was asked.
type fakeReleases struct {
	anticipated []game.Release
	series      []game.Release
	err         error
	windows     []time.Duration
	owned       [][]home.OwnedRef
}

func (f *fakeReleases) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:           "db-releases",
		Kind:         provider.KindReleases,
		Name:         "Game DB",
		DefaultOrder: 10,
	}
}

func (f *fakeReleases) Test(context.Context, schema.Settings) error { return nil }

func (f *fakeReleases) Anticipated(_ context.Context, _ schema.Settings, window time.Duration) ([]game.Release, error) {
	f.windows = append(f.windows, window)
	return f.anticipated, f.err
}

func (f *fakeReleases) Sequels(_ context.Context, _ schema.Settings, owned []home.OwnedRef) ([]game.Release, error) {
	f.owned = append(f.owned, owned)
	return f.series, f.err
}

func (f *fakeReleases) calls() int { return len(f.windows) + len(f.owned) }

// configs is the providers registry as the home service sees it: the enabled providers of each
// kind, and the answers reported.
type configs struct {
	enabled map[provider.Kind][]providers.View
	results []error
}

func (c *configs) Enabled(_ context.Context, kind provider.Kind) ([]providers.View, error) {
	return c.enabled[kind], nil
}

func (c *configs) ReportResult(_ provider.ID, err error) { c.results = append(c.results, err) }

// enable makes p an enabled provider of its kind.
func (c *configs) enable(p providers.Impl) {
	d := p.Descriptor()
	cfg := provider.New(d, 0, start)
	_ = cfg.Configure(d, true, schema.Settings{}, start)

	if c.enabled == nil {
		c.enabled = map[provider.Kind][]providers.View{}
	}

	c.enabled[d.Kind] = append(c.enabled[d.Kind], providers.View{
		Provider:   cfg,
		Descriptor: d,
	})
}

func (c *configs) disable(kind provider.Kind) { delete(c.enabled, kind) }

// snapshots is an in-memory home.Snapshots.
type snapshots map[home.Block]home.Snapshot

func (m snapshots) Snapshot(_ context.Context, b home.Block) (home.Snapshot, bool, error) {
	s, ok := m[b]
	if !ok {
		return home.Snapshot{Block: b}, false, nil
	}

	return s, true, nil
}

func (m snapshots) SaveSnapshot(_ context.Context, s home.Snapshot) error {
	m[s.Block] = s
	return nil
}

// catalog is an in-memory home.Catalog.
type catalog []*game.Game

func (c catalog) List(context.Context) ([]*game.Game, error) { return c, nil }

// owner is the ownership lookup on a catalog: the real one, which records the size of each check
// so the tests see the batches.
type owner struct {
	svc     *ownership.Service
	batches []int
}

func ownerOf(games catalog) *owner { return &owner{svc: ownership.NewService(games)} }

func (o *owner) Check(ctx context.Context, items []ownership.Item) ([]ownership.Result, error) {
	o.batches = append(o.batches, len(items))
	return o.svc.Check(ctx, items)
}

// brokenCatalog is a catalog whose storage fails.
type brokenCatalog struct{ err error }

func (c brokenCatalog) List(context.Context) ([]*game.Game, error) { return nil, c.err }

// prefs is the user's saved preferences.
type prefs settings.Preferences

func (p prefs) Preferences(context.Context) (settings.Preferences, error) {
	return settings.Preferences(p), nil
}

// unsaved is a home.Snapshots whose saves fail, as a full disk or a locked database does.
type unsaved struct{ snapshots }

func (u unsaved) SaveSnapshot(context.Context, home.Snapshot) error {
	return errors.New("database is locked")
}

// unreadable is a home.Snapshots whose next read fails once failNext is set, as a locked database
// does; the reads after it work again.
type unreadable struct {
	snapshots

	failNext *bool
}

func (u unreadable) Snapshot(ctx context.Context, b home.Block) (home.Snapshot, bool, error) {
	if *u.failNext {
		*u.failNext = false

		return home.Snapshot{}, false, errors.New("database is locked")
	}

	return u.snapshots.Snapshot(ctx, b)
}
