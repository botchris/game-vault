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
	"gamevault/internal/adapters/outbound/igdb"
	"gamevault/internal/adapters/outbound/itad"
	"gamevault/internal/adapters/outbound/itch"
	"gamevault/internal/adapters/outbound/ntfy"
	"gamevault/internal/adapters/outbound/playstation"
	"gamevault/internal/adapters/outbound/steam"
	"gamevault/internal/adapters/outbound/steamgriddb"
	"gamevault/internal/adapters/outbound/telegram"
	"gamevault/internal/adapters/outbound/thegamesdb"
	"gamevault/internal/adapters/outbound/ubisoft"
	"gamevault/internal/adapters/outbound/upcitemdb"
	"gamevault/internal/adapters/outbound/wikipedia"
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
		epic.Plugin(log),
		gog.Plugin(log),
		battlenet.Plugin(steamStore),
		eaapp.Plugin(log),
		ubisoft.Plugin(log),
		xbox.Plugin(log),
		playstation.Plugin(log),
		amazon.Plugin(),
		itch.Plugin(log),
		// Key sellers
		humble.Plugin(log),
		fanatical.Plugin(log),
		// Game databases
		thegamesdb.Plugin(),
		wikipedia.Plugin(version),
		igdb.Plugin(),
		steamgriddb.Plugin(),
		// Deals
		itad.Plugin(),
		// Barcode lookups
		cex.Plugin(),
		ebay.Plugin(),
		upcitemdb.Plugin(),
		eansearch.Plugin(),
		// Notifications
		telegram.Plugin(),
		ntfy.Plugin(),
	)
}
