package playstation

import (
	"log/slog"

	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the PlayStation plugin: the library of PlayStation accounts.
func Plugin(log *slog.Logger) plugin.Plugin {
	return plugin.Plugin{
		ID:      "playstation",
		Name:    "PlayStation",
		Sources: []sync.Provider{NewProvider(WithLogger(log))},
	}
}
