package wikipedia

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the Wikipedia plugin: game sheets from Wikidata's facts and Wikipedia's articles, in
// the UI language when there is an article in it. version is Game Vault's, sent in the User-Agent
// as Wikimedia asks.
func Plugin(version string) plugin.Plugin {
	return plugin.Plugin{
		ID:       "wikipedia",
		Name:     "Wikipedia",
		Metadata: []media.MetadataProvider{New(version)},
	}
}
