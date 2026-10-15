# Notifications: system events sent through connectors

Status: design for review. Branch `feature/notifications`.

## Why

Game Vault notices things the user would want to know without opening it: games that just entered
the collection, a new entry in a series they own, a source that signed out, a provider whose key
was rejected, keys about to expire. Today those only show up in the UI (System → Health, the home
page). Notifications send them out, the way media managers do with their "connections": a set of
pre-built integrations sharing one interface, each subscribed to the events the user picks.

## Decisions

- **Activity and attention, chosen per connector.** One event catalog covers both what happened
  (new games, releases, deals) and what needs the user (problems, deadlines, failed sign-ins); each
  configured connector subscribes to its own list.
- **One message per batch, never per game.** A source scan or a CSV import that created games sends
  one message: the source, the list of titles and a couple of covers. Games added by hand (Scan, the
  new game form) are one batch until 10 minutes pass without another.
- **First connectors: Telegram, a generic webhook, ntfy.** Email and Discord later.
- **Hybrid event architecture.** Attention events are derived from System → Health (what the user
  is told is what Health shows); activity and security events are published explicitly by their use
  case; expiring keys come from a daily job. Everything goes through one durable queue and one
  dispatcher.
- **Keys expiring:** the library's rule, 30 days.
- **Test copies never send.** With `-no-unattended` nothing is queued or delivered; only the
  user-pressed Test sends.

## Event catalog (v1)

Event kinds are stored in subscriptions and in the queue, so their names never change.

| Kind | Group | When | Content |
| --- | --- | --- | --- |
| `collection.added` | activity | a source scan or a CSV import created games; games added by hand, 10 minutes after the last one | origin (the source, "Added by hand" or "CSV import"), the new games' titles, systems and ids, how many copies were added to games already owned, up to two covers |
| `release.announced` | activity | a release from the user's series appears that was not in the previous releases snapshot | title, date, platforms, the owned game that explains it |
| `release.out` | activity | a release from the user's series comes out today (daily job) | title, platforms, the owned game |
| `deal.historical_low` | activity | a game the user does not own on PC enters the historical lows (not in the previous deals snapshot) | title, price, regular price, cut, shop, ITAD's link (unchanged) |
| `issue.opened` | attention | a Health issue appears and holds for two checks | Health's issue: key, level, reason, params, target |
| `issue.resolved` | attention | a notified issue is gone for two checks | the same issue |
| `keys.expiring` | attention | daily: keys that entered the 30-day window, or reached 7 or 1 day | the keys (title, store, deadline, days left), nearest first |
| `auth.failed_logins` | security | 3 or more failed sign-ins from one address within an hour | address, count, first and last attempt |

Health's issues cover: a source signed out or failing, a provider whose key is rejected, whose quota
is used up or which is unavailable, a failed or stale backup, low disk space, and (new: Health kind
`connector`, reason `connector_paused`, target the Notifications page) a connector paused by a
permanent failure. The first gathering of deals or releases after a provider is set up
seeds the snapshot silently: no `release.announced` or `deal.historical_low` for it.

## Architecture

### Domain

- **`internal/domain/event`**: `Event` (`ID`, `Kind`, `At`, and a typed payload per kind:
  `CollectionAdded`, `ReleaseAnnounced`, `ReleaseOut`, `HistoricalLow`, `IssueChanged`,
  `KeysExpiring`, `FailedLogins`), `Kind` constants, `Group` (activity, attention, security) and
  `Kinds()` (the catalog, in display order). Payloads hold plain data (titles, ids, systems, dates,
  money), never ports or adapter types, and are serialised to JSON for the queue.
- **`internal/domain/connection`**: `Connection` (`ID`, `Name`, `Type` (a connector type id),
  `Enabled`, `Events []event.Kind`, `Language`, `Settings schema.Settings`, `Paused` with its reason
  and time) and `TypeDescriptor` (id, name, description key, `schema.Fields`), with the same secret
  handling as sources (`schema.FieldSecret`, `Merge`, `Masked`).

### Application: `internal/application/notify`

