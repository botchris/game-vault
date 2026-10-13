package ebay

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/valuation"
)

// Plugin is the eBay plugin: barcode lookups in eBay's catalog and the asking prices of used
// listings. Both share one client, so one set of keys costs one access token.
func Plugin() plugin.Plugin {
	ebay := New()

	return plugin.Plugin{
		ID:         "ebay",
		Name:       "eBay",
		Barcodes:   []media.BarcodeProvider{ebay},
		Valuations: []valuation.Provider{NewPrices(ebay)},
	}
}
