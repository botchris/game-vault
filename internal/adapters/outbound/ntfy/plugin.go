package ntfy

import (
	"gamevault/internal/application/notify"
	"gamevault/internal/application/plugin"
)

// Plugin is the ntfy plugin: push notifications to a topic of ntfy.sh or a server of the user's own.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:         "ntfy",
		Name:       "ntfy",
		Connectors: []notify.Connector{NewConnector()},
	}
}