- **Port `Publisher`** with `Publish(ctx, event.Event)`. Use cases take it through an option
  (`sync.WithEvents`, `catalog.WithEvents`, `transfer.WithEvents`, `home.WithEvents`,
  `auth.WithEvents`) whose default does nothing, so existing tests and callers are untouched.
  Publishing happens after the change it reports is saved.
- **Port `Connector`** (the plugin kind): `Descriptor() connection.TypeDescriptor`,
  `Send(ctx, settings, Message) error`, `Test(ctx, settings) error`. A connector may implement the
  optional `ChatFinder` (Telegram: find the chat of the last message sent to the bot).
- **`Message`**: what a connector sends, built by the service from an event: `Title`, `Text`,
  `Level` (info or important), `Link` (empty without a Game Vault address), `Images` (up to two:
  either bytes with a content type, for the collection's covers, or a public URL, for a deal's or a
  release's art, which Telegram and ntfy fetch themselves), and the `Event` itself (the webhook sends
  its data). Connectors only format it for their service.
- **`Service`**:
  - `Publish`: finds the enabled, unpaused connections subscribed to the kind and queues one
    delivery per connection. Does nothing under `-no-unattended`.
  - Rendering: event → `Message` in the connection's language, from a small catalog of texts in Go
    (en, es; English when a text is missing; a test requires the same keys in every language).
    Covers come from the local game data (`config/game-data`, through the asset store's
    `GetCover`, which never reaches the network) as bytes; a game without a cached cover is sent
    without one. The language is the connection's own: the web creates a connection with the UI's
    language (the UI language lives in the browser, not on the server).
  - CRUD for connections, `Test(type, settings)` (sends a sample message built from a fixed
    example event, with unsaved settings), deliveries listing, the Game Vault address.
- **Health watcher** (job, every minute; a component of its own, built after Health, since the use
  cases Health reads are built with the publisher): reads `health.Service.Issues`, keyed by `Issue.Key`;
  publishes `issue.opened` after an issue is seen in two consecutive checks and `issue.resolved`
  after a notified issue is missing from two. The notified set is stored (`notified_issues`), so a
  restart neither repeats nor loses one. On its first run (fresh install or upgrade) the current
  issues are recorded as notified without sending. A connection's own pause issue is never
  delivered to that connection.
- **Daily job** (once a day at a random time in a morning window, local time, jittered like other
  unattended work): `keys.expiring` (pending keys, the library's rule: `RedeemBy` within 30 days,
  notified on entering the window and again at 7 and 1 day; the thresholds already notified per
  copy are stored) and `release.out` (series releases dated today, once per release).
- **Dispatcher**: sends queued deliveries, one connection at a time in queue order, at the
  connector's pace (Telegram one message every 3 s per connection; ntfy and webhook one a second).
  - Transient failures (no answer, 5xx, 429 with `Retry-After`): retried after 1 min, 5 min, 30
    min, 2 h and 6 h, then given up.
  - Permanent failures (rejected token, unknown chat or topic: 400, 401, 403, 404): the connection
    is paused with the reason, which Health shows; the user unpauses it by saving or testing it.
  - Deliveries older than 24 h when their turn comes are dropped, not sent.
  - Sent deliveries are kept 7 days; each connection lists its last 20 (sent, retrying, failed,
    dropped, with the reason).
- **Events published by use cases:**
  - `sync`: after a scan is recorded, when it created games (`collection.added`).
  - `catalog`: each game created by hand (Scan, the new game form) is held by the service in a
    pending batch, published as one `collection.added` when 10 minutes pass without another (the
    batch is kept in memory: a restart in those minutes loses it).
  - `transfer`: after a CSV import (`collection.added`).
  - `home`: in `refresh`, before saving the snapshot, comparing with the previous one
    (`release.announced`, `deal.historical_low`; only deals left after the owned filter).
  - `auth`: failed sign-ins are counted per address in one-hour windows (in memory); the third in a
    window publishes `auth.failed_logins`, once per window. `auth.NewService` gains a variadic
    options parameter for it.

### Adapters

