package humble

import (
	"log/slog"

	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Humble Bundle plugin: the keys in Humble Bundle orders.
func Plugin(log *slog.Logger) plugin.Plugin {
	return plugin.Plugin{
		ID:      "humble",
		Name:    "Humble Bundle",
		Sources: []sync.Provider{NewProvider(log)},
	}
}
