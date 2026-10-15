package webhook

import (
	"gamevault/internal/application/notify"
	"gamevault/internal/application/plugin"
)

// Plugin is the webhook plugin: every event as a JSON document posted to an address of the user's.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:         "webhook",
		Name:       "Webhook",
		Connectors: []notify.Connector{NewConnector()},
	}
}
