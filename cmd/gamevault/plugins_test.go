package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/battlenet"
	"gamevault/internal/adapters/outbound/eaapp"
	"gamevault/internal/adapters/outbound/epic"
	"gamevault/internal/adapters/outbound/example"
	"gamevault/internal/adapters/outbound/gog"
	"gamevault/internal/adapters/outbound/steam"
	"gamevault/internal/adapters/outbound/thegamesdb"
	"gamevault/internal/adapters/outbound/ubisoft"
	"gamevault/internal/adapters/outbound/xbox"
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin/plugintest"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// TestPlugins checks every compiled-in plugin, so a new one is checked as soon as it is listed in
// newPlugins.
func TestPlugins(t *testing.T) {
	t.Run("GIVEN the plugins compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err, "plugin, source type and provider ids must be unique")

		tr, err := plugintest.LoadTranslations(".")
		require.NoError(t, err)

		for _, p := range plugins.Plugins() {
			t.Run("THEN "+p.ID+" is complete", func(t *testing.T) {
				plugintest.Check(t, p, tr)
			})
		}
	})
}

// coverRow is one query a cover provider is asked whether it applies to.
type coverRow struct {
	name string
	q    media.CoverQuery
	want bool
}

// storeRows are the queries every store's cover provider answers alike: it has art for the PC
// edition of the games linked to it (and for a game without copies), never for a console edition.
func storeRows(link string) []coverRow {
	linked := game.Links{link: "1"}

	return []coverRow{
		{"its PC edition", media.CoverQuery{
			System: game.SystemPC,
			Links:  linked,
		}, true},
		{"a game without copies", media.CoverQuery{Links: linked}, true},
		{"its PS3 edition", media.CoverQuery{
			System: "PS3",
			Links:  linked,
		}, false},
		{"a PC edition not linked to it", media.CoverQuery{System: game.SystemPC}, false},
	}
}

// TestCoverProviders_appliesBySystem checks which editions each store's cover provider has art
// for. TheGamesDB, box art for every system, is checked in its own package.
func TestCoverProviders_appliesBySystem(t *testing.T) {
	providers := map[provider.ID]struct {
		p    media.CoverProvider
		rows []coverRow
	}{
		steam.CoverProviderID:     {&steam.Store{}, storeRows(game.LinkSteam)},
		gog.CoverProviderID:       {&gog.Covers{}, storeRows(gog.LinkedStore.Key)},
		epic.CoverProviderID:      {&epic.Covers{}, storeRows(epic.LinkedStore.Key)},
		eaapp.CoverProviderID:     {&eaapp.Covers{}, storeRows(eaapp.LinkedStore.Key)},
		battlenet.CoverProviderID: {&battlenet.Covers{}, storeRows(battlenet.LinkedStore.Key)},
		example.CoverProviderID:   {&example.Covers{}, storeRows(example.LinkedStore.Key)},
		xbox.CoverProviderID: {&xbox.Covers{}, append(storeRows(xbox.LinkedStore.Key),
			coverRow{"its Xbox 360 edition", media.CoverQuery{
				System: "Xbox 360",
				Links:  game.Links{xbox.LinkedStore.Key: "1"},
			}, true})},
		ubisoft.CoverProviderID: {&ubisoft.Covers{}, append(storeRows(ubisoft.LinkedStore.Key),
			coverRow{"its PC edition by a Ubisoft Connect copy", media.CoverQuery{
				System:    game.SystemPC,
				Platforms: []string{ubisoft.Platform},
			}, true},
			coverRow{"its PS4 edition by a Ubisoft Connect copy", media.CoverQuery{
				System:    "PS4",
				Platforms: []string{ubisoft.Platform},
			}, false})},
	}

	t.Run("GIVEN the cover providers compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)

		t.Run("THEN each store's provider is in the table below", func(t *testing.T) {
			for _, p := range plugins.Media().Covers {
				if id := p.Descriptor().ID; id != thegamesdb.ID {
					assert.Contains(t, providers, id)
				}
			}
		})
	})

	for id, tc := range providers {
		t.Run(string(id), func(t *testing.T) {
			for _, row := range tc.rows {
				assert.Equal(t, row.want, tc.p.Applies(row.q), row.name)
			}
		})
	}
}
