package thegamesdb

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the TheGamesDB plugin: box art and details from the community game database, sharing
// one API key.
func Plugin() plugin.Plugin {
	tgdb := New()

	return plugin.Plugin{
		ID: "thegamesdb", Name: "TheGamesDB",
		Covers:   []media.CoverProvider{tgdb},
		Metadata: []media.MetadataProvider{NewDetails(tgdb)},
	}
}
