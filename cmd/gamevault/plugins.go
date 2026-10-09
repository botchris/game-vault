package main

import (
	"log/slog"

	"gamevault/internal/adapters/outbound/amazon"
	"gamevault/internal/adapters/outbound/battlenet"
	"gamevault/internal/adapters/outbound/cex"
	"gamevault/internal/adapters/outbound/eaapp"
	"gamevault/internal/adapters/outbound/eansearch"
	"gamevault/internal/adapters/outbound/ebay"
	"gamevault/internal/adapters/outbound/epic"
	"gamevault/internal/adapters/outbound/fanatical"
	"gamevault/internal/adapters/outbound/gog"
	"gamevault/internal/adapters/outbound/humble"
	"gamevault/internal/adapters/outbound/playstation"
	"gamevault/internal/adapters/outbound/steam"
	"gamevault/internal/adapters/outbound/thegamesdb"
	"gamevault/internal/adapters/outbound/ubisoft"
	"gamevault/internal/adapters/outbound/upcitemdb"
	"gamevault/internal/adapters/outbound/xbox"
	"gamevault/internal/application/plugin"
)

// newPlugins lists the integrations compiled into Game Vault. Adding one is writing its package
// (see .claude/docs/integrations.md) and one line here. The order only breaks ties between
// providers with the same DefaultOrder.
func newPlugins(log *slog.Logger) (*plugin.Registry, error) {
	steamStore := steam.NewStore() // shared: Battle.net's covers search the Steam store

	return plugin.NewRegistry(
		// Stores
		steam.Plugin(steamStore),
		epic.Plugin(),
		gog.Plugin(),
		battlenet.Plugin(steamStore),
		eaapp.Plugin(),
		ubisoft.Plugin(),
		xbox.Plugin(),
		playstation.Plugin(),
		amazon.Plugin(),
		// Key sellers
		humble.Plugin(log),
		fanatical.Plugin(),
		// Game databases
		thegamesdb.Plugin(),
		// Barcode lookups
		cex.Plugin(),
		ebay.Plugin(),
		upcitemdb.Plugin(),
		eansearch.Plugin(),
	)
}
