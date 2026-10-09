package steam

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// Plugin is the Steam plugin: the library of Steam accounts, Steam's library art and the store
// page's details, all through store (also the store Battle.net's covers search).
func Plugin(store *Store) plugin.Plugin {
	return plugin.Plugin{
		ID:       "steam",
		Name:     "Steam",
		Sources:  []sync.Provider{NewProvider()},
		Covers:   []media.CoverProvider{store},
		Metadata: []media.MetadataProvider{NewDetails(store)},
	}
}
