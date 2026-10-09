package cex

import (
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
)

// Plugin is the CeX plugin: barcode lookups in CeX's catalog.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:       "cex",
		Name:     "CeX",
		Barcodes: []media.BarcodeProvider{New()},
	}
}
