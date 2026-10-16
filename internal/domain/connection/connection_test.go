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
		{
			Key:      "token",
			LabelKey: "x",
			Kind:     schema.FieldSecret,
			Required: true,
		},
		{
			Key:      "chat",
			LabelKey: "y",
			Kind:     schema.FieldText,
		},
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

func TestConnection_subscribedAndSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	cfg := connection.Config{
		Events:   []event.Kind{event.KindIssueOpened},
		Language: "en",
		Settings: schema.Settings{"token": "t-123"},
	}

	t.Run("GIVEN a disabled connection THEN it is subscribed to nothing", func(t *testing.T) {
		c, err := connection.New(telegram, cfg, now)
		require.NoError(t, err)
		assert.False(t, c.Subscribed(event.KindIssueOpened))
		assert.Equal(t, "Telegram", c.Name(), "an empty name falls back to the type's")
	})

	t.Run("GIVEN a paused enabled connection THEN subscription ignores the pause", func(t *testing.T) {
		cfg.Enabled = true
		c, err := connection.New(telegram, cfg, now)
		require.NoError(t, err)
		c.Pause("bad token", now)
		assert.True(t, c.Subscribed(event.KindIssueOpened))
		assert.Equal(t, "bad token", c.PausedWhy())
		assert.Equal(t, now, c.PausedAt())

		c.Unpause()
		assert.False(t, c.Paused())
	})

	t.Run("GIVEN a connection WHEN it is rehydrated from its snapshot THEN nothing changes", func(t *testing.T) {
		c, err := connection.New(telegram, cfg, now)
		require.NoError(t, err)
		c.Pause("bad token", now)

		got := connection.Rehydrate(c.Snapshot())
		assert.Equal(t, c, got)
	})

	t.Run("GIVEN accessors that return slices and maps THEN callers cannot change the connection", func(t *testing.T) {
		c, err := connection.New(telegram, cfg, now)
		require.NoError(t, err)

		c.Events()[0] = event.KindKeysExpiring
		c.Settings()["token"] = "other"

		assert.Equal(t, []event.Kind{event.KindIssueOpened}, c.Events())
		assert.Equal(t, "t-123", c.Settings()["token"])
	})
}
