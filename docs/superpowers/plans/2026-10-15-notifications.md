# Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Send system events (new games, releases, deals, Health problems, expiring keys, failed sign-ins) to user-configured connectors (Telegram, a generic webhook, ntfy), each subscribed to the events it wants.

**Architecture:** A new domain (`event`, `connection`), an application package `notify` (publisher port, service with a durable SQLite outbox, dispatcher, Health watcher, daily job, message rendering in en/es), connectors as a new plugin piece (`plugin.Plugin.Connectors`), events published by the existing use cases through `WithEvents` options, a `NotificationService` API and a Settings → Notifications page.

**Tech Stack:** Go 1.26 (hexagonal/DDD, SQLite JSON docs, ConnectRPC/buf), React 19 + TypeScript, i18next (en/es), Task + the Docker toolchain.

**Spec:** `docs/superpowers/specs/2026-10-15-notifications-design.md`

## Global Constraints

- Everything builds and tests through Task in the toolchain container: `task test`, `task lint`, `task lint:fix`, `task generate`, `task i18n`, `task build`. Never host Go/Node.
- Before any Go: read `.claude/skills/write-go/SKILL.md`. Doc comment on every exported identifier and interface method; blank line between documented members; one struct field per line; functional options (`type Option func(*T)`); sentinel errors with `errkind`; tests GIVEN/WHEN/THEN with `ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)`; never a bare context.
- UI: `.claude/docs/ui.md`. Every string through `t()` in `web/src/i18n/locales/en.json` and `es.json` (English default, never Spanish-first).
- Repository is public: invented data only in tests, docs and commits (no real library titles, accounts, tokens, chat ids).
- Event kind strings are stored (subscriptions, outbox): `collection.added`, `release.announced`, `release.out`, `deal.historical_low`, `issue.opened`, `issue.resolved`, `keys.expiring`, `auth.failed_logins` — never renamed.
- Keys expiring rule: pending keys (`Copy.IsPendingKey()`) whose `RedeemBy` is within **30** days (the library's `EXPIRING_DAYS`), notified on entering the window and again at **7** and **1** day.
- Hand-added games batch: one `collection.added` when **10 minutes** pass without another hand-added game.
- Health watcher: every minute; an issue must be seen in **2** consecutive checks to open, missing from **2** to resolve; first run seeds silently.
- Failed sign-ins: per client address, **1-hour** windows, published at the **3rd** failure, once per window.
- Dispatcher: retries after **1 min, 5 min, 30 min, 2 h, 6 h**, then gives up; permanent (400/401/403/404) pauses the connection; deliveries older than **24 h** are dropped; sent deliveries kept **7 days**; **20** latest listed per connection; pace: Telegram one message every **3 s** per connection, ntfy and webhook **1 s**.
- Telegram caption limit **1024** characters (cut with "… and N more"); up to **2** images per message.
- With `-no-unattended` nothing is queued or delivered; only the user-pressed Test sends (the config copy holds real secrets).
- Webhook JSON: `{"version":1,"event":<kind>,"at":<RFC 3339>,"data":{…},"message":{"title","text","link"}}`; redirects refused.
- Next migration number: **0013**.
- Commit messages: plain English, no attribution lines.

## Review Focus

1. A test server on a copy of the config (`-no-unattended`) must never send: covered in Task 3 (Publish gate) and Task 7 (main.go wiring), plus Test still sending.
2. The Telegram bot token must never appear in an error, log line or API answer: Task 8 test asserts every error string lacks the token.
3. Restart safety: deliveries survive a restart and the watcher does not repeat or flood after one (Task 2 repositories round trip, Task 5 watcher seeded from the store).
4. Flapping problems (a source failing once) must not notify: Task 5 two-check margin tests, both directions.
5. A connector failing permanently must not loop notifying itself: Task 4 test that its own `connector_paused` issue is never queued for it.

---

## File Structure

**Create**
- `internal/domain/event/event.go`, `event_test.go` — kinds, groups, payloads, catalog.
- `internal/domain/connection/connection.go`, `connection_test.go` — Connection aggregate, TypeDescriptor.
- `internal/adapters/outbound/sqlite/migrations/0013_notifications.sql`
- `internal/adapters/outbound/sqlite/connection_repository.go`, `notify_store.go`, tests, `testdata/connection.v1.json`.
- `internal/application/notify/` — `ports.go`, `options.go`, `service.go`, `publish.go`, `render.go`, `texts.go`, `dispatcher.go`, `health.go`, `watcher.go`, `daily.go`, tests (`fakes_test.go`, `service_test.go`, `render_test.go`, `dispatcher_test.go`, `watcher_test.go`, `daily_test.go`, `texts_test.go`, `webparity_test.go`).
- `internal/adapters/outbound/telegram/` — `connector.go`, `plugin.go`, `connector_test.go`, `fake_test.go`.
- `internal/adapters/outbound/ntfy/` — same shape.
- `internal/adapters/outbound/webhook/` — same shape.
- `proto/gamevault/v1/notify.proto`; `internal/adapters/inbound/rpc/notify_handler.go`, `notify_mapper.go`, `notify_handler_test.go`.
- `web/src/features/notifications/NotificationsPage.tsx`, `ConnectionDialog.tsx`, `Deliveries.tsx`; `web/src/lib/notifications.ts`; `web/tests/notifications.test.mjs`.

**Modify**
- `internal/application/plugin/plugin.go`, `plugintest/plugintest.go`, `cmd/gamevault/plugins.go`.
- `internal/application/health/health.go` (+ web `lib/health.ts`, locales).
- `internal/application/sync/service.go`, `catalog/options.go` + `service.go` + `scanned.go`, `transfer/service.go`, `home/options.go` + `service.go`, `auth/service.go`.
- `internal/app/app.go`, `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server.go`.
- `web/src/api/client.ts`, `web/src/lib/route.ts`, `web/src/App.tsx`, `web/src/components/Icon.tsx`, `web/tests/route.test.mjs`, locales.
- `docs/technical.md`, `docs/plugins.md`, `README.md`, `.claude/MEMORY.md`, new `.claude/memory/telegram.md`, `.claude/memory/ntfy.md`.

---

### Task 1: Domain — events and connections

**Files:**
- Create: `internal/domain/event/event.go`, `internal/domain/event/event_test.go`
- Create: `internal/domain/connection/connection.go`, `internal/domain/connection/connection_test.go`

**Interfaces:**
- Produces: `event.Kind`, the eight `event.Kind…` constants, `event.Group` (`GroupActivity`, `GroupAttention`, `GroupSecurity`), `event.Kinds() []Kind`, `event.GroupOf(Kind) Group`, `event.Known(Kind) bool`, `event.Event{ID string; Kind Kind; At time.Time; Data any}`, payload structs (below), `event.New(kind, at, data) Event`; `connection.ID`, `connection.NewID()`, `connection.Type`, `connection.TypeDescriptor{Type; Name; DescriptionKey; Fields schema.Fields}` with `Validate`, `MergeSettings`, `Masked`; `connection.Config{Name; Enabled; Events []event.Kind; Language string; Settings schema.Settings}`; `connection.Connection` with `New`, `Rehydrate(Snapshot)`, accessors, `Reconfigure`, `Pause(reason, at)`, `Unpause()`, `Subscribed(event.Kind) bool`; errors `ErrNotFound`, `ErrUnknownType`, `ErrNoEvents`, `ErrUnknownEvent`, `ErrBadLanguage`; `connection.Languages() []string` (`"en"`, `"es"`).

- [ ] **Step 1: Write the failing tests**

`internal/domain/event/event_test.go`:

```go
package event_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/event"
)

// TestKinds_catalog pins the stored kind names and their groups: subscriptions and the outbox keep
// these strings, so a rename would silently drop what users chose.
func TestKinds_catalog(t *testing.T) {
	want := map[event.Kind]event.Group{
		"collection.added":    event.GroupActivity,
		"release.announced":   event.GroupActivity,
		"release.out":         event.GroupActivity,
		"deal.historical_low": event.GroupActivity,
		"issue.opened":        event.GroupAttention,
		"issue.resolved":      event.GroupAttention,
		"keys.expiring":       event.GroupAttention,
		"auth.failed_logins":  event.GroupSecurity,
	}

	got := map[event.Kind]event.Group{}
	for _, k := range event.Kinds() {
		got[k] = event.GroupOf(k)
		assert.True(t, event.Known(k), k)
	}

	assert.Equal(t, want, got)
	assert.False(t, event.Known("collection.removed"))
}
```

`internal/domain/connection/connection_test.go`:

```go
package connection_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

var telegram = connection.TypeDescriptor{
	Type: "telegram",
	Name: "Telegram",
	Fields: schema.Fields{
		{Key: "token", LabelKey: "x", Kind: schema.FieldSecret, Required: true},
		{Key: "chat", LabelKey: "y", Kind: schema.FieldText},
	},
}

func TestNew_validates(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	ok := connection.Config{
		Name:     "Phone",
		Enabled:  true,
		Events:   []event.Kind{event.KindCollectionAdded},
		Language: "es",
		Settings: schema.Settings{"token": "t-123"},
	}

	t.Run("GIVEN a complete configuration WHEN it is created THEN it keeps it", func(t *testing.T) {
		c, err := connection.New(telegram, ok, now)
		require.NoError(t, err)
		assert.Equal(t, "Phone", c.Name())
		assert.True(t, c.Subscribed(event.KindCollectionAdded))
		assert.False(t, c.Subscribed(event.KindKeysExpiring))
		assert.False(t, c.Paused())
	})

	for name, tc := range map[string]struct {
		edit func(*connection.Config)
		want error
	}{
		"no events":       {func(c *connection.Config) { c.Events = nil }, connection.ErrNoEvents},
		"an unknown kind": {func(c *connection.Config) { c.Events = []event.Kind{"x.y"} }, connection.ErrUnknownEvent},
		"a language":      {func(c *connection.Config) { c.Language = "fr" }, connection.ErrBadLanguage},
		"no token":        {func(c *connection.Config) { c.Settings = schema.Settings{} }, nil},
	} {
		t.Run("GIVEN "+name+" WHEN it is created THEN it is refused", func(t *testing.T) {
			cfg := ok
			cfg.Events = append([]event.Kind(nil), ok.Events...)
			tc.edit(&cfg)

			_, err := connection.New(telegram, cfg, now)
			require.Error(t, err)
			if tc.want != nil {
				assert.True(t, errors.Is(err, tc.want), err)
			}
		})
	}

	t.Run("GIVEN a paused connection WHEN it is reconfigured THEN it is unpaused", func(t *testing.T) {
		c, err := connection.New(telegram, ok, now)
		require.NoError(t, err)
		c.Pause("Telegram rejected the bot token", now)
		require.True(t, c.Paused())

		require.NoError(t, c.Reconfigure(telegram, ok, now.Add(time.Minute)))
		assert.False(t, c.Paused())
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task shell` then `go test ./internal/domain/event/ ./internal/domain/connection/` (or `docker run … go test …` with the volumes from `Taskfile.yml`).
Expected: FAIL — packages do not exist.

- [ ] **Step 3: Implement `internal/domain/event/event.go`**

```go
// Package event names what Game Vault tells the outside world about: things that happened to the
// collection, things that need the user, and security notices. Kinds are stored in subscriptions
// and in the notification queue, so their strings never change.
package event

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"gamevault/internal/domain/game"
)

// Kind names an event. Values are stored: never rename one.
type Kind string

// The event kinds, in the order the UI lists them.
const (
	KindCollectionAdded   Kind = "collection.added"
	KindReleaseAnnounced  Kind = "release.announced"
	KindReleaseOut        Kind = "release.out"
	KindHistoricalLow     Kind = "deal.historical_low"
	KindIssueOpened       Kind = "issue.opened"
	KindIssueResolved     Kind = "issue.resolved"
	KindKeysExpiring      Kind = "keys.expiring"
	KindFailedLogins      Kind = "auth.failed_logins"
)

// Group sorts kinds for the UI.
type Group string

// Values of Group.
const (
	GroupActivity  Group = "activity"
	GroupAttention Group = "attention"
	GroupSecurity  Group = "security"
)

var catalog = []struct {
	kind  Kind
	group Group
}{
	{KindCollectionAdded, GroupActivity},
	{KindReleaseAnnounced, GroupActivity},
	{KindReleaseOut, GroupActivity},
	{KindHistoricalLow, GroupActivity},
	{KindIssueOpened, GroupAttention},
	{KindIssueResolved, GroupAttention},
	{KindKeysExpiring, GroupAttention},
	{KindFailedLogins, GroupSecurity},
}

// Kinds returns every kind, in display order.
func Kinds() []Kind {
	out := make([]Kind, 0, len(catalog))
	for _, c := range catalog {
		out = append(out, c.kind)
	}

	return out
}

// GroupOf returns a kind's group, "" for an unknown kind.
func GroupOf(k Kind) Group {
	for _, c := range catalog {
		if c.kind == k {
			return c.group
		}
	}

	return ""
}

// Known reports whether k is a kind of the catalog.
func Known(k Kind) bool { return slices.Contains(Kinds(), k) }

// Event is one thing to tell: its kind, when it happened and its payload (one of the types below,
// matching the kind).
type Event struct {
	// ID identifies the event in the queue.
	ID string

	// Kind says what happened.
	Kind Kind

	// At is when it happened.
	At time.Time

	// Data is the payload: CollectionAdded for KindCollectionAdded, and so on.
	Data any
}

// New returns an event with a new id.
func New(kind Kind, at time.Time, data any) Event {
	return Event{
		ID:   uuid.Must(uuid.NewV7()).String(),
		Kind: kind,
		At:   at,
		Data: data,
	}
}

// Origin of games that entered the collection.
type Origin string

// Values of Origin. A source scan's origin is the source's name, in OriginName.
const (
	OriginSource Origin = "source"
	OriginManual Origin = "manual"
	OriginImport Origin = "import"
)

// GameRef is a game named in an event.
type GameRef struct {
	// ID is the game's id.
	ID game.ID

	// Title is its title.
	Title string

	// Systems are the systems of its copies, sorted.
	Systems []string
}

// CollectionAdded is KindCollectionAdded's payload.
type CollectionAdded struct {
	// Origin says where the games came from.
	Origin Origin

	// OriginName is the source's name for OriginSource, empty otherwise.
	OriginName string

	// Games are the games created, in the order they were created.
	Games []GameRef

	// CopiesAdded counts copies added to games the user already had.
	CopiesAdded int
}

// Release is KindReleaseAnnounced's and KindReleaseOut's payload.
type Release struct {
	// Title, Date (YYYY-MM-DD, "" when not announced), Platforms, CoverURL and URL are the release's.
	Title string

	// Date is when it comes out, "" when not announced.
	Date game.Date

	// Platforms are the platforms it comes out on.
	Platforms []string

	// CoverURL is its public cover, "" without one.
	CoverURL string

	// URL is its page on the database that announced it.
	URL string

	// Because and BecauseTitle are the owned game that explains it.
	Because game.ID

	// BecauseTitle is that game's title.
	BecauseTitle string
}

// HistoricalLow is KindHistoricalLow's payload.
type HistoricalLow struct {
	// Deal is the deal as the provider gave it.
	Deal game.Deal
}

// Issue is KindIssueOpened's and KindIssueResolved's payload: a Health issue, in plain data.
type Issue struct {
	// Key identifies the issue (Health's key).
	Key string

	// Level is "error" or "warning".
	Level string

	// Kind, Source, Reason, Params, Message, Target and TargetID are Health's.
	Kind string

	// Source names the provider or source; empty for backups and disk.
	Source string

	// Reason is Health's reason code.
	Reason string

	// Params fill the reason's text.
	Params map[string]string

	// Message is the English detail.
	Message string

	// Since is when the problem began.
	Since time.Time

	// Target is the page that fixes it.
	Target string

	// TargetID names the item on that page.
	TargetID string
}

// ExpiringKey is one key of KeysExpiring.
type ExpiringKey struct {
	// Game is the key's game.
	Game GameRef

	// Store is where the key redeems (the copy's platform).
	Store string

	// RedeemBy is the deadline.
	RedeemBy game.Date

	// DaysLeft is how many days remain (0: today).
	DaysLeft int
}

// KeysExpiring is KindKeysExpiring's payload, nearest deadline first.
type KeysExpiring struct {
	// Keys are the keys to tell about.
	Keys []ExpiringKey
}

// FailedLogins is KindFailedLogins's payload.
type FailedLogins struct {
	// Address is the client address.
	Address string

	// Count is how many sign-ins failed in the window.
	Count int

	// First and Last bound the attempts.
	First time.Time

	// Last is the latest attempt.
	Last time.Time
}
```

Give every payload field a lower-camel `json` tag (`json:"originName"`, `json:"copiesAdded"`, …; `game.Deal` is embedded as `json:"deal"`): the queue's codec and the webhook both send these names, so they are part of the stored format. Run `task lint:fix` after writing (doc spacing, field lines), then fix by hand what remains.

- [ ] **Step 4: Implement `internal/domain/connection/connection.go`**

```go
// Package connection is a configured notification connector: which service, its settings, the
// events it sends and in which language.
package connection

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

// ID identifies a connection.
type ID string

// NewID returns a new id.
func NewID() ID { return ID(uuid.Must(uuid.NewV7()).String()) }

// Type names a connector type ("telegram", "ntfy", "webhook").
type Type string

// TypeDescriptor describes a connector type: its name and settings.
type TypeDescriptor struct {
	// Type is the type's id.
	Type Type

	// Name is shown in the UI.
	Name string

	// DescriptionKey is the i18n key of a one-line description.
	DescriptionKey string

	// Fields are the settings it needs.
	Fields schema.Fields
}

// Validate checks settings against the fields.
func (d TypeDescriptor) Validate(s schema.Settings) error { return d.Fields.Validate(s, d.Name) }

// MergeSettings merges incoming settings over stored ones (secrets sent back masked keep theirs).
func (d TypeDescriptor) MergeSettings(stored, incoming schema.Settings) schema.Settings {
	return d.Fields.Merge(stored, incoming)
}

// Masked returns the settings with secrets masked, for clients.
func (d TypeDescriptor) Masked(s schema.Settings) schema.Settings { return d.Fields.Masked(s) }

// Errors of a connection.
var (
	// ErrNotFound means no connection has the id.
	ErrNotFound = errkind.New(errkind.NotFound, "connection not found")

	// ErrUnknownType means no connector has the type.
	ErrUnknownType = errkind.New(errkind.NotFound, "unknown connector type")

	// ErrNoEvents means the connection would send nothing.
	ErrNoEvents = errkind.New(errkind.Invalid, "choose at least one event to send")

	// ErrUnknownEvent means an event kind is not in the catalog.
	ErrUnknownEvent = errkind.New(errkind.Invalid, "unknown event")

	// ErrBadLanguage means the language is not one messages are written in.
	ErrBadLanguage = errkind.New(errkind.Invalid, "messages can be written in English (en) or Spanish (es)")
)

// Languages returns the languages messages are written in.
func Languages() []string { return []string{"en", "es"} }

// Config is what the user sets.
type Config struct {
	// Name labels the connection.
	Name string

	// Enabled turns sending on.
	Enabled bool

	// Events are the kinds it sends.
	Events []event.Kind

	// Language is the messages' language ("en" or "es").
	Language string

	// Settings are the connector's settings.
	Settings schema.Settings
}

// Connection is a configured connector.
type Connection struct {
	id          ID
	typ         Type
	name        string
	enabled     bool
	events      []event.Kind
	language    string
	settings    schema.Settings
	pausedWhy   string
	pausedAt    time.Time
	createdAt   time.Time
	updatedAt   time.Time
}

// New returns a connection of type d.
func New(d TypeDescriptor, cfg Config, now time.Time) (*Connection, error) {
	c := &Connection{
		id:        NewID(),
		typ:       d.Type,
		createdAt: now,
	}

	return c, c.Reconfigure(d, cfg, now)
}

// Reconfigure replaces the configuration (settings merged, secrets kept) and unpauses it: the user
// fixed what paused it, or will be told again.
func (c *Connection) Reconfigure(d TypeDescriptor, cfg Config, now time.Time) error {
	if len(cfg.Events) == 0 {
		return ErrNoEvents
	}

	for _, k := range cfg.Events {
		if !event.Known(k) {
			return errkind.Wrap(errkind.Invalid, fmt.Errorf("%w: %q", ErrUnknownEvent, k))
		}
	}

	if !slices.Contains(Languages(), cfg.Language) {
		return ErrBadLanguage
	}

	settings := d.MergeSettings(c.settings, cfg.Settings)
	if err := d.Validate(settings); err != nil {
		return err
	}

	c.name = strings.TrimSpace(cmp.Or(cfg.Name, d.Name))
	c.enabled = cfg.Enabled
	c.events = slices.Clone(cfg.Events)
	c.language = cfg.Language
	c.settings = settings
	c.Unpause()
	c.updatedAt = now

	return nil
}
```

Complete the file with: accessors `ID() ID`, `Type() Type`, `Name() string`, `Enabled() bool`, `Events() []event.Kind` (clone), `Language() string`, `Settings() schema.Settings` (clone), `Paused() bool` (`pausedWhy != ""`), `PausedWhy() string`, `PausedAt() time.Time`, `CreatedAt()`, `UpdatedAt()`; `Subscribed(k event.Kind) bool` (`enabled && slices.Contains(events, k)` — note: subscription ignores pause; the service checks pause separately); `Pause(why string, at time.Time)`; `Unpause()`; `Snapshot` struct with every field exported and `Rehydrate(s Snapshot) *Connection`. Imports `cmp`, `fmt`. `ErrUnknownEvent` wrapped with `%w` keeps `errors.Is` working (the test checks it).

- [ ] **Step 5: Run tests**

Run: `go test ./internal/domain/event/ ./internal/domain/connection/` → PASS. Then `task lint`.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/event internal/domain/connection
git commit -m "Domain: notification events and connections"
```

---

### Task 2: SQLite — migration 0013 and the stores

**Files:**
- Create: `internal/adapters/outbound/sqlite/migrations/0013_notifications.sql`
- Create: `internal/adapters/outbound/sqlite/connection_repository.go`, `internal/adapters/outbound/sqlite/notify_store.go`
- Create: `internal/adapters/outbound/sqlite/connection_repository_test.go`, `notify_store_test.go`, `testdata/connection.v1.json`
- Modify: `internal/adapters/outbound/sqlite/docs.go` (add `connectionDocVersion = 1`, `connectionDoc`, `encodeConnection`, `decodeConnection`), `migrate_test.go` (add `TestMigration0013`), `golden_test.go` (add the connection golden).

**Interfaces:**
- Consumes: `connection.Connection`, `connection.Snapshot`, `connection.Rehydrate`, `event.Event`.
- Produces (the `notify` ports Task 3 declares; implement exactly these signatures):
  - `type ConnectionRepository struct{…}`; `NewConnectionRepository(db *DB) *ConnectionRepository`; `List(ctx) ([]*connection.Connection, error)` (by name, NOCASE, then id); `Get(ctx, id connection.ID) (*connection.Connection, error)`; `Save(ctx, c *connection.Connection) error`; `Delete(ctx, id connection.ID) error`.
  - `type NotifyStore struct{ db *DB }`; `NewNotifyStore(db *DB) *NotifyStore` implementing `notify.Outbox`, `notify.IssueMemory`, `notify.KeyMemory`, `notify.SettingsStore` (Task 3 lists the methods; this task implements them, with the types in package `notify`). To avoid an import of `application/notify` from `sqlite` creating a cycle, `notify` must not import `sqlite` (it does not), so `sqlite` may import `notify` types — as `sqlite/home_snapshots.go` imports `home`.

**Order note:** Task 3 defines the `notify` types this task stores. Implement Task 2 **after** Task 3 Step 3 (the ports file) exists, or write the ports file first as part of this task: create `internal/application/notify/ports.go` exactly as in Task 3 Step 3 here, and Task 3 then starts from it.

- [ ] **Step 1: Migration**

`0013_notifications.sql`:

```sql
-- Notifications (docs/superpowers/specs/2026-10-15-notifications-design.md): the configured
-- connectors as JSON documents, the queue of deliveries, and what was already told, so a restart
-- neither repeats nor loses a notification.
CREATE TABLE connections (
  id  TEXT PRIMARY KEY,
  doc TEXT NOT NULL
);
CREATE INDEX connections_name ON connections(json_extract(doc, '$.name') COLLATE NOCASE);

-- One row per event and connection. state: queued, sent, failed (given up), dropped (too old).
CREATE TABLE notification_outbox (
  id            TEXT PRIMARY KEY,
  connection_id TEXT NOT NULL,
  event         TEXT NOT NULL,
  state         TEXT NOT NULL,
  attempts      INTEGER NOT NULL DEFAULT 0,
  next_at       TEXT NOT NULL,
  last_error    TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX notification_outbox_due ON notification_outbox(state, next_at);
CREATE INDEX notification_outbox_connection ON notification_outbox(connection_id, created_at);

-- Health issues already notified (the watcher's memory), by Health's key.
CREATE TABLE notified_issues (
  key   TEXT PRIMARY KEY,
  issue TEXT NOT NULL,
  since TEXT NOT NULL
);

-- Deadline thresholds already notified per key copy (30, 7, 1 days).
CREATE TABLE notified_keys (
  copy_id   TEXT NOT NULL,
  threshold INTEGER NOT NULL,
  at        TEXT NOT NULL,
  PRIMARY KEY (copy_id, threshold)
);
```

The Game Vault address is a key/value setting (`d.writeSetting(ctx, "notifications", json)`), no table.

- [ ] **Step 2: Write the failing tests**

`TestMigration0013` in `migrate_test.go`, copying `TestMigration0012`: open up to `"0012"`, reopen at head, assert the four tables exist and `pre-migration-0013.db` was made.

`connection_repository_test.go` (GWT): save a connection (invented name "Phone", token "t-123", events collection.added + issue.opened, language es), list it, get it, pause it and save, reload (pause kept), delete, get → `connection.ErrNotFound`.

`notify_store_test.go`:
- Outbox: `Enqueue` two deliveries for two connections; `Due(now)` returns them in `next_at, created_at` order; `MarkSent` / `MarkRetry(id, attempts, next, err)` / `MarkFailed` / `MarkDropped`; `Recent(conn, 20)` newest first; `Prune(before)` removes sent/failed/dropped older than `before` but never queued ones.
- IssueMemory: `Notified(ctx)` empty → `SaveNotified(ctx, issues)` → reload equal; `Seeded(ctx) (bool, error)` false until `MarkSeeded`.
- KeyMemory: `KeyThresholds(ctx, copyIDs)` → map; `RememberKey(ctx, copyID, threshold, at)`; `ForgetKeys(ctx, keep []game.ID)` deletes rows of copies not in keep.
- SettingsStore: `Address(ctx)` "" → `SaveAddress(ctx, "http://192.168.1.10:8080")` → read back.

Event JSON round trip: `Enqueue` stores `event.Event` encoded by `notify.EncodeEvent` and `Due` decodes with `notify.DecodeEvent` (Task 3) — assert a `CollectionAdded` payload survives (games, origin, copies count).

`testdata/connection.v1.json`: a golden document for `encodeConnection` (invented values), checked by `golden_test.go` like `source.v1.json`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/adapters/outbound/sqlite/` → FAIL (undefined types).

- [ ] **Step 4: Implement**

`docs.go`:

```go
// connectionDoc is the stored form of a connection.
type connectionDoc struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Enabled   bool            `json:"enabled"`
	Events    []string        `json:"events"`
	Language  string          `json:"language"`
	Settings  schema.Settings `json:"settings,omitempty"`
	PausedWhy string          `json:"pausedWhy,omitempty"`
	PausedAt  string          `json:"pausedAt,omitempty"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
}
```

`encodeConnection(c *connection.Connection) (string, error)` and `decodeConnection(id connection.ID, raw string) (*connection.Connection, error)` follow `encodeSource`/`decodeSource` (`checkVersion(doc.V, connectionDocVersion)`, nil settings → `schema.Settings{}`, `formatTime`/`parseTime`, `timeOrEmpty` for `PausedAt`).

`connection_repository.go` mirrors `source_repository.go` with `docTable[connection.ID, *connection.Connection]{db: db, table: "connections", noun: "connection", encode: encodeConnection, decode: decodeConnection, notFound: connection.ErrNotFound}`; `List` uses `ORDER BY json_extract(doc, '$.name') COLLATE NOCASE, id`.

`notify_store.go`: plain SQL through `s.db.conn(ctx)` like `home_snapshots.go`; times with `formatTime`/`parseTime`; the outbox `event` column holds `notify.EncodeEvent(e)`.

- [ ] **Step 5: Run tests** → PASS; `task lint`.

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/outbound/sqlite internal/application/notify/ports.go
git commit -m "SQLite: connections, the notification queue and what was already told (migration 0013)"
```

---

### Task 3: `notify` — ports, service, publishing and messages

**Files:**
- Create: `internal/application/notify/ports.go`, `options.go`, `service.go`, `publish.go`, `codec.go`, `render.go`, `texts.go`
- Create: `fakes_test.go`, `service_test.go`, `publish_test.go`, `render_test.go`, `texts_test.go`

**Interfaces:**
- Consumes: Task 1 domain; Task 2 stores satisfy these ports.
- Produces:

```go
// ports.go (package notify)

// Connector is the plugin piece that sends messages to one kind of service.
type Connector interface {
	// Descriptor describes the connector type and its settings.
	Descriptor() connection.TypeDescriptor

	// Send delivers m with settings. An error wrapping ErrPermanent means retrying cannot help
	// (a rejected token, an unknown chat); any other error is retried.
	Send(ctx context.Context, settings schema.Settings, m Message) error

	// Test sends a short sample with settings; nil means it arrived.
	Test(ctx context.Context, settings schema.Settings, m Message) error
}

// ChatFinder is implemented by connectors that can find where to send (Telegram: the last chat
// that wrote to the bot).
type ChatFinder interface {
	// FindChat returns the settings key to fill and its value.
	FindChat(ctx context.Context, settings schema.Settings) (key, value string, err error)
}

// Pacer is implemented by connectors that need more than a second between messages.
type Pacer interface {
	// Gap returns the time to leave between two messages of one connection.
	Gap() time.Duration
}

// Connections stores the configured connections.
type Connections interface {
	// List returns every connection by name.
	List(ctx context.Context) ([]*connection.Connection, error)

	// Get returns one, or connection.ErrNotFound.
	Get(ctx context.Context, id connection.ID) (*connection.Connection, error)

	// Save stores it.
	Save(ctx context.Context, c *connection.Connection) error

	// Delete removes it (its queued deliveries are deleted by the service).
	Delete(ctx context.Context, id connection.ID) error
}

// Delivery is one event to send through one connection.
type Delivery struct {
	// ID identifies the delivery.
	ID string

	// Connection is where it goes.
	Connection connection.ID

	// Event is what it tells.
	Event event.Event

	// State is queued, sent, failed or dropped.
	State State

	// Attempts counts failed sends.
	Attempts int

	// NextAt is when it is due.
	NextAt time.Time

	// LastError is the last failure's reason.
	LastError string

	// CreatedAt and UpdatedAt are bookkeeping.
	CreatedAt time.Time

	// UpdatedAt is the last change.
	UpdatedAt time.Time
}

// State of a delivery.
type State string

// Values of State.
const (
	StateQueued  State = "queued"
	StateSent    State = "sent"
	StateFailed  State = "failed"
	StateDropped State = "dropped"
)

// Outbox is the durable queue.
type Outbox interface {
	// Enqueue stores new deliveries.
	Enqueue(ctx context.Context, ds []Delivery) error

	// Due returns queued deliveries due at now, oldest first.
	Due(ctx context.Context, now time.Time) ([]Delivery, error)

	// MarkSent, MarkRetry, MarkFailed and MarkDropped record an outcome.
	MarkSent(ctx context.Context, id string, at time.Time) error

	// MarkRetry records a failed attempt and when to try again.
	MarkRetry(ctx context.Context, id string, attempts int, next time.Time, reason string, at time.Time) error

	// MarkFailed gives a delivery up.
	MarkFailed(ctx context.Context, id string, reason string, at time.Time) error

	// MarkDropped records a delivery too old to send.
	MarkDropped(ctx context.Context, id string, at time.Time) error

	// Recent returns a connection's latest deliveries, newest first.
	Recent(ctx context.Context, id connection.ID, limit int) ([]Delivery, error)

	// DeleteFor removes a connection's deliveries.
	DeleteFor(ctx context.Context, id connection.ID) error

	// Prune removes finished deliveries older than before.
	Prune(ctx context.Context, before time.Time) error
}

// IssueMemory is the watcher's memory of notified Health issues.
type IssueMemory interface {
	// Notified returns the issues told as open, by key.
	Notified(ctx context.Context) (map[string]event.Issue, error)

	// SaveNotified replaces them.
	SaveNotified(ctx context.Context, issues map[string]event.Issue) error

	// Seeded reports whether the watcher ever ran.
	Seeded(ctx context.Context) (bool, error)

	// MarkSeeded records that it ran.
	MarkSeeded(ctx context.Context) error
}

// KeyMemory remembers the deadline thresholds told per key copy.
type KeyMemory interface {
	// KeyThresholds returns the thresholds told for each copy.
	KeyThresholds(ctx context.Context) (map[game.ID][]int, error)

	// RememberKey records a threshold told.
	RememberKey(ctx context.Context, copyID game.ID, threshold int, at time.Time) error

	// ForgetKeys drops the copies not in keep (redeemed, deleted).
	ForgetKeys(ctx context.Context, keep []game.ID) error
}

// SettingsStore keeps the notification settings.
type SettingsStore interface {
	// Address returns the Game Vault address messages link to, "" when unset.
	Address(ctx context.Context) (string, error)

	// SaveAddress stores it.
	SaveAddress(ctx context.Context, address string) error
}

// CoverFiles reads a game's cover from the local game data, never from the network.
type CoverFiles interface {
	// GetCover returns the cover of the game's edition on system; false when none is stored.
	GetCover(id game.ID, system string) (media.Image, bool, error)
}

// Catalog reads the games (for covers, keys and names).
type Catalog interface {
	// List returns every game.
	List(ctx context.Context) ([]*game.Game, error)

	// Get returns one game.
	Get(ctx context.Context, id game.ID) (*game.Game, error)
}

// Issues is Health, as the watcher reads it.
type Issues interface {
	// Issues returns the current problems.
	Issues(ctx context.Context) []health.Issue
}

// Publisher is what use cases publish events through.
type Publisher interface {
	// Publish queues e for the connections subscribed to its kind.
	Publish(ctx context.Context, e event.Event)
}

// Nop is a Publisher that drops every event: the default of every use case's option.
type Nop struct{}

// Publish implements Publisher.
func (Nop) Publish(context.Context, event.Event) {}

// ErrPermanent marks a send failure retrying cannot fix.
var ErrPermanent = errors.New("permanent delivery failure")
```

- `Message`:

```go
// Message is what a connector sends.
type Message struct {
	// Title is a short headline.
	Title string

	// Text is the body, plain text with line breaks.
	Text string

	// Important is true for attention and security events (ntfy high priority).
	Important bool

	// Link opens the thing in Game Vault (or a deal's or a release's public page), "" without one.
	Link string

	// LinkLabel is the link's text in the connection's language ("Open in Game Vault").
	LinkLabel string

	// Images are at most two.
	Images []Image

	// Event is the event, for connectors that send data (the webhook).
	Event event.Event
}

// Image is either bytes (a local cover) or a public URL (a deal's or a release's art).
type Image struct {
	// Name is a file name.
	Name string

	// Data and ContentType hold a local image.
	Data []byte

	// ContentType is Data's type.
	ContentType string

	// URL is a public image instead.
	URL string
}
```

- `Service`: `NewService(conns Connections, outbox Outbox, settings SettingsStore, catalog Catalog, covers CoverFiles, opts ...Option) *Service`; options `WithConnectors(cs ...Connector)`, `WithClock(port.Clock)`, `WithLogger(*slog.Logger)`, `WithSending(bool)` (default **false**: nothing is queued unless main turns it on; the Test path always sends), `WithBatchWindow(time.Duration)` (default 10 min, for tests). Methods:
  - `Types() []connection.TypeDescriptor`
  - `List(ctx) ([]*connection.Connection, error)`, `Get(ctx, id)`
  - `Save(ctx, id connection.ID /* "" = new */, typ connection.Type, cfg connection.Config) (*connection.Connection, error)` (new: `connection.New`; existing: `Reconfigure`)
  - `Delete(ctx, id)` (also `outbox.DeleteFor`)
  - `Test(ctx, id connection.ID, typ connection.Type, cfg connection.Config) (found map[string]string, err error)` — merges with the stored settings when `id` is set (masked secrets), validates, renders the sample (`event.KindCollectionAdded` with two invented games "Example Game", "Another Game", no covers) in `cfg.Language`, calls `Connector.Test`; when the connector is a `ChatFinder` and a field it finds is empty, calls `FindChat` first and returns `{key: value}`; on success of a saved, paused connection, unpauses and saves it.
  - `Deliveries(ctx, id, limit) ([]Delivery, error)`
  - `Address(ctx)`, `SaveAddress(ctx, address string) error` (validates: empty, or an absolute `http`/`https` URL without query; error `ErrBadAddress = errkind.New(errkind.Invalid, "the address must start with http:// or https://")`; stored without a trailing slash)
  - `Publish(ctx, e event.Event)` implements `Publisher`.
- `EncodeEvent(e event.Event) (string, error)`, `DecodeEvent(raw string) (event.Event, error)` in `codec.go`: JSON `{"id","kind","at","data"}` where `data` is decoded into the payload type matching `kind` (a switch over the eight kinds); unknown kind → error.

- [ ] **Step 1: Write the failing tests**

`fakes_test.go`: in-memory `Connections`, `Outbox`, `SettingsStore`, `Catalog` (games built with `game.New` + `AddCopy`), `CoverFiles` (map of id → image), and `fakeConnector{typ; sent []Message; err error; found string}` implementing `Connector`, `ChatFinder`.

`publish_test.go` (GWT):
1. GIVEN sending on, a connection subscribed to `collection.added` and one to `issue.opened` only, WHEN a `collection.added` is published THEN one delivery is queued, for the first, due now.
2. GIVEN a disabled connection, or a paused one, THEN nothing is queued for it.
3. GIVEN sending off (`WithSending(false)`) THEN nothing is queued (Review Focus 1).
4. GIVEN a `collection.added` with `OriginManual`, WHEN two arrive 4 minutes apart THEN nothing is queued until 10 minutes after the second; THEN one delivery whose payload has both games (driven by `Flush(now)`, the method the dispatcher loop calls each tick — see Task 4; expose `(*Service).FlushBatches(ctx)` here).
5. GIVEN an `issue.opened` whose `Key` is `"connector:<id>"` THEN it is not queued for connection `<id>` but is for the others (Review Focus 5).

`service_test.go`: Save validates (unknown type → `connection.ErrUnknownType`), Save of an existing connection keeps a masked secret, Delete removes its deliveries, SaveAddress rejects `ftp://x`, `Test` with an empty chat calls `FindChat` and returns the found value, `Test` of a paused saved connection that succeeds unpauses it.

`render_test.go`: for each kind and both languages, `Render(e, lang, address, catalog, covers)` produces the titles/texts below (assert exact strings for en, and that es differs and contains the title); collection with 3 games, 2 with covers on disk → 2 images with bytes; 30 games → text lists the first 20 and "… and 10 more"; no address → empty `Link`; address set → `Link` = `<address>/#/game/<id>` for one game, `<address>/#/library/recent` for several; deal → image URL = `Deal.ImageURL`; release → image URL = `CoverURL`.

`texts_test.go`: every key in `texts["en"]` exists in `texts["es"]` and vice versa, and both are non-empty.

- [ ] **Step 2: Run tests to verify they fail** → FAIL (undefined).

- [ ] **Step 3: Implement `ports.go`** (as above; imports `context`, `errors`, `time`, `connection`, `event`, `game`, `schema`, `media` (for `media.Image`), `health`).

- [ ] **Step 4: Implement `service.go`, `publish.go`, `codec.go`**

`publish.go`:

```go
// Publish implements Publisher: it queues one delivery per enabled, unpaused connection subscribed
// to e's kind. Games added by hand wait in a batch (see FlushBatches). A failure is logged: an event
// that cannot be queued is not worth failing the use case that reported it.
func (s *Service) Publish(ctx context.Context, e event.Event) {
	if !s.sending {
		return
	}

	if added, ok := e.Data.(event.CollectionAdded); ok && added.Origin == event.OriginManual {
		s.batch(e.At, added)

		return
	}

	if err := s.enqueue(ctx, e); err != nil {
		s.log.Warn("queueing a notification", "event", e.Kind, "error", err)
	}
}

func (s *Service) enqueue(ctx context.Context, e event.Event) error {
	conns, err := s.conns.List(ctx)
	if err != nil {
		return err
	}

	now := s.now()
	ds := make([]Delivery, 0, len(conns))

	for _, c := range conns {
		if !c.Subscribed(e.Kind) || c.Paused() || ownIssue(e, c.ID()) {
			continue
		}

		ds = append(ds, Delivery{
			ID:         uuid.Must(uuid.NewV7()).String(),
			Connection: c.ID(),
			Event:      e,
			State:      StateQueued,
			NextAt:     now,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	if len(ds) == 0 {
		return nil
	}

	if err := s.outbox.Enqueue(ctx, ds); err != nil {
		return err
	}

	s.wake()

	return nil
}

// ownIssue reports whether e is the problem of connection id itself: a connection that cannot
// send would only queue its own alarm forever.
func ownIssue(e event.Event, id connection.ID) bool {
	is, ok := e.Data.(event.Issue)

	return ok && is.Key == ConnectionIssueKey(id)
}
```

`ConnectionIssueKey(id) string` returns `"connector:" + string(id)` (used by Task 4's Health check too). `batch` keeps `pending event.CollectionAdded` and `lastAt` under a mutex (games appended, `CopiesAdded` summed); `FlushBatches(ctx)` enqueues and clears when `now - lastAt >= window`. `wake()` does a non-blocking send on a `chan struct{}` of size 1 the dispatcher selects on.

`codec.go`: as described in Interfaces, with one test-covered `decodeData(kind, raw json.RawMessage) (any, error)` switch.

- [ ] **Step 5: Implement `render.go` and `texts.go`**

`texts.go` holds `var texts = map[string]map[string]string{"en": {...}, "es": {...}}` and `func text(lang, key string, args ...any) string` (falls back to English, then to the key; `fmt.Sprintf` with the args). Keys and English values (Spanish in brackets):

| key | en | es |
| --- | --- | --- |
| `added.title.source` | `%s: %d new games` | `%s: %d juegos nuevos` |
| `added.title.source.one` | `%s: a new game` | `%s: un juego nuevo` |
| `added.title.manual` | `%d games added by hand` | `%d juegos añadidos a mano` |
| `added.title.manual.one` | `A game added by hand` | `Un juego añadido a mano` |
| `added.title.import` | `CSV import: %d new games` | `Importación CSV: %d juegos nuevos` |
| `added.copies` | `and %d new copies of games you had` | `y %d copias nuevas de juegos que ya tenías` |
| `more` | `… and %d more` | `… y %d más` |
| `release.announced.title` | `New in your series: %s` | `Nuevo en tu saga: %s` |
| `release.out.title` | `Out today: %s` | `Hoy sale %s` |
| `release.because` | `Because you have %s` | `Porque tienes %s` |
| `release.tba` | `date to be announced` | `fecha por anunciar` |
| `deal.title` | `%s at its historical low` | `%s en mínimo histórico` |
| `deal.text` | `%s (−%d%%, was %s) at %s` | `%s (−%d %%, antes %s) en %s` |
| `issue.opened.title` | `⚠ %s` | `⚠ %s` |
| `issue.resolved.title` | `✓ %s works again` | `✓ %s vuelve a funcionar` |
| `keys.title` | `%d keys expire within 30 days` | `%d claves caducan en 30 días` |
| `keys.title.one` | `A key expires within 30 days` | `Una clave caduca en 30 días` |
| `keys.line` | `%s (%s): %s` | `%s (%s): %s` |
| `days.today` | `today` | `hoy` |
| `days.one` | `in 1 day` | `en 1 día` |
| `days.other` | `in %d days` | `en %d días` |
| `logins.title` | `%d failed sign-ins from %s` | `%d accesos fallidos desde %s` |
| `logins.text` | `Between %s and %s. If it was not you, check who can reach Game Vault.` | `Entre %s y %s. Si no has sido tú, revisa quién puede acceder a Game Vault.` |
| `open` | `Open in Game Vault` | `Abrir en Game Vault` |
| `open.page` | `Open the page` | `Abrir la página` |
| `test.title` | `Game Vault test` | `Prueba de Game Vault` |
| `test.text` | `This connection works. Example Game and Another Game would be listed like this.` | `Esta conexión funciona. Example Game y Another Game aparecerían así.` |
| `reason.*` | one per Health reason (the 12 of `health.Reasons()` plus `connector_paused`), English texts matching `web/src/i18n/locales/en.json` `health.reasons.*`, Spanish matching `es.json`; the issue title is `issue.opened.title` with `"<Source or kind name>: <reason text>"` | |

`render.go`: `func Render(ctx context.Context, e event.Event, lang, address string, catalog Catalog, covers CoverFiles) Message`. Rules:
- `collection.added`: title by origin and count (`.one` when 1); text = one line per game `"<title> (<systems joined ", ">)"` for the first 20, then `more`; then `added.copies` when `CopiesAdded > 0`; images: for the games in order, the main edition's cover (`g.MainEdition().System`, via `catalog.Get` + `covers.GetCover`) until two are found; link as in the test.
- `release.*`: title; text = date (`release.tba` when empty) + " · " + platforms, then `release.because`; image = `CoverURL`; link = the release's `URL` when no address (it is public), else `<address>/#/game/<Because>`.
- `deal.historical_low`: title; text = `deal.text` with price, cut, regular and shop. There is no money formatter on the server: add `formatMoney(m game.Money, lang string) string` in `render.go` (minor units with `money.CurrencyDigits(m.Currency)` decimals; `es`: `9,99 €`; `en`: `€9.99` for EUR, `$9.99` for USD, else `9.99 XXX`), with a table test; image = `ImageURL`; link = the deal's `URL` (ITAD's link, unchanged).
- `issue.*`: title as in the table; text = `Message` (English detail) when present; `Important` true; link `<address>/#/system/health` when address set.
- `keys.expiring`: title by count; one `keys.line` per key: title, store, days text; `Important` true; link `<address>/#/library/expiring`.
- `auth.failed_logins`: title and text with times formatted `15:04` in local time; `Important` true.

- [ ] **Step 6: Run tests** → PASS; `task lint`.

- [ ] **Step 7: Commit**

```bash
git add internal/application/notify
git commit -m "Notify: connections, publishing into the queue, and messages in English and Spanish"
```

---

### Task 4: Dispatcher and the connector Health check

**Files:**
- Create: `internal/application/notify/dispatcher.go`, `internal/application/notify/health.go`, `dispatcher_test.go`, `health_test.go`
- Modify: `internal/application/health/health.go` (add `KindConnector Kind = "connector"`, `TargetNotifications Target = "notifications"`, `ReasonConnectorPaused Reason = "connector_paused"` and add it to `reasons`); `web/src/lib/health.ts` (`ConnectorPaused: 'connector_paused'` in `Reason`, `issueHref` case `'notifications': return '#/notifications'`); `web/src/i18n/locales/en.json` + `es.json` (`health.reasons.connector_paused`: "Notifications to {{source}} are paused: {{reason}}" / "Las notificaciones a {{source}} están en pausa: {{reason}}"; `health.kinds.connector` if `issueTitle` needs it — check `web/src/lib/health.ts`); `web/tests/health.test.mjs` (href case).

**Interfaces:**
- Produces: `(*Service).Run(ctx context.Context)` — the dispatcher loop; `(*Service).Issues(ctx) []health.Issue` (implements `health.Check`); `ConnectionIssueKey`.

- [ ] **Step 1: Write the failing tests** (`dispatcher_test.go`, controlled clock and a fake sleep through `unattended.Rhythm{Sleep: …}` via `WithRhythm`, add that option):
1. GIVEN two due deliveries for one Telegram-paced connection (fake connector implementing `Pacer` with 3 s) WHEN the dispatcher runs one round THEN both are sent, in order, with a 3 s wait between them, and marked sent.
2. GIVEN the connector fails with a plain error THEN the delivery is retried with `next_at` = now + 1 min, then 5 min, 30 min, 2 h, 6 h; after the 6th failure it is `failed`.
3. GIVEN the connector fails with `ErrPermanent` THEN the delivery is `failed`, the connection is paused with the error's text (saved), and its other queued deliveries stay queued (sent after unpausing).
4. GIVEN a delivery created 25 h ago THEN it is `dropped`, not sent.
5. GIVEN a connection deleted after queueing THEN its deliveries are dropped.
6. GIVEN sent deliveries 8 days old THEN `Prune` removes them at the round's end.
7. The loop: `Run` returns when the context ends; a `wake()` makes it look at once; otherwise it looks every 15 s and calls `FlushBatches` each time.

`health_test.go`: a paused connection yields one `health.Issue{Key: "connector:<id>", Level: error, Kind: KindConnector, Source: <name>, Reason: ReasonConnectorPaused, Params: {"reason": <why>}, Since: <pausedAt>, Target: TargetNotifications, TargetID: <id>}`; a working one yields none.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `dispatcher.go`**

```go
// retryAfter is the wait after each failed attempt; after the last, the delivery is given up.
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

const (
	// tick is how often the dispatcher looks at the queue without being woken.
	tick = 15 * time.Second

	// maxAge is how old a delivery may be when its turn comes.
	maxAge = 24 * time.Hour

	// keepFinished is how long sent, failed and dropped deliveries are listed.
	keepFinished = 7 * 24 * time.Hour

	// defaultGap is the time between two messages of a connection whose connector has no Pacer.
	defaultGap = time.Second

	// sendTimeout bounds one send.
	sendTimeout = 30 * time.Second
)

// Run sends due deliveries until ctx ends: when woken by a new delivery, and every tick.
func (s *Service) Run(ctx context.Context) {
	for {
		s.FlushBatches(ctx)
		s.round(ctx)

		select {
		case <-ctx.Done():
			return
		case <-s.woken:
		case <-time.After(tick):
		}
	}
}
```

`round(ctx)`: `Due(now)`; group by connection preserving order; for each connection: `Get` (not found → `MarkDropped` each); paused → skip (stay queued); per delivery: older than `maxAge` → `MarkDropped`; render (`Render(ctx, d.Event, c.Language(), address, …)`); `Send` with a `sendTimeout` context; outcome as in the tests (`attempt := d.Attempts + 1; if attempt > len(retryAfter) → MarkFailed else MarkRetry(next = now + retryAfter[attempt-1])`); permanent → `MarkFailed`, `c.Pause(errText, now)`, `conns.Save`, stop that connection's round; between two sends `s.rhythm.Wait(ctx, gap)` where gap is the `Pacer`'s or `defaultGap`. End of round: `Prune(now - keepFinished)`. Logging: `s.log.Warn("sending a notification", "connection", c.Name(), "error", err)` — never settings.

- [ ] **Step 4: Implement `health.go`** — `Issues` lists connections (log and return nil on error) and builds the issue for each paused one.

- [ ] **Step 5: Server and web Health parity**: add the reason/kind/target constants; update `web/src/lib/health.ts` and both locales; run `go test ./internal/application/health/` (the parity test) and `node --test web/tests/health.test.mjs`.

- [ ] **Step 6: Run all** → `task test` PASS; `task lint`.

- [ ] **Step 7: Commit**

```bash
git add internal/application/notify internal/application/health web/src/lib/health.ts web/src/i18n web/tests/health.test.mjs
git commit -m "Notify: the dispatcher (pace, retries, pausing on a permanent failure) and paused connections in Health"
```

---

### Task 5: Health watcher and the daily job

**Files:**
- Create: `internal/application/notify/watcher.go`, `internal/application/notify/daily.go`, `watcher_test.go`, `daily_test.go`, `webparity_test.go`

**Interfaces:**
- Consumes: `Issues` (Health), `IssueMemory`, `KeyMemory`, `Catalog`, `Publisher`.
- Produces: `NewWatcher(issues Issues, memory IssueMemory, pub Publisher, opts ...WatcherOption) *Watcher` with `Run(ctx)` (every minute) and `Check(ctx)` (one comparison, for tests); `NewDaily(catalog Catalog, keys KeyMemory, releases ReleaseSource, pub Publisher, opts ...DailyOption) *Daily` with `Run(ctx)` and `RunOnce(ctx)`; `ExpiringDays = 30`, `KeyThresholds = []int{30, 7, 1}`; port `ReleaseSource interface { SeriesReleases(ctx) ([]game.Release, error) }` (home's stored "from your series" list: implement in Task 6 on `home.Service` as `SeriesReleases`, reading the stored snapshot without filters other than the catalog check); options `WithWatcherClock`, `WithDailyClock`, `WithDailyRhythm`, `WithDailyLogger`, `WithWatcherLogger`.

- [ ] **Step 1: Write the failing tests**

`watcher_test.go` (GWT, an in-memory memory and a fake `Issues` whose answer the test changes between `Check` calls):
1. GIVEN a first run with two current issues THEN nothing is published and both are remembered as notified (seeding).
2. GIVEN a seeded watcher WHEN a new issue appears in one check THEN nothing; WHEN it is still there in the next THEN one `issue.opened` with its fields.
3. GIVEN a notified issue WHEN it is missing once THEN nothing; missing twice THEN `issue.resolved`.
4. GIVEN an issue that flaps (present, absent, present) THEN nothing is published (Review Focus 4).
5. GIVEN a notified issue whose `Reason` changes (signed out → sync failed) THEN `issue.resolved` for the old is not sent; one `issue.opened` with the new reason (the key is the same: compare key+reason, treat a changed reason as a new issue after two checks).
6. A restart: a new `Watcher` on the same memory sees the notified issue still present → nothing.

`daily_test.go` (clock fixed to 2026-10-15 10:00 local):
1. Keys: copies with `RedeemBy` 2026-11-14 (30 days), 2026-11-15 (31), 2026-10-22 (7), 2026-10-16 (1), and a redeemed one dated 2026-10-20 → one `keys.expiring` with three keys nearest first (`DaysLeft` 1, 7, 30), and thresholds remembered: 30 for the 30-day one, 7 for the 7-day one, 1 for the 1-day one.
2. Next day, nothing changed → nothing published. The 31-day key is now 30 → published alone.
3. A key that entered at 30 and is now at 7 → told again (threshold 7 not yet told).
4. A key past its deadline (−2 days) never notified before → not told (the library's expired view covers it; the chip says so).
5. Releases: a series release dated today → `release.out` once; another run the same day → not again (the job keeps an in-memory set of the releases told, keyed by title and date; a restart the same day may tell one again, which is acceptable and documented in `docs/technical.md`).
6. `Run` waits until a random moment in 09:00–10:00 local each day (`rhythm.Between`).

`webparity_test.go`: read `web/src/lib/deadlines.ts`, regex `export const EXPIRING_DAYS = (\d+);`, assert equal to `ExpiringDays`.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `watcher.go`**

Keep in memory `seen map[string]int` (consecutive checks present, for not-yet-notified keys) and `missing map[string]int` (consecutive checks absent, for notified keys); the notified set comes from `IssueMemory` (loaded once, saved after each change). Convert `health.Issue` to `event.Issue` field by field. Publish `event.New(event.KindIssueOpened, now, issue)` / `KindIssueResolved`.

- [ ] **Step 4: Implement `daily.go`**

Days left computed in local time: `deadline := time.ParseInLocation("2006-01-02", string(c.RedeemBy), time.Local)`; `days := int(deadline.Sub(midnight(now)).Hours() / 24)` with `midnight` = local 00:00 (round, as `daysUntil` in `deadlines.ts`). A key qualifies when `IsPendingKey()` and `0 <= days <= ExpiringDays`. Threshold for a days value: the smallest of `KeyThresholds` that is `>= days` (30 for 8–30, 7 for 2–7, 1 for 0–1). Tell the key when that threshold is not in its remembered list. After telling, `RememberKey`; then `ForgetKeys(keep = every pending key copy id)`.

- [ ] **Step 5: Run** → PASS; `task lint`.

- [ ] **Step 6: Commit**

```bash
git add internal/application/notify
git commit -m "Notify: Health problems as they open and close, expiring keys and releases out today, once each"
```

---

### Task 6: Use cases publish their events

**Files:**
- Modify: `internal/application/sync/service.go` (+ test), `internal/application/catalog/options.go`, `service.go`, `scanned.go` (+ test), `internal/application/transfer/service.go` (+ test), `internal/application/home/options.go`, `service.go` (+ test, and `SeriesReleases`), `internal/application/auth/service.go` (+ test).

**Interfaces:**
- Consumes: `notify.Publisher`, `notify.Nop`, `event` payloads.
- Produces: `sync.WithEvents(p notify.Publisher) Option`, `catalog.WithEvents`, `transfer.WithEvents`, `home.WithEvents`, `auth.WithEvents` (auth: change `NewService(repo, settings, hash, certs, now, log)` to `NewService(repo, settings, hash, certs, now, log, opts ...Option)` with `type Option func(*Service)`); `(*home.Service).SeriesReleases(ctx) ([]game.Release, error)`.

**Import note:** `notify` imports `health` and `media`; `sync`, `catalog`, `transfer`, `home` and `auth` importing `notify` must not create a cycle: `notify` must not import any of them (it does not; the `Catalog` and `ReleaseSource` ports are its own). Verify with `go build ./...`.

- [ ] **Step 1: Write the failing tests** (a recording publisher `type recorder struct{ events []event.Event }` in each package's test files):
- sync: a scan that creates 2 games publishes one `collection.added` with `OriginSource`, `OriginName` = the source's name, both games (id, title, systems) and `CopiesAdded` = copies added to existing games; a scan that creates none publishes nothing; a failed scan publishes nothing. The games created by a scan must be known: extend the consolidation result the scan already gets (`catalog.Service.Consolidate` returns a result with `AddTo(report)`) with the created games' ids (read `internal/application/catalog/consolidate.go` and add `Created []game.ID` to its result type, filled where games are created), then load them for titles/systems.
- catalog: `CreateGame` publishes `collection.added` with `OriginManual` and the game; `AddScannedCopies` publishes one with the games it created (none when it only added copies to existing games, but `CopiesAdded` counts those).
- transfer: `Import` that creates games publishes one `OriginImport` event.
- home: a refresh whose new releases snapshot has a series release not in the previous one publishes `release.announced`; the first gathering (no previous snapshot, or the previous one never fetched) publishes nothing; a deals refresh publishes `deal.historical_low` for a historical low (`Deal.HistoricalLow`) of a game not owned on PC (reuse `unowned`) that was not in the previous snapshot's lows (compare by `ProviderGameID`).
- auth: two failed logins from 192.0.2.10 publish nothing; the third publishes `auth.failed_logins` with count 3; a fourth in the same hour publishes nothing; after the hour, three more publish again; failures from another address count separately. (Use the documentation range 192.0.2.0/24 in tests.)

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement**, publishing after the change is saved:
- `sync.Sync`: after `recordScan` succeeds and `report.GamesCreated > 0`.
- `catalog.CreateGame`: after the transaction commits; `AddScannedCopies`: after its transaction, one event for the call.
- `transfer.Import`: after consolidation.
- `home.refresh`: before `SaveSnapshot`, read the previous with `s.snaps.Snapshot(ctx, b)`; publish only when the previous `FetchedAt` is not zero.
- `auth.Login`: on failure, `s.failedLogins.record(ip, now)` (a small per-address window map under a mutex); publish when it returns 3.
- Each NewService doc comment's "Options:" list gains the new option.

- [ ] **Step 4: Run** → `task test` PASS; `task lint`.

- [ ] **Step 5: Commit**

```bash
git add internal/application
git commit -m "Use cases publish their events: new games by scan, by hand and by import, releases and lows, failed sign-ins"
```

---

### Task 7: Connectors as plugin pieces, wiring and background jobs

**Files:**
- Modify: `internal/application/plugin/plugin.go` (field `Connectors []notify.Connector`, accessor `(*Registry).Connectors() []notify.Connector`, duplicate type check in `NewRegistry`), `internal/application/plugin/plugintest/plugintest.go` (count connectors in "adds nothing"; check each connector: non-empty type and name, fields with label keys, secret fields marked), its test.
- Modify: `internal/app/app.go`: `Plugins` interface gains `Connectors() []notify.Connector`; `noPlugins` returns nil; build `notifySvc := notify.NewService(sqlite.NewConnectionRepository(db), store, store, games, assets, notify.WithConnectors(o.plugins.Connectors()...), notify.WithClock(now), notify.WithLogger(log), notify.WithSending(o.sending))` **before** catalog, home, sync, transfer and auth, and pass `WithEvents(notifySvc)` to each; add `notifySvc` to `health.NewService(...)`; after `healthSvc`, `watcher := notify.NewWatcher(healthSvc, store, notifySvc, …)` and `daily := notify.NewDaily(games, store, homeSvc, notifySvc, …)`; `App` gains `Notify *notify.Service`, `NotifyWatcher *notify.Watcher`, `NotifyDaily *notify.Daily`; new option `WithSending(on bool) Option` (default false, so the rpc test harness never sends).
- Modify: `cmd/gamevault/main.go`: `app.WithSending(!cfg.NoUnattended)`; in the `else` branch start `go vault.Notify.Run(ctx)`, `go vault.NotifyWatcher.Run(ctx)`, `go vault.NotifyDaily.Run(ctx)`; update the `-no-unattended` warning text to add "and notifications". Do **not** edit `internal/config/config.go` (permission-blocked); note it in the report.

- [ ] **Step 1: Failing tests**: plugintest rejects a connector with an empty type, and a plugin with only connectors is accepted; `TestBuild_noSendingByDefault` in `internal/app` (or the rpc harness): with default options, publishing an event queues nothing.

- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4:** `task test`, `task lint` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/application/plugin internal/app cmd/gamevault
git commit -m "Connectors are plugin pieces; notifications are wired and run unless -no-unattended"
```

---

### Task 8: Telegram connector

**Files:**
- Create: `internal/adapters/outbound/telegram/connector.go`, `plugin.go`, `connector_test.go`, `fake_test.go`
- Modify: `cmd/gamevault/plugins.go` (a `// Notifications` group: `telegram.Plugin(), ntfy.Plugin(), webhook.Plugin()` — add the other two in their tasks).

**Interfaces:**
- Produces: `telegram.Plugin() plugin.Plugin` (`ID: "telegram"`, `Connectors: []notify.Connector{NewConnector()}`); `type Connector struct { API *apiclient.Client }`; `NewConnector() *Connector` (base `https://api.telegram.org`, timeout 20 s, `NoRedirects`); settings keys `token` (secret, required, label `notifications.telegram.token`, help `notifications.telegram.tokenHelp`, help URL `https://t.me/BotFather`) and `chat` (text, required, label `notifications.telegram.chat`, help `notifications.telegram.chatHelp`); `Gap() = 3 * time.Second`; `FindChat`.

Telegram Bot API facts to code against (from core.telegram.org/bots/api; record in `.claude/memory/telegram.md` in Task 12 after probing):
- Requests: `POST https://api.telegram.org/bot<token>/<method>`; answers `{"ok":true,"result":…}` or `{"ok":false,"error_code":N,"description":"…","parameters":{"retry_after":S}}`.
- `sendMessage` JSON `{chat_id, text, parse_mode:"HTML", link_preview_options:{is_disabled:true}}`; text ≤ 4096.
- `sendPhoto` multipart (`chat_id`, `photo` file or URL string, `caption` ≤ 1024, `parse_mode`).
- `sendMediaGroup` multipart: `media` JSON array of `{"type":"photo","media":"attach://p0"|<url>,"caption":…,"parse_mode":"HTML"}` (caption on the first) plus parts `p0`, `p1`; 2–10 items.
- `getUpdates` → `result[].message.chat.id` (take the last update's).
- Errors: 401 Unauthorized (bad token), 400 "Bad Request: chat not found", 403 "Forbidden: bot was blocked by the user", 429 with `parameters.retry_after`.

Rules: one image → `sendPhoto`; two → `sendMediaGroup`; none → `sendMessage`. Text = `<b>Title</b>\n` + HTML-escaped text + (when `Link` is set: `\n<a href="Link">LinkLabel</a>`; the service fills `LinkLabel` with `open` for Game Vault links and `open.page` for public pages). Caption cut at 1024 runes: cut whole lines and append the `more` text with the remaining count is the service's job for lists; the connector only hard-cuts at 1024 with "…" as a last resort. Errors: 401/400/403/404 → `ErrPermanent` wrapped with Telegram's `description`; 429 → an error with `RetryAfter()`; 5xx/transport → plain error. **Redaction:** every returned error passes through `redact(err, token)` which replaces the token with `…` in its text (`fmt.Errorf("%s", strings.ReplaceAll(err.Error(), token, "…"))` keeping `errors.Is` for `ErrPermanent` via a wrapper type).

- [ ] **Step 1: Failing tests** against a fake server (`fake_test.go` reproduces the shapes above): text only, one photo (multipart contains the bytes and the caption), two photos (`sendMediaGroup` with `attach://` and URLs), 401 → `errors.Is(err, notify.ErrPermanent)`, 400 chat not found → permanent, 429 → not permanent and `RetryAfter` 7 s, 500 → transient, **no error text contains the token** (Review Focus 2), `FindChat` returns the last update's chat id, `Test` sends a message.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4:** PASS, `task lint`.
- [ ] **Step 5: Commit** `git commit -m "Telegram connector: messages, photos and albums through a bot, the chat found from the bot's updates"`

---

### Task 9: ntfy connector

**Files:** `internal/adapters/outbound/ntfy/connector.go`, `plugin.go`, tests; `cmd/gamevault/plugins.go`.

**Interfaces:** `ntfy.Plugin()` (`ID: "ntfy"`); settings `server` (text, default shown in help: `https://ntfy.sh`; empty means it), `topic` (text, required), `token` (secret, optional; sent as `Authorization: Bearer`). Publishing (docs.ntfy.sh/publish): `PUT <server>/<topic>` with headers `Title`, `Priority` (`high` when `Important`, else `default`), `Click` (link), `Tags`; body = the message text; with a local image: body = image bytes, headers `Filename`, `Message` (text, newlines as `\n` — ntfy accepts `X-Message`/`Message` header; if header length is a concern, send the text in `Message` and cut at 4096 bytes); with a URL image: header `Attach: <url>`. Errors: 401/403/404 → permanent; 413 (attachment too large) → resend without the image once; 429/5xx → transient. `Test` → a plain message. `Gap()` default (1 s).

- [ ] Steps as Task 8 (fake server asserting method, path, headers and body; errors). Commit: `"ntfy connector: push messages with the cover attached"`.

---

### Task 10: Webhook connector

**Files:** `internal/adapters/outbound/webhook/connector.go`, `plugin.go`, tests; `cmd/gamevault/plugins.go`.

**Interfaces:** `webhook.Plugin()` (`ID: "webhook"`); settings `url` (text, required, http/https only — validate in `Send`/`Test`: `ErrPermanent` for another scheme), `header` (text, optional: a header name), `secret` (secret, optional: its value). `POST` JSON:

```json
{"version":1,"event":"collection.added","at":"2026-10-15T10:00:00Z",
 "data":{"origin":"source","originName":"Example Store","games":[{"id":"…","title":"Example Game","systems":["PC"]}],"copiesAdded":0},
 "message":{"title":"…","text":"…","link":"…"},
 "images":["https://…"]}
```

`data` is the event payload as the codec of Task 3 encodes it (the payload structs carry lower-camel `json` tags from Task 1); `images` lists only URL images. 2xx = sent; 4xx except 408/429 → permanent; 3xx (redirect refused) → permanent with "the webhook answered with a redirect: use its final address"; others → transient. `Test` sends `{"version":1,"event":"test",…}`.

- [ ] Steps as Task 8. Commit: `"Webhook connector: a stable JSON document per event"`.

---

### Task 11: API

**Files:**
- Create: `proto/gamevault/v1/notify.proto`, `internal/adapters/inbound/rpc/notify_handler.go`, `notify_mapper.go`, `notify_handler_test.go`
- Modify: `internal/adapters/inbound/rpc/server.go` (`Notifications *NotificationHandler` + `mux.Handle(gamevaultv1connect.NewNotificationServiceHandler(h.Notifications, ic))`), `internal/app/app.go` (Handlers entry), `web/src/api/client.ts` (`notificationClient`).

**Interfaces:** proto (comment every message and field; reuse `SettingField` from `gamevault/v1/source.proto`):

```proto
service NotificationService {
  rpc ListConnectorTypes(ListConnectorTypesRequest) returns (ListConnectorTypesResponse);
  rpc ListConnections(ListConnectionsRequest) returns (ListConnectionsResponse);
  rpc SaveConnection(SaveConnectionRequest) returns (SaveConnectionResponse);
  rpc DeleteConnection(DeleteConnectionRequest) returns (DeleteConnectionResponse);
  rpc TestConnection(TestConnectionRequest) returns (TestConnectionResponse);
  rpc ListDeliveries(ListDeliveriesRequest) returns (ListDeliveriesResponse);
  rpc GetNotificationSettings(GetNotificationSettingsRequest) returns (GetNotificationSettingsResponse);
  rpc SaveNotificationSettings(SaveNotificationSettingsRequest) returns (SaveNotificationSettingsResponse);
}
message ConnectorType { string id = 1; string name = 2; string description_key = 3; repeated SettingField fields = 4; }
message EventKind { string kind = 1; string group = 2; }
message ListConnectorTypesResponse { repeated ConnectorType types = 1; repeated EventKind events = 2; repeated string languages = 3; string secret_placeholder = 4; }
message Connection { string id = 1; string type = 2; string name = 3; bool enabled = 4; repeated string events = 5; string language = 6; map<string, string> settings = 7; string paused_reason = 8; google.protobuf.Timestamp paused_at = 9; }
message ConnectionInput { string type = 1; string name = 2; bool enabled = 3; repeated string events = 4; string language = 5; map<string, string> settings = 6; }
message SaveConnectionRequest { string id = 1; ConnectionInput connection = 2; }
message TestConnectionRequest { string id = 1; ConnectionInput connection = 2; }
message TestConnectionResponse { bool success = 1; string message = 2; map<string, string> found = 3; }
message Delivery { string id = 1; string event = 2; string state = 3; int32 attempts = 4; string last_error = 5; google.protobuf.Timestamp created_at = 6; google.protobuf.Timestamp updated_at = 7; string summary = 8; }
message ListDeliveriesRequest { string connection_id = 1; }
message GetNotificationSettingsResponse { string address = 1; }
message SaveNotificationSettingsRequest { string address = 1; }
```

(plus the empty request/response messages buf STANDARD needs). `summary` is the delivery's title rendered in the connection's language. `TestConnection` mirrors `TestSource`: a failure is `success=false` with the error text, not an RPC error.

- [ ] **Step 1:** write the proto; `task generate`.
- [ ] **Step 2: Failing tests** (`notify_handler_test.go` through the rpc harness): list types includes the three connectors and the eight events; save → list (secret masked) → test (fake connector) → deliveries → delete; an access token is refused (`Unauthenticated`/`PermissionDenied` as the harness asserts for other services).
- [ ] **Step 3: Implement** handler + mapper (`connectionToPB` masks with the descriptor; errors through `toConnectError`). Add `notify` sentinels to `TestSentinels_haveACode` if it lists application sentinels.
- [ ] **Step 4:** `task test`, `task lint` PASS.
- [ ] **Step 5: Commit** `git commit -m "API: NotificationService"` (with the generated code).

---

### Task 12: Web — Settings → Notifications

**Files:**
- Create: `web/src/features/notifications/NotificationsPage.tsx`, `ConnectionDialog.tsx`, `Deliveries.tsx`, `web/src/lib/notifications.ts`, `web/tests/notifications.test.mjs`
- Modify: `web/src/lib/route.ts` (`'notifications'` in `PAGES` after `'providers'`), `web/tests/route.test.mjs`, `web/src/App.tsx` (`SETTINGS` entry `{ route: 'notifications', icon: 'bell' }` and the render line), `web/src/components/Icon.tsx` (`bell: 'M6 10a6 6 0 1 1 12 0c0 4 2 6 2 6H4s2-2 2-6M10 20a2 2 0 0 0 4 0'`), locales (`nav.notifications`, `notifications.*`).

**Interfaces:**
- `lib/notifications.ts` (pure, Node-tested): `groupEvents(events: {kind: string; group: string}[]): {group: string; kinds: string[]}[]` (catalog order, groups activity → attention → security); `toggleGroup(selected: string[], kinds: string[], on: boolean): string[]`; `connectionState(c: {enabled: boolean; pausedReason: string}, lastDelivery?: {state: string}): 'working' | 'paused' | 'off' | 'untested'`.

Page: the address field (save on blur/enter, error shown inline), the connections list (icon by type, name, type name, a switch calling `saveConnection` with `enabled` flipped, a state pill, "N events"), "Add connection" → a type picker (three cards) → `ConnectionDialog`. The dialog follows `SourceDialog.tsx`: name, `SettingInputs` with the type's fields, a language select (`connection.languages`, default `i18n.language` when it is one of them, else `en`), the events in groups with a checkbox per kind (label `t('notifications.events.<kind>')`, hint `t('notifications.eventsHint.<kind>')`) and an "All" checkbox per group, `TestButton` (`useSettingsTest` over `notificationClient.testConnection`; when the answer has `found`, fill those settings and show "Chat found: <id>"), Save, Delete (confirm). `Deliveries` lists the last 20 under the dialog's form when editing (`state` pill, summary, relative time, `lastError`).

i18n keys (en; es equivalents): `nav.notifications` "Notifications"; `notifications.title`, `.intro` ("Send what happens in Game Vault to Telegram, ntfy or any web address."), `.address` ("Game Vault address"), `.addressHelp` ("Optional. The address you open Game Vault at, such as http://192.168.1.10:8080: messages then link to the game."), `.add`, `.none` ("No connections yet."), `.events.<eight kinds>`, `.eventsHint.<eight kinds>`, `.groups.activity|attention|security`, `.all`, `.language`, `.state.working|paused|off|untested`, `.eventCount_one|_other`, `.deliveries`, `.delivery.queued|sent|failed|dropped`, `.chatFound`, `.deleteConfirm`, plus per connector `notifications.telegram.token`, `.tokenHelp` (numbered steps: 1. Open @BotFather in Telegram and send /newbot. 2. Copy the token it gives you. 3. Send any message to your new bot. 4. Leave the chat empty and press Test: Game Vault finds it), `.chat`, `.chatHelp`, `notifications.ntfy.server`, `.serverHelp`, `.topic`, `.topicHelp`, `.token`, `.tokenHelp`, `notifications.webhook.url`, `.urlHelp`, `.header`, `.headerHelp`, `.secret`, `.secretHelp`, and the descriptors' `DescriptionKey`s.

- [ ] **Step 1: Failing Node tests** for `lib/notifications.ts`; route test with the new page.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4:** `task test` (tsc, node tests, i18n) and `task lint` (oxlint hooks) PASS.
- [ ] **Step 5: Commit** `git commit -m "Settings → Notifications: connections, their events and recent deliveries"`

---

### Task 13: Docs, probes and the visual check

**Files:** `docs/technical.md` (a "Notifications" section: events and their rules, delivery, connectors, `-no-unattended`), `docs/plugins.md` (a "Connectors" section: the `notify.Connector` port, `ErrPermanent`, `Pacer`, `ChatFinder`, a test with a fake server), `README.md` (one line in the features: "Notifications to Telegram, ntfy or any webhook"), `.claude/memory/telegram.md`, `.claude/memory/ntfy.md`, `.claude/MEMORY.md` (two index lines), `.claude/docs/ui.md` (the page in the features list).

- [ ] **Step 1: Probes** (bogus credentials only):
  - `curl -s -X POST https://api.telegram.org/bot123456:NOT-A-REAL-TOKEN/getMe` → expect HTTP 401 `{"ok":false,"error_code":401,"description":"Unauthorized"}`; record the exact answer in `telegram.md` (dated).
  - `curl -s https://ntfy.sh/v1/health` → `{"healthy":true}`; never publish to a topic.
- [ ] **Step 2: Docs** as listed.
- [ ] **Step 3: Visual check**: `task test-server`; open http://127.0.0.1:8093/?v=N#/notifications in the browser pane: add a Telegram connection with bogus values, press Test (expect the clean "Telegram rejected the bot token" error), pick events by group, save, see it listed; desktop then the mobile preset; reset; `task test-server:stop`. The test server runs with `-no-unattended`, so nothing is queued; Test is the only send, to Telegram with a bogus token.
- [ ] **Step 4:** `task lint`, `task test`, `task test:race` PASS.
- [ ] **Step 5: Commit** `git commit -m "Docs: notifications, connectors and the Telegram and ntfy notes"`
