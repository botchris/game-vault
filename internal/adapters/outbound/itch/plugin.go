// Package itch is the itch.io plugin: the games of an itch.io account, read with a personal API
// key through the official server-side API (https://itch.io/docs/api/serverside).
package itch

import (
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
)

// LinkedStore is itch.io. Its game pages are addressed by the developer's subdomain and a slug, not
// by the numeric id the API gives, so it has no PageURL.
var LinkedStore = game.Store{
	Key:  "itch",
	Name: "itch.io",
}

// Plugin is the itch.io plugin: the games an itch.io account owns.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:      "itch",
		Name:    "itch.io",
		Sources: []sync.Provider{NewProvider()},
	}
}
