package notify_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// fakeConnections keeps connections in memory.
type fakeConnections struct {
	mu    sync.Mutex
	items map[connection.ID]*connection.Connection
}

func newFakeConnections() *fakeConnections {
	return &fakeConnections{items: map[connection.ID]*connection.Connection{}}
}

func (f *fakeConnections) List(context.Context) ([]*connection.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]*connection.Connection, 0, len(f.items))
	for _, c := range f.items {
		out = append(out, connection.Rehydrate(c.Snapshot()))
	}

	slices.SortFunc(out, func(a, b *connection.Connection) int { return compare(a.Name(), b.Name()) })

	return out, nil
}

func (f *fakeConnections) Get(_ context.Context, id connection.ID) (*connection.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.items[id]
	if !ok {
		return nil, connection.ErrNotFound
	}

	return connection.Rehydrate(c.Snapshot()), nil
}

func (f *fakeConnections) Save(_ context.Context, c *connection.Connection) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.items[c.ID()] = connection.Rehydrate(c.Snapshot())

	return nil
}

func (f *fakeConnections) Delete(_ context.Context, id connection.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.items[id]; !ok {
		return connection.ErrNotFound
	}

	delete(f.items, id)

	return nil
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// fakeOutbox keeps deliveries in memory, in the order they were queued; enqueueErr makes queueing
// fail.
type fakeOutbox struct {
	mu         sync.Mutex
	items      []notify.Delivery
	enqueueErr error
}

func (f *fakeOutbox) Enqueue(_ context.Context, ds []notify.Delivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.enqueueErr != nil {
		return f.enqueueErr
	}

	f.items = append(f.items, ds...)

	return nil
}

func (f *fakeOutbox) Due(_ context.Context, now time.Time) ([]notify.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []notify.Delivery

	for _, d := range f.items {
		if d.State == notify.StateQueued && !d.NextAt.After(now) {
			out = append(out, d)
		}
	}

	return out, nil
}

func (f *fakeOutbox) update(id string, fn func(*notify.Delivery)) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.items {
		if f.items[i].ID == id {
			fn(&f.items[i])

			return nil
		}
	}

	return nil
}

func (f *fakeOutbox) MarkSent(_ context.Context, id string, at time.Time) error {
	return f.update(id, func(d *notify.Delivery) {
		d.State = notify.StateSent
		d.UpdatedAt = at
	})
}

func (f *fakeOutbox) MarkRetry(_ context.Context, id string, attempts int, next time.Time, reason string, at time.Time) error {
	return f.update(id, func(d *notify.Delivery) {
		d.Attempts = attempts
		d.NextAt = next
		d.LastError = reason
		d.UpdatedAt = at
	})
}

func (f *fakeOutbox) MarkFailed(_ context.Context, id string, reason string, at time.Time) error {
	return f.update(id, func(d *notify.Delivery) {
		d.State = notify.StateFailed
		d.LastError = reason
		d.UpdatedAt = at
	})
}

func (f *fakeOutbox) MarkDropped(_ context.Context, id string, at time.Time) error {
	return f.update(id, func(d *notify.Delivery) {
		d.State = notify.StateDropped
		d.UpdatedAt = at
	})
}

func (f *fakeOutbox) Recent(_ context.Context, id connection.ID, limit int) ([]notify.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []notify.Delivery

	for i := len(f.items) - 1; i >= 0 && len(out) < limit; i-- {
		if f.items[i].Connection == id {
			out = append(out, f.items[i])
		}
	}

	return out, nil
}

func (f *fakeOutbox) DeleteFor(_ context.Context, id connection.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.items = slices.DeleteFunc(f.items, func(d notify.Delivery) bool { return d.Connection == id })

	return nil
}

func (f *fakeOutbox) Prune(_ context.Context, before time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.items = slices.DeleteFunc(f.items, func(d notify.Delivery) bool {
		return d.State != notify.StateQueued && d.UpdatedAt.Before(before)
	})

	return nil
}

func (f *fakeOutbox) all() []notify.Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.items)
}

// fakeSettings keeps the address in memory.
type fakeSettings struct {
	address string
}

func (f *fakeSettings) Address(context.Context) (string, error) { return f.address, nil }

func (f *fakeSettings) SaveAddress(_ context.Context, address string) error {
	f.address = address

	return nil
}

// fakeCatalog holds games built by the tests.
type fakeCatalog struct {
	games map[game.ID]*game.Game
}

