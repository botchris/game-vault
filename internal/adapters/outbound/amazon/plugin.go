package amazon

import (
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Amazon Games plugin: the library of Amazon Games / Prime Gaming accounts.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:      "amazon",
		Name:    "Amazon Games",
		Sources: []sync.Provider{NewProvider()},
	}
}
