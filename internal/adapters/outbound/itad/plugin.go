package itad

import (
	"gamevault/internal/application/home"
	"gamevault/internal/application/plugin"
)

// Plugin is the IsThereAnyDeal plugin: the home page's current deals.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:    string(ID),
		Name:  "IsThereAnyDeal",
		Deals: []home.DealsProvider{NewDeals()},
	}
}
