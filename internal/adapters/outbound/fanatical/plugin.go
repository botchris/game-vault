package fanatical

import (
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Fanatical plugin: the keys bought on Fanatical (behind a risk consent).
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID: "fanatical", Name: "Fanatical",
		Sources: []sync.Provider{NewProvider()},
	}
}
