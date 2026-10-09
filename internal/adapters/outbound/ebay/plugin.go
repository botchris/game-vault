package ebay

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/valuation"
)

// Plugin is the eBay plugin: barcode lookups in eBay's catalog and the asking prices of used
// listings.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:         "ebay",
		Name:       "eBay",
		Barcodes:   []media.BarcodeProvider{New()},
		Valuations: []valuation.Provider{NewPrices()},
	}
}
