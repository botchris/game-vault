// Package example is a complete plugin for an imaginary store, "Example Store", to copy when
// writing a new one (see docs/plugins.md). It compiles and is tested like the real plugins but is
// not registered in cmd/gamevault/plugins.go, so it never shows up in Game Vault.
//
// It has the two pieces most store plugins have:
//   - a source (source.go) that imports the games of an account with an API token, and links each
//     game to the store;
//   - a cover provider (covers.go) that finds the box art of games linked to the store, whether the
//     link came from an account or was added by hand.
//
// A new plugin starts as a copy of this package: rename it, replace the API calls and the answer
// shapes, add its texts to web/src/i18n/locales/en.json and es.json, and list its Plugin() in
// cmd/gamevault/plugins.go.
package example

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
)

// defaultBaseURL is the store's API. Plugins keep it in their apiclient.Client, so tests replace it.
const defaultBaseURL = "https://api.example-store.invalid"

// LinkedStore is the store this plugin links games to. The source sets the link on every game it
// imports and the cover provider reads it: that is the only thing the two pieces share.
var LinkedStore = game.Store{
	Key:     "example",
	Name:    "Example Store",
	PageURL: "https://example-store.invalid/game/{id}",
}

// Plugin is the Example Store plugin: the library of Example Store accounts and the store's box art.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:      "example",
		Name:    "Example Store",
		Sources: []sync.Provider{NewProvider()},
		Covers:  []media.CoverProvider{NewCovers()},
	}
}
