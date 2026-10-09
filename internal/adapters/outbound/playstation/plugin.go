package playstation

import (
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the PlayStation plugin: the library of PlayStation accounts.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID: "playstation", Name: "PlayStation",
		Sources: []sync.Provider{NewProvider()},
	}
}
