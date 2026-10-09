package battlenet

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// SteamStore is what the Battle.net plugin needs from the Steam plugin: Blizzard publishes no box
// art outside its shop, so its games are looked up on the Steam store.
type SteamStore interface {
	media.LinkSearcher
	media.CoverProvider
}

// Plugin is the Battle.net plugin: the library of Battle.net accounts and, through steam, their
// box art.
func Plugin(steam SteamStore) plugin.Plugin {
	return plugin.Plugin{
		ID: "battlenet", Name: "Battle.net",
		Sources: []sync.Provider{NewProvider()},
		Covers:  []media.CoverProvider{NewCovers(steam, steam)},
	}
}
