package cex

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/valuation"
)

// Plugin is the CeX plugin: barcode lookups in CeX's catalog and CeX's second-hand prices.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:         "cex",
		Name:       "CeX",
		Barcodes:   []media.BarcodeProvider{New()},
		Valuations: []valuation.Provider{NewPrices()},
	}
}
