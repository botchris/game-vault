package eaapp

import (
	"log/slog"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the EA app plugin: the library of EA accounts and EA's pack art.
func Plugin(log *slog.Logger) plugin.Plugin {
	return plugin.Plugin{
		ID:      "eaapp",
		Name:    "EA app",
		Sources: []sync.Provider{NewProvider(WithLogger(log))},
		Covers:  []media.CoverProvider{NewCovers()},
	}
}
