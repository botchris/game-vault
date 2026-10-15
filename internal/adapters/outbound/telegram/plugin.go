package telegram

import (
	"gamevault/internal/application/notify"
	"gamevault/internal/application/plugin"
)

// Plugin is the Telegram plugin: notifications through a bot.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:         "telegram",
		Name:       "Telegram",
		Connectors: []notify.Connector{NewConnector()},
	}
}
