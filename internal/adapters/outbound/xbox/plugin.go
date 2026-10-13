package xbox

import (
	"log/slog"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Xbox plugin: the library of Microsoft accounts and the Microsoft Store's art.
func Plugin(log *slog.Logger) plugin.Plugin {
	return plugin.Plugin{
		ID:      "xbox",
		Name:    "Xbox",
		Sources: []sync.Provider{NewProvider(WithLogger(log))},
		Covers:  []media.CoverProvider{NewCovers()},
	}
}
