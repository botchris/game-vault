package eansearch

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the EAN-Search plugin: barcode lookups in EAN-Search.
func Plugin() plugin.Plugin {
	return plugin.Plugin{ID: "eansearch", Name: "EAN-Search", Barcodes: []media.BarcodeProvider{New()}}
}
