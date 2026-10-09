package ubisoft

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Ubisoft Connect plugin: the library of Ubisoft accounts and Ubisoft's box art.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:      "ubisoft",
		Name:    "Ubisoft Connect",
		Sources: []sync.Provider{NewProvider()},
		Covers:  []media.CoverProvider{NewCovers()},
	}
}
