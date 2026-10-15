package providers

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// testTimeout bounds a provider's connection test, which the user waits for on the Providers page.
const testTimeout = 20 * time.Second

// Registry keeps the providers compiled in and the user's configuration of each.
type Registry struct {
	repo  provider.Repository
	impls map[provider.ID]Impl
	order []provider.ID // default chain order: DefaultOrder, then registration order
	now   port.Clock
	log   *slog.Logger

	// mu guards read, registered and configs. Writes hold it while they save, so two changes never
	// interleave.
	mu sync.Mutex

	// read reports that configs holds what storage had.
	read bool

	// registered holds the kinds whose new providers have their default configuration (see load).
	registered map[provider.Kind]bool

	// configs holds every stored configuration by kind, in priority order (then by id, as storage
	// lists them), including those of providers no longer compiled in, which keep their place.
	configs map[provider.Kind][]*provider.Provider

	// status keeps the providers' current provider-level problems (see Statuses).
	status statuses

	// coversChanged is called when the cover chain may find covers it did not before (see
	// OnCoversChanged).
	coversChanged func(ctx context.Context) error
}

// NewRegistry builds the registry of the providers compiled in. Their DefaultOrder (then their
// order here) is the chain order the first time each provider is seen; afterwards the user's order
// is used.
func NewRegistry(repo provider.Repository, impls []Impl, opts ...Option) *Registry {
	r := &Registry{
		repo:          repo,
		impls:         make(map[provider.ID]Impl, len(impls)),
		now:           time.Now,
		log:           slog.New(slog.DiscardHandler),
		registered:    map[provider.Kind]bool{},
		configs:       map[provider.Kind][]*provider.Provider{},
		coversChanged: func(context.Context) error { return nil },
	}
	for _, o := range opts {
		o(r)
	}

	for _, p := range impls {
		id := p.Descriptor().ID
		r.impls[id] = p
		r.order = append(r.order, id)
	}

	slices.SortStableFunc(r.order, func(a, b provider.ID) int {
		return cmp.Compare(r.impls[a].Descriptor().DefaultOrder, r.impls[b].Descriptor().DefaultOrder)
	})

	return r
}

// OnCoversChanged sets what to do when the cover chain may now find covers it did not before (a
// cover provider, or a sibling whose settings it shares, was configured; the cover chain was
// reordered): the covers service forgets its "no cover found" results.
func (r *Registry) OnCoversChanged(fn func(ctx context.Context) error) { r.coversChanged = fn }

// All returns every provider compiled in, in default chain order.
func (r *Registry) All() []Impl {
	out := make([]Impl, len(r.order))
	for i, id := range r.order {
		out[i] = r.impls[id]
	}

	return out
}

// ProviderName returns a registered provider's display name, or its id when it is not registered
// (details cached by a provider that was removed since).
func (r *Registry) ProviderName(id provider.ID) string {
	if p, ok := r.impls[id]; ok {
		return p.Descriptor().Name
	}

	return string(id)
}

// load reads the configurations from storage the first time the registry is used, and gives the
// providers of kind seen for the first time their default configuration, the first time that kind
// is used. Kinds are registered on first use rather than all at once, so a new provider inherits the
// state its siblings have then (see registerNew). Afterwards the configurations are only read from
// memory. r.mu is held.
func (r *Registry) load(ctx context.Context, kind provider.Kind) error {
	if !r.read {
		configs := map[provider.Kind][]*provider.Provider{}

		for _, k := range provider.Kinds() {
			list, err := r.repo.List(ctx, k)
			if err != nil {
				return err
			}

			configs[k] = list
		}

		r.configs, r.read = configs, true
	}

	if r.registered[kind] {
		return nil
	}

	if err := r.registerNew(ctx, kind); err != nil {
		return err
	}

	r.registered[kind] = true

	return nil
}

