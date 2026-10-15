package notify

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"strings"
	"sync"
	"time"

	"gamevault/internal/application/events"
	"gamevault/internal/application/media"
	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// ErrBadAddress means the Game Vault address is not a web address messages can link to.
var ErrBadAddress = errkind.New(errkind.Invalid, "the address must start with http:// or https://")

// defaultBatchWindow is how long games added by hand wait for another (see FlushBatches).
const defaultBatchWindow = 10 * time.Minute

// Service keeps the connections, queues events for them and renders their messages.
type Service struct {
	conns      Connections
	outbox     Outbox
	settings   SettingsStore
	catalog    Catalog
	covers     CoverFiles
	connectors map[connection.Type]Connector
	order      []connection.Type
	now        port.Clock
	log        *slog.Logger
	sending    bool
	window     time.Duration

	// wakeup tells the dispatcher that deliveries were queued; it holds one signal at most.
	wakeup chan struct{}

	// rhythm waits between two messages of a connection and between two looks at the queue.
	rhythm unattended.Rhythm

	// lastSend is when each connection last sent, so its pace holds across rounds. Only the
	// dispatcher's goroutine touches it.
	lastSend map[connection.ID]time.Time

	// mu guards the batch of games added by hand and flushFailing.
	mu      sync.Mutex
	pending *event.CollectionAdded
	lastAt  time.Time

	// flushFailing is true while queueing the batch keeps failing, so the failure is logged once.
	flushFailing bool
}

var _ events.Publisher = (*Service)(nil)

// NewService returns the notification service. Sending is off until WithSending(true).
func NewService(conns Connections, outbox Outbox, settings SettingsStore, catalog Catalog, covers CoverFiles, opts ...Option) *Service {
	s := &Service{
		conns:      conns,
		outbox:     outbox,
		settings:   settings,
		catalog:    catalog,
		covers:     covers,
		connectors: map[connection.Type]Connector{},
		now:        time.Now,
		log:        slog.New(slog.DiscardHandler),
		window:     defaultBatchWindow,
		wakeup:     make(chan struct{}, 1),
		lastSend:   map[connection.ID]time.Time{},
	}

	for _, o := range opts {
		o(s)
	}

	return s
}

// Types returns the connector types compiled in.
func (s *Service) Types() []connection.TypeDescriptor {
	out := make([]connection.TypeDescriptor, 0, len(s.order))
	for _, typ := range s.order {
		out = append(out, s.connectors[typ].Descriptor())
	}

	return out
}

// connector returns the connector of type typ, or connection.ErrUnknownType.
func (s *Service) connector(typ connection.Type) (Connector, error) {
	c, ok := s.connectors[typ]
	if !ok {
		return nil, fmt.Errorf("%w: %q", connection.ErrUnknownType, typ)
	}

	return c, nil
}

// List returns every connection.
func (s *Service) List(ctx context.Context) ([]*connection.Connection, error) {
	return s.conns.List(ctx)
}

// Get returns one connection, or connection.ErrNotFound.
func (s *Service) Get(ctx context.Context, id connection.ID) (*connection.Connection, error) {
	return s.conns.Get(ctx, id)
}

// Save creates a connection of type typ when id is empty, or reconfigures the connection id (its
// type cannot change, so typ is ignored). Secrets sent back masked keep their stored value.
func (s *Service) Save(ctx context.Context, id connection.ID, typ connection.Type, cfg connection.Config) (*connection.Connection, error) {
	if id == "" {
		conn, err := s.connector(typ)
		if err != nil {
			return nil, err
		}

		c, err := connection.New(conn.Descriptor(), cfg, s.now())
		if err != nil {
			return nil, err
		}

		if err := s.conns.Save(ctx, c); err != nil {
			return nil, fmt.Errorf("saving the connection: %w", err)
		}

		return c, nil
	}

	c, err := s.conns.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	conn, err := s.connector(c.Type())
	if err != nil {
		return nil, err
	}

	if err := c.Reconfigure(conn.Descriptor(), cfg, s.now()); err != nil {
		return nil, err
	}

	if err := s.conns.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("saving the connection: %w", err)
	}

	return c, nil
}

// Delete removes a connection and its deliveries.
func (s *Service) Delete(ctx context.Context, id connection.ID) error {
	if _, err := s.conns.Get(ctx, id); err != nil {
		return err
	}

	// The deliveries go first: a connection left without them is harmless, deliveries left without
	// their connection would never be sent nor listed.
	if err := s.outbox.DeleteFor(ctx, id); err != nil {
		return fmt.Errorf("deleting the connection's deliveries: %w", err)
	}

	return s.conns.Delete(ctx, id)
}

