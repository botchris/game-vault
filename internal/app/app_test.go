package app_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/logfile"
	"gamevault/internal/adapters/outbound/passwordhash"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/app"
	"gamevault/internal/application/home"
	"gamevault/internal/application/notify"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/config"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
)

// chat is a notification connector that sends nowhere.
type chat struct{}

func (chat) Descriptor() connection.TypeDescriptor {
	return connection.TypeDescriptor{
		Type: "chat",
		Name: "Chat",
	}
}

func (chat) Send(context.Context, schema.Settings, notify.Message) error { return nil }
func (chat) Test(context.Context, schema.Settings, notify.Message) error { return nil }

// connectorsOnly are plugins with one notification connector and nothing else.
type connectorsOnly struct{}

func (connectorsOnly) Sources() []sync.Provider          { return nil }
func (connectorsOnly) Providers() []providers.Impl       { return nil }
func (connectorsOnly) Valuations() []valuation.Provider  { return nil }
func (connectorsOnly) Deals() []home.DealsProvider       { return nil }
func (connectorsOnly) Releases() []home.ReleasesProvider { return nil }
func (connectorsOnly) Connectors() []notify.Connector    { return []notify.Connector{chat{}} }

// build builds Game Vault on a database and folders in a temporary directory.
func build(ctx context.Context, t *testing.T, opts ...app.Option) *app.App {
	t.Helper()

	dir := t.TempDir()

	db, err := sqlite.Open(ctx, filepath.Join(dir, "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	level := new(slog.LevelVar)

	logFiles, err := logfile.Open(filepath.Join(dir, "logs"), level, settings.DefaultLogging())
	require.NoError(t, err)
	t.Cleanup(func() { logFiles.Close() })

	opts = append([]app.Option{
		app.WithPlugins(connectorsOnly{}),
		app.WithHasher(passwordhash.Bcrypt{Cost: 4}),
		app.WithDiskSpace(nil),
	}, opts...)

	vault, err := app.Build(ctx, config.Config{
		ConfigDir:  dir,
		BackupKeep: 3,
	}, db, logFiles, slog.New(slog.DiscardHandler), opts...)
	require.NoError(t, err)

	return vault
}

func TestBuild_noSendingByDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	issue := event.New(event.KindIssueOpened, time.Now(), event.Issue{
		Key:     "source:demo",
		Level:   "error",
		Kind:    "source",
		Source:  "Demo",
		Reason:  "signed_out",
		Message: "signed out",
	})
	subscribe := func(t *testing.T, vault *app.App) connection.ID {
		t.Helper()

		c, err := vault.Notify.Save(ctx, "", "chat", connection.Config{
			Name:     "Phone",
			Enabled:  true,
			Events:   []event.Kind{event.KindIssueOpened},
			Language: "en",
		})
		require.NoError(t, err)

		return c.ID()
	}

	t.Run("GIVEN Game Vault built with the default options and a connection subscribed to issues", func(t *testing.T) {
		vault := build(ctx, t)
		id := subscribe(t, vault)

		t.Run("WHEN an issue is published", func(t *testing.T) {
			vault.Notify.Publish(ctx, issue)

			t.Run("THEN nothing is queued: tests and copies of the config never send", func(t *testing.T) {
				ds, err := vault.Notify.Deliveries(ctx, id, 20)
				require.NoError(t, err)
				assert.Empty(t, ds)
			})
		})
	})

	t.Run("GIVEN Game Vault built with sending on and the same connection", func(t *testing.T) {
		vault := build(ctx, t, app.WithSending(true))
		id := subscribe(t, vault)

		t.Run("WHEN an issue is published", func(t *testing.T) {
			vault.Notify.Publish(ctx, issue)

			t.Run("THEN it is queued for the connection", func(t *testing.T) {
				ds, err := vault.Notify.Deliveries(ctx, id, 20)
				require.NoError(t, err)
				assert.Len(t, ds, 1)
			})
		})
	})
}