// registerNew saves the default configuration of each provider of kind seen for the first time, at
// the end of the chain, in default order.
func (r *Registry) registerNew(ctx context.Context, kind provider.Kind) error {
	known := map[provider.ID]bool{}
	for _, c := range r.configs[kind] {
		known[c.ID()] = true
	}

	for _, id := range r.order {
		d := r.impls[id].Descriptor()
		if d.Kind != kind || known[id] {
			continue
		}

		p := provider.New(d, len(r.configs[kind]), r.now())
		// A new capability of an already configured service inherits its credentials, and is
		// enabled if the service is (e.g. TheGamesDB details once TheGamesDB covers have a key).
		if sib := r.siblings(d.SettingsGroup, id); len(sib) > 0 {
			p.ShareSettings(sib[0].Settings(), r.now())

			// Providers asked on a schedule (prices, deals, releases) send requests nobody asked
			// for at that moment, so the user turns them on explicitly.
			if sib[0].Enabled() && !d.Kind.Scheduled() {
				_ = p.Configure(d, true, sib[0].Settings(), r.now()) // stays disabled if settings are incomplete
			}
		}

		if err := r.repo.Save(ctx, p); err != nil {
			return err
		}

		r.configs[kind] = append(r.configs[kind], p)
		sortConfigs(r.configs[kind])
	}

	return nil
}

// siblings returns the stored configurations sharing a settings group, other than except, kind by
// kind in priority order. r.mu is held.
func (r *Registry) siblings(group string, except provider.ID) []*provider.Provider {
	if group == "" {
		return nil
	}

	var out []*provider.Provider

	for _, k := range provider.Kinds() {
		for _, c := range r.configs[k] {
			if impl, ok := r.impls[c.ID()]; ok && c.ID() != except && impl.Descriptor().SettingsGroup == group {
				out = append(out, c)
			}
		}
	}

	return out
}

// Providers returns the configured providers of a kind in priority order.
func (r *Registry) Providers(ctx context.Context, kind provider.Kind) ([]View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.load(ctx, kind); err != nil {
		return nil, err
	}

	return r.views(kind), nil
}

// views returns copies of the configurations of kind whose provider is compiled in. r.mu is held.
func (r *Registry) views(kind provider.Kind) []View {
	var out []View

	for _, c := range r.configs[kind] {
		if impl, ok := r.impls[c.ID()]; ok { // configurations of removed implementations are ignored
			out = append(out, View{clone(c), impl.Descriptor()})
		}
	}

	return out
}

// Enabled returns the enabled providers of a kind in priority order.
func (r *Registry) Enabled(ctx context.Context, kind provider.Kind) ([]View, error) {
	views, err := r.Providers(ctx, kind)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(views, func(v View) bool { return !v.Enabled() }), nil
}

// Find returns the configuration of the provider id.
func (r *Registry) Find(ctx context.Context, id provider.ID) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.find(ctx, id)
}

// find is Find with r.mu held.
func (r *Registry) find(ctx context.Context, id provider.ID) (View, error) {
	impl, ok := r.impls[id]
	if !ok {
		return View{}, fmt.Errorf("%w: %q", ErrUnknownProvider, id)
	}

	kind := impl.Descriptor().Kind
	if err := r.load(ctx, kind); err != nil {
		return View{}, err
	}

	for _, v := range r.views(kind) {
		if v.ID() == id {
			return v, nil
		}
	}

	return View{}, provider.ErrNotFound
}

// ConfigureProvider enables/disables a provider and updates its settings, and those of the
// providers sharing them (same SettingsGroup). Changing a cover provider, or a provider whose
// settings a cover provider shares, calls OnCoversChanged's function, so games without a cover are
// looked up again.
func (r *Registry) ConfigureProvider(ctx context.Context, id provider.ID, enabled bool, settings schema.Settings) (View, error) {
	v, coverChanged, err := r.configure(ctx, id, enabled, settings)
	if err != nil {
		return v, err
	}

	if coverChanged {
		r.notifyCovers(ctx)
	}

	return v, nil
}

