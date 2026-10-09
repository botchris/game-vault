package gog

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the GOG plugin: the library of GOG accounts and the store's box art.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:      "gog",
		Name:    "GOG",
		Sources: []sync.Provider{NewProvider()},
		Covers:  []media.CoverProvider{NewCovers()},
	}
}
