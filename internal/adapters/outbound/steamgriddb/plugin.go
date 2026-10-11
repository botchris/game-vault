package steamgriddb

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the SteamGridDB plugin: community box art as a last resort in the cover chain, behind a
// risk consent and the user's own API key.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:     "steamgriddb",
		Name:   "SteamGridDB",
		Covers: []media.CoverProvider{NewCovers()},
	}
}