// configure is ConfigureProvider without the notification, which runs once r.mu is released.
func (r *Registry) configure(ctx context.Context, id provider.ID, enabled bool, settings schema.Settings) (View, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	v, err := r.find(ctx, id)
	if err != nil {
		return v, false, err
	}

	if err := v.Configure(v.Descriptor, enabled, settings, r.now()); err != nil {
		return v, false, err
	}

	if err := r.save(ctx, v.Provider); err != nil {
		return v, false, err
	}

	// New settings (a new key) may have solved the provider's problem: the next answer tells.
	r.status.forget(statusKey(v.Descriptor))

	coverChanged := v.Kind() == provider.KindCover

	for _, sib := range r.siblings(v.Descriptor.SettingsGroup, id) { // keep shared credentials in sync
		c := clone(sib)
		c.ShareSettings(v.Settings(), r.now())

		if err := r.save(ctx, c); err != nil {
			return v, false, err
		}

		// Credentials entered from the details tab also reach a cover provider of the same service.
		coverChanged = coverChanged || c.Kind() == provider.KindCover
	}

	return v, coverChanged, nil
}

// ReorderProviders sets the chain order of a kind of provider (first id = tried first).
func (r *Registry) ReorderProviders(ctx context.Context, kind provider.Kind, ids []provider.ID) ([]View, error) {
	if err := r.reorder(ctx, kind, ids); err != nil {
		return nil, err
	}

	if kind == provider.KindCover {
		r.notifyCovers(ctx)
	}

	return r.Providers(ctx, kind)
}

// reorder is ReorderProviders without the notification, which runs once r.mu is released.
func (r *Registry) reorder(ctx context.Context, kind provider.Kind, ids []provider.ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.load(ctx, kind); err != nil {
		return err
	}

	views := r.views(kind)

	configs := make([]*provider.Provider, len(views))
	for i, v := range views {
		configs[i] = v.Provider
	}

	provider.Reorder(configs, ids, r.now())

	for _, c := range configs {
		if err := r.save(ctx, c); err != nil {
			return err
		}
	}

	return nil
}

// SaveState keeps the state fields a provider wrote into settings during a lookup (see
// provider.Provider.UpdateState), when they changed.
func (r *Registry) SaveState(ctx context.Context, id provider.ID, settings schema.Settings) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v, err := r.find(ctx, id)
	if err != nil {
		return err
	}

	if !v.UpdateState(v.Descriptor, settings, r.now()) {
		return nil
	}

	return r.save(ctx, v.Provider)
}

// TestProvider checks a provider's settings (stored ones, overridden by those given).
func (r *Registry) TestProvider(ctx context.Context, id provider.ID, settings schema.Settings) error {
	v, err := r.Find(ctx, id)
	if err != nil {
		return err
	}

	merged := v.Settings()
	maps.Copy(merged, v.Descriptor.Fields.Merge(v.Settings(), settings))

	if err := v.Descriptor.Fields.Validate(merged, v.Descriptor.Name); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	// The check is an answer like any other: a success clears the service's status, a
	// provider-level problem sets it.
	err = r.impls[id].Test(ctx, merged)
	r.ReportResult(id, err)

	return err
}

// save stores c and puts it in place of the configuration it replaces. r.mu is held.
func (r *Registry) save(ctx context.Context, c *provider.Provider) error {
	if err := r.repo.Save(ctx, c); err != nil {
		return err
	}

	list := r.configs[c.Kind()]
	if i := slices.IndexFunc(list, func(p *provider.Provider) bool { return p.ID() == c.ID() }); i >= 0 {
		list[i] = clone(c)
	} else {
		list = append(list, clone(c))
	}

	sortConfigs(list)
	r.configs[c.Kind()] = list

	return nil
}

// notifyCovers tells the covers service the chain changed. Its failure does not undo the change.
func (r *Registry) notifyCovers(ctx context.Context) {
	if err := r.coversChanged(ctx); err != nil {
		r.log.Warn("forgetting missing covers", "error", err)
	}
}

// sortConfigs orders configurations as storage lists them: by priority, then by id.
func sortConfigs(list []*provider.Provider) {
	slices.SortStableFunc(list, func(a, b *provider.Provider) int {
		return cmp.Or(cmp.Compare(a.Priority(), b.Priority()), cmp.Compare(a.ID(), b.ID()))
	})
}

// clone copies a configuration, so a View never shares the one kept in memory.
func clone(p *provider.Provider) *provider.Provider {
	return provider.Rehydrate(p.ID(), p.Kind(), p.Enabled(), p.Priority(), p.Settings(), p.UpdatedAt())
}
