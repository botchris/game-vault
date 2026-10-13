package battlenet

import (
	"context"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
)

// SteamStore is what the Battle.net plugin needs from the Steam plugin: Blizzard publishes no box
// art outside its shop, so its games are looked up on the Steam store, with Steam's own rules for
// finding a game by its title.
type SteamStore interface {
	media.CoverProvider

	// AppIDByTitle returns the AppID of the only Steam store app with the title (an exact title
	// first, then one with the same game.MatchKey), 0 when there is none or several. An answer that
	// says nothing about the game (a 429, an outage) is an error matching provider.ErrRateLimited.
	AppIDByTitle(ctx context.Context, title string) (int64, error)
}

// Plugin is the Battle.net plugin: the library of Battle.net accounts and, through steam, their
// box art.
func Plugin(steam SteamStore) plugin.Plugin {
	return plugin.Plugin{
		ID:      "battlenet",
		Name:    "Battle.net",
		Sources: []sync.Provider{NewProvider()},
		Covers:  []media.CoverProvider{NewCovers(steam)},
	}
}