- **`telegram`**: Bot API (`https://api.telegram.org/bot<token>/…`). Settings: bot token (secret),
  chat id. `sendMediaGroup` with up to two photos and the text as the first one's caption (HTML,
  escaped; captions are cut to Telegram's 1024 characters with "… and N more"), `sendMessage`
  without images. `Test` sends a sample; `ChatFinder` reads `getUpdates` for the last chat that
  wrote to the bot. Errors: 401 bad token, 400 "chat not found", 429 with `parameters.retry_after`.
  The token is part of the URL path, so every error the adapter returns has it replaced by `…`.
- **`ntfy`**: settings: server (default `https://ntfy.sh`), topic, optional access token (secret).
  Publishes with the image as the body (`PUT` with `Filename`, `Title`, `Message`, `Priority`,
  `Click` headers) or as a plain message without one; important messages get high priority.
- **`webhook`**: settings: URL, optional secret header name and value (secret). `POST` of a stable
  JSON document: `{"version":1,"event":<kind>,"at":<RFC 3339>,"data":{…payload…},
  "message":{"title","text","link"}}`; images are not sent (only their URLs when a Game Vault
  address is set). Redirects are refused; 2xx is success.
- All three go through `apiclient` (timeouts, error classification, no redirects) with base URL
  fields for tests, and are plugins (`plugin.Plugin` gains `Connectors`).
- **SQLite** (migration 0013): `connections` (JSON doc like providers), `notification_outbox`
  (delivery id, connection id, event JSON, state, attempts, next attempt, last error, timestamps),
  `notified_issues` (issue key, issue JSON, since), `notified_keys` (copy id, threshold).

### API

`NotificationService` (`proto/gamevault/v1/notify.proto`): `ListConnectorTypes` (with the event
catalog and its groups), `ListConnections`, `SaveConnection`, `DeleteConnection`, `TestConnection`
(unsaved settings; may return a found chat id), `ListDeliveries`, `GetNotificationSettings` /
`SaveNotificationSettings` (the Game Vault address). Secrets come back masked. Signed-in users only;
extension access tokens are refused.

## UI

A new page, **Settings → Notifications** (`#/notifications`):
- The Game Vault address (optional), with a help line: messages link to games only when it is set.
- The connections: icon, name, type, an Enabled switch, the state (working, paused and why, not
  tested) and how many events it sends.
- **Add connection**: pick the type, then name, the type's fields (help as numbered steps, like
  sources), language, events grouped (Activity, Needs your attention, Security, with "all" per
  group), **Test** (with the unsaved settings), Save.
- A connection's detail lists its last 20 deliveries.
- Every string in en and es.

## Privacy and safety

- What is sent is what the user subscribed to, to services the user configured. Covers are sent as
  images; nothing else from the catalog leaves the server unless an event says so.
- Secrets (bot token, ntfy token, webhook secret) are stored like other secrets, masked in the API
  and never logged.
- `-no-unattended` (test servers on a copy of the config) sends nothing on its own.
- The webhook refuses redirects and only sends to the URL configured.

## Testing

- Each connector against a fake server reproducing the real API's shapes and failures (Telegram: 401
  bad token, 400 chat not found, 429 with `retry_after`, caption limits; ntfy: 403, 429, 5xx;
  webhook: non-2xx, no answer, redirect refused).
- The service with a controlled clock and random source: each event's rule, the watcher's two-check
  margin and first-run seeding, a connection never notified of its own pause, retries and giving
  up, pausing on a permanent failure, dropping old deliveries, `-no-unattended`.
- Use cases publish what they should (sync, catalog, transfer, home with the silent first
  gathering, auth windows).
- Web/server parity tests: the 30-day rule and the event catalog.
- A real-endpoint probe with bogus credentials (Telegram: a made-up token, expecting a clean 401;
  ntfy: `GET /v1/health` only, never a publish to a public topic).
- The page on a test server, desktop and phone.

## Out of scope

Email and Discord connectors, scheduled digests (daily or weekly summaries), browser push
notifications, per-event message templates.

## Delivery

Branch `feature/notifications`, one PR. Docs: `docs/technical.md` (events, rules, delivery,
connectors), `docs/plugins.md` (adding a connector), README (one line), memory files for Telegram
and ntfy.
