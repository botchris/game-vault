package upcitemdb

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the UPCitemdb plugin: barcode lookups in UPCitemdb.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:       "upcitemdb",
		Name:     "UPCitemdb",
		Barcodes: []media.BarcodeProvider{New()},
	}
}
