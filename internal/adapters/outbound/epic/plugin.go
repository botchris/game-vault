package epic

import (
	"log/slog"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Epic Games plugin: the library of Epic accounts and the store's box art.
func Plugin(log *slog.Logger) plugin.Plugin {
	return plugin.Plugin{
		ID:      "epic",
		Name:    "Epic Games",
		Sources: []sync.Provider{NewProvider(WithLogger(log))},
		Covers:  []media.CoverProvider{NewCovers()},
	}
}
