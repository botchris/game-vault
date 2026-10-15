package igdb

import (
	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/domain/schema"
)

// Plugin is the IGDB plugin: game sheets, covers and upcoming releases from Twitch's game
// database, sharing one Twitch application's credentials, one access token and one rate limiter.
func Plugin() plugin.Plugin {
	c := NewClient()

	return plugin.Plugin{
		ID:       "igdb",
		Name:     "IGDB",
		Covers:   []media.CoverProvider{NewCovers(c)},
		Metadata: []media.MetadataProvider{NewDetails(c)},
		Releases: []home.ReleasesProvider{NewReleases(c)},
	}
}

// fields are the settings the plugin's providers share: the Twitch application's credentials.
func fields() schema.Fields {
	return schema.Fields{
		{
			Key:      settingClientID,
			LabelKey: "providers.igdb.clientId",
			HelpKey:  "providers.igdb.clientIdHelp",
			HelpURL:  "https://dev.twitch.tv/console/apps",
			Kind:     schema.FieldText,
			Required: true,
		},
		{
			Key:      settingClientSecret,
			LabelKey: "providers.igdb.clientSecret",
			HelpKey:  "providers.igdb.clientSecretHelp",
			HelpURL:  "https://dev.twitch.tv/console/apps",
			Kind:     schema.FieldSecret,
			Required: true,
		},
	}
}
