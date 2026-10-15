package itad

import (
	"gamevault/internal/application/home"
	"gamevault/internal/application/plugin"
)

// Plugin is the IsThereAnyDeal plugin: the home page's current deals.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:    "itad",
		Name:  "IsThereAnyDeal",
		Deals: []home.DealsProvider{NewDeals()},
	}
}
