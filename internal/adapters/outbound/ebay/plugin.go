package ebay

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the eBay plugin: barcode lookups in eBay's catalog.
func Plugin() plugin.Plugin {
	return plugin.Plugin{ID: "ebay", Name: "eBay", Barcodes: []media.BarcodeProvider{New()}}
}