func (f *fakeCatalog) List(context.Context) ([]*game.Game, error) {
	out := make([]*game.Game, 0, len(f.games))
	for _, g := range f.games {
		out = append(out, g)
	}

	return out, nil
}

func (f *fakeCatalog) Get(_ context.Context, id game.ID) (*game.Game, error) {
	g, ok := f.games[id]
	if !ok {
		return nil, game.ErrGameNotFound
	}

	return g, nil
}

// fakeCovers holds the covers on disk, by game; it records the systems asked for.
type fakeCovers struct {
	images map[game.ID]media.Image
	asked  map[game.ID]string
}

func (f *fakeCovers) GetCover(id game.ID, system string) (media.Image, bool, error) {
	if f.asked == nil {
		f.asked = map[game.ID]string{}
	}

	f.asked[id] = system

	img, ok := f.images[id]

	return img, ok, nil
}

// fakeConnector records what it sends; found is the chat FindChat returns.
type fakeConnector struct {
	typ    connection.Type
	sent   []notify.Message
	tested []schema.Settings
	err    error
	found  string
	finds  int

	// invalid is what Validate answers.
	invalid error

	// onTest runs inside Test, before it answers (a change made while the sample is on its way).
	onTest func()
}

func (f *fakeConnector) Descriptor() connection.TypeDescriptor {
	return connection.TypeDescriptor{
		Type: f.typ,
		Name: "Fake",
		Fields: schema.Fields{
			{
				Key:      "token",
				LabelKey: "fake.token",
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:      "chat",
				LabelKey: "fake.chat",
				Kind:     schema.FieldText,
				Required: true,
			},
		},
	}
}

func (f *fakeConnector) Send(_ context.Context, _ schema.Settings, m notify.Message) error {
	f.sent = append(f.sent, m)

	return f.err
}

func (f *fakeConnector) Test(_ context.Context, settings schema.Settings, m notify.Message) error {
	f.sent = append(f.sent, m)
	f.tested = append(f.tested, settings)

	if f.onTest != nil {
		f.onTest()
	}

	return f.err
}

func (f *fakeConnector) Validate(schema.Settings) error { return f.invalid }

func (f *fakeConnector) FindChat(context.Context, schema.Settings) (key, value string, err error) {
	f.finds++

	return "chat", f.found, nil
}

// clock is a settable clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// world is a service with in-memory ports.
type world struct {
	svc       *notify.Service
	conns     *fakeConnections
	outbox    *fakeOutbox
	settings  *fakeSettings
	catalog   *fakeCatalog
	covers    *fakeCovers
	connector *fakeConnector
	clock     *clock
}

func newWorld(t *testing.T, opts ...notify.Option) *world {
	t.Helper()

	w := &world{
		conns:     newFakeConnections(),
		outbox:    &fakeOutbox{},
		settings:  &fakeSettings{},
		catalog:   &fakeCatalog{games: map[game.ID]*game.Game{}},
		covers:    &fakeCovers{images: map[game.ID]media.Image{}},
		connector: &fakeConnector{typ: "fake"},
		clock:     &clock{now: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)},
	}

	all := append([]notify.Option{
		notify.WithConnectors(w.connector),
		notify.WithClock(w.clock.Now),
	}, opts...)
	w.svc = notify.NewService(w.conns, w.outbox, w.settings, w.catalog, w.covers, all...)

	return w
}

// connect saves a connection of the fake type subscribed to kinds.
func (w *world) connect(t *testing.T, ctx context.Context, name string, enabled bool, kinds ...event.Kind) *connection.Connection {
	t.Helper()

	c, err := w.svc.Save(ctx, "", "fake", connection.Config{
		Name:     name,
		Enabled:  enabled,
		Events:   kinds,
		Language: "en",
		Settings: schema.Settings{
			"token": "t-123",
			"chat":  "42",
		},
	})
	require.NoError(t, err)

	return c
}

// addGame adds a game with one copy on platform to the catalog.
func (w *world) addGame(t *testing.T, title, platform string) *game.Game {
	t.Helper()

	g, err := game.New(title, w.clock.Now())
	require.NoError(t, err)

	_, err = g.AddCopy(game.CopyDetails{
		Kind:     game.KindLibrary,
		Platform: platform,
	}, w.clock.Now())
	require.NoError(t, err)

	w.catalog.games[g.ID()] = g

	return g
}
