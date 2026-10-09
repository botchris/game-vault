package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"gamevault/internal/application/plugin/plugintest"
)

// TestPlugins checks every compiled-in plugin, so a new one is checked as soon as it is listed in
// newPlugins.
func TestPlugins(t *testing.T) {
	t.Run("GIVEN the plugins compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err, "plugin, source type and provider ids must be unique")

		tr, err := plugintest.LoadTranslations(".")
		require.NoError(t, err)

		for _, p := range plugins.Plugins() {
			t.Run("THEN "+p.ID+" is complete", func(t *testing.T) {
				plugintest.Check(t, p, tr)
			})
		}
	})
}