// Test sends a sample message with the settings given, merged over the stored ones when id names a
// saved connection (so masked secrets keep theirs), in cfg's language. When the connector can find
// a setting the user left empty (Telegram's chat), it finds it first and returns it in found, for
// the form to fill. A saved connection that was paused is unpaused when its stored settings pass the
// test. Test sends even when sending is off: the user pressed it.
func (s *Service) Test(ctx context.Context, id connection.ID, typ connection.Type, cfg connection.Config) (found map[string]string, err error) {
	var (
		saved  *connection.Connection
		stored schema.Settings
	)

	if id != "" {
		saved, err = s.conns.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		typ = saved.Type()
		stored = saved.Settings()
		cfg.Language = cmp.Or(cfg.Language, saved.Language())
	}

	conn, err := s.connector(typ)
	if err != nil {
		return nil, err
	}

	d := conn.Descriptor()
	settings := d.MergeSettings(stored, cfg.Settings)

	found, err = s.findMissing(ctx, conn, d, settings)
	if err != nil {
		return nil, err
	}

	maps.Copy(settings, found)

	if err := conn.Test(ctx, settings, s.sample(ctx, cfg.Language)); err != nil {
		return found, err
	}

	// Only a pass of the very settings stored proves the pause is over; a pass of other, unsaved
	// ones says nothing about them (Save unpauses when those are stored).
	if saved != nil && saved.Paused() && maps.Equal(settings, stored) {
		if err := s.unpause(ctx, saved.ID(), stored); err != nil {
			return found, err
		}
	}

	return found, nil
}

// unpause lets connection id send again after its stored settings passed a test. It reads the
// connection again first: the user may have changed or deleted it while the sample was on its way,
// and saving the old copy would undo that.
func (s *Service) unpause(ctx context.Context, id connection.ID, tested schema.Settings) error {
	c, err := s.conns.Get(ctx, id)
	if errors.Is(err, connection.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("unpausing the connection: %w", err)
	}

	if !c.Paused() || !maps.Equal(c.Settings(), tested) {
		return nil // newer settings: the test said nothing about them
	}

	c.Unpause()

	if err := s.conns.Save(ctx, c); err != nil {
		return fmt.Errorf("unpausing the connection: %w", err)
	}

	return nil
}

// findMissing validates settings; when they miss something and the connector can find it, it asks
// the connector and returns what it found, as long as that fills an empty setting.
func (s *Service) findMissing(ctx context.Context, conn Connector, d connection.TypeDescriptor, settings schema.Settings) (map[string]string, error) {
	invalid := d.Validate(settings)
	if invalid == nil {
		return map[string]string{}, nil
	}

	finder, ok := conn.(ChatFinder)
	if !ok {
		return nil, invalid
	}

	key, value, err := finder.FindChat(ctx, settings)
	if err != nil {
		return nil, err
	}

	found := map[string]string{}
	if key != "" && value != "" && strings.TrimSpace(settings[key]) == "" {
		found[key] = value
	}

	filled := maps.Clone(settings)
	maps.Copy(filled, found)

	if err := d.Validate(filled); err != nil {
		return nil, err
	}

	return found, nil
}

// sample renders the test message: two invented games, as a collection message would list them.
func (s *Service) sample(ctx context.Context, lang string) Message {
	e := event.New(event.KindCollectionAdded, s.now(), event.CollectionAdded{
		Origin: event.OriginManual,
		Games: []event.GameRef{
			{
				ID:      "example-game",
				Title:   "Example Game",
				Systems: []string{game.SystemPC},
			},
			{
				ID:      "another-game",
				Title:   "Another Game",
				Systems: []string{game.SystemPC},
			},
		},
	})

	// No catalog and no address: the sample must not depend on the library or link to it.
	m := Render(ctx, e, lang, "", noGames{}, noCovers{})
	m.Text = text(lang, "test.text") + "\n\n" + m.Title + "\n" + m.Text
	m.Title = text(lang, "test.title")

	return m
}

// Deliveries returns a connection's latest deliveries, newest first.
func (s *Service) Deliveries(ctx context.Context, id connection.ID, limit int) ([]Delivery, error) {
	return s.outbox.Recent(ctx, id, limit)
}

// Address returns the Game Vault address messages link to, "" when unset.
func (s *Service) Address(ctx context.Context) (string, error) {
	return s.settings.Address(ctx)
}

// SaveAddress stores the Game Vault address messages link to: empty (no links), or an absolute
// http or https address without a query, stored without a trailing slash.
func (s *Service) SaveAddress(ctx context.Context, address string) error {
	address, err := cleanAddress(address)
	if err != nil {
		return err
	}

	return s.settings.SaveAddress(ctx, address)
}

func cleanAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", nil
	}

	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", ErrBadAddress
	}

	return strings.TrimRight(address, "/"), nil
}

// ConnectionIssueKey returns the Health key of connection id's own problem (its pause).
func ConnectionIssueKey(id connection.ID) string { return "connector:" + string(id) }

// noGames is the catalog of the test message: it holds nothing.
type noGames struct{}

func (noGames) List(context.Context) ([]*game.Game, error) { return nil, nil }

func (noGames) Get(context.Context, game.ID) (*game.Game, error) { return nil, errNoGame }

// noCovers is the cover store of the test message: it holds nothing.
type noCovers struct{}

func (noCovers) GetCover(game.ID, string) (media.Image, bool, error) {
	return media.Image{}, false, nil
}

var errNoGame = errors.New("no game")
