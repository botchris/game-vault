package main

import (
	"cmp"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/battlenet"
	"gamevault/internal/adapters/outbound/eaapp"
	"gamevault/internal/adapters/outbound/epic"
	"gamevault/internal/adapters/outbound/example"
	"gamevault/internal/adapters/outbound/gog"
	"gamevault/internal/adapters/outbound/igdb"
	"gamevault/internal/adapters/outbound/steam"
	"gamevault/internal/adapters/outbound/steamgriddb"
	"gamevault/internal/adapters/outbound/thegamesdb"
	"gamevault/internal/adapters/outbound/ubisoft"
	"gamevault/internal/adapters/outbound/wikipedia"
	"gamevault/internal/adapters/outbound/xbox"
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
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
	q    media.GameQuery
	want bool
}

// storeRows are the queries every store's cover provider answers alike: it has art for the PC
// edition of the games linked to it (and for a game without copies), never for a console edition.
func storeRows(link string) []coverRow {
	linked := game.Links{link: "1"}

	return []coverRow{
		{"its PC edition", media.GameQuery{
			System: game.SystemPC,
			Links:  linked,
		}, true},
		{"a game without copies", media.GameQuery{Links: linked}, true},
		{"its PS3 edition", media.GameQuery{
			System: "PS3",
			Links:  linked,
		}, false},
		{"a PC edition not linked to it", media.GameQuery{System: game.SystemPC}, false},
	}
}

// TestCoverProviders_appliesBySystem checks which editions each store's cover provider has art
// for. TheGamesDB, box art for every system, is checked in its own package.
func TestCoverProviders_appliesBySystem(t *testing.T) {
	providers := map[provider.ID]struct {
		p    media.CoverProvider
		rows []coverRow
	}{
		// Steam also searches its store by title for a PC edition without an AppID, on the fallback pass.
		steam.CoverProviderID: {&steam.Store{}, append(storeRows(game.LinkSteam),
			coverRow{"a PC edition not linked to it, on the fallback pass", media.GameQuery{
				System:   game.SystemPC,
				Fallback: true,
			}, true},
			coverRow{"a PS3 edition not linked to it, on the fallback pass", media.GameQuery{
				System:   "PS3",
				Fallback: true,
			}, false},
			coverRow{"a PC edition not linked to it, with store links only", media.GameQuery{
				System:    game.SystemPC,
				Fallback:  true,
				LinksOnly: true,
			}, false})},
		gog.CoverProviderID:   {&gog.Covers{}, storeRows(gog.LinkedStore.Key)},
		epic.CoverProviderID:  {&epic.Covers{}, storeRows(epic.LinkedStore.Key)},
		eaapp.CoverProviderID: {&eaapp.Covers{}, storeRows(eaapp.LinkedStore.Key)},
		battlenet.CoverProviderID: {&battlenet.Covers{}, append(storeRows(battlenet.LinkedStore.Key),
			coverRow{"a virtual PC edition (store links only): it searches by title", media.GameQuery{
				System:    game.SystemPC,
				Links:     game.Links{battlenet.LinkedStore.Key: "1"},
				LinksOnly: true,
			}, false})},
		example.CoverProviderID: {&example.Covers{}, storeRows(example.LinkedStore.Key)},
		xbox.CoverProviderID: {&xbox.Covers{}, append(storeRows(xbox.LinkedStore.Key),
			coverRow{"its Xbox 360 edition", media.GameQuery{
				System: "Xbox 360",
				Links:  game.Links{xbox.LinkedStore.Key: "1"},
			}, true})},
		// IGDB's covers belong to the game, not to a system: any edition, as a last resort.
		igdb.CoversID: {igdb.NewCovers(igdb.NewClient()), []coverRow{
			{"a PC edition", media.GameQuery{System: game.SystemPC}, false},
			{"a game without copies", media.GameQuery{}, false},
			{"a PS3 edition", media.GameQuery{System: "PS3"}, false},
			{"a PC edition on the fallback pass", media.GameQuery{
				System:   game.SystemPC,
				Fallback: true,
			}, true},
			{"a PS3 edition on the fallback pass", media.GameQuery{
				System:   "PS3",
				Fallback: true,
			}, true},
			{"a virtual PC edition (store links only)", media.GameQuery{
				System:    game.SystemPC,
				Links:     game.Links{game.LinkSteam: "1"},
				Fallback:  true,
				LinksOnly: true,
			}, false},
		}},
		// SteamGridDB's community art is a last resort too, for any edition, but never a virtual PC box.
		steamgriddb.ID: {steamgriddb.NewCovers(), []coverRow{
			{"a PC edition", media.GameQuery{System: game.SystemPC}, false},
			{"a game without copies", media.GameQuery{}, false},
			{"a PS2 edition", media.GameQuery{System: "PS2"}, false},
			{"a PC edition linked to Steam", media.GameQuery{
				System: game.SystemPC,
				Links:  game.Links{game.LinkSteam: "1"},
			}, false},
			{"a PC edition on the fallback pass", media.GameQuery{
				System:   game.SystemPC,
				Fallback: true,
			}, true},
			{"a PS2 edition on the fallback pass", media.GameQuery{
				System:   "PS2",
				Fallback: true,
			}, true},
			{"a virtual PC edition (store links only)", media.GameQuery{
				System:    game.SystemPC,
				Links:     game.Links{game.LinkSteam: "1"},
				Fallback:  true,
				LinksOnly: true,
			}, false},
		}},
		ubisoft.CoverProviderID: {&ubisoft.Covers{}, append(storeRows(ubisoft.LinkedStore.Key),
			coverRow{"its PC edition by a Ubisoft Connect copy", media.GameQuery{
				System:    game.SystemPC,
				Platforms: []string{ubisoft.Platform},
			}, true},
			coverRow{"its PS4 edition by a Ubisoft Connect copy", media.GameQuery{
				System:    "PS4",
				Platforms: []string{ubisoft.Platform},
			}, false})},
	}

	t.Run("GIVEN the cover providers compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)

		t.Run("THEN each store's provider is in the table below", func(t *testing.T) {
			for _, p := range coversOf(plugins) {
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

// TestMetadataChain_defaultOrder checks the game sheet chain a new install starts with: the store
// page first, then Wikipedia (a summary in the UI language), IGDB, and TheGamesDB last.
func TestMetadataChain_defaultOrder(t *testing.T) {
	t.Run("GIVEN the metadata providers compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)

		t.Run("WHEN they are put in their default order", func(t *testing.T) {
			chain := metadataOf(plugins)
			slices.SortStableFunc(chain, func(a, b media.MetadataProvider) int {
				return cmp.Compare(a.Descriptor().DefaultOrder, b.Descriptor().DefaultOrder)
			})

			ids := make([]provider.ID, 0, len(chain))
			for _, p := range chain {
				ids = append(ids, p.Descriptor().ID)
			}

			t.Run("THEN Steam comes first, then Wikipedia, IGDB and TheGamesDB", func(t *testing.T) {
				assert.Equal(t, []provider.ID{steam.DetailsProviderID, wikipedia.DetailsID, igdb.DetailsID, thegamesdb.DetailsID}, ids)
			})
		})
	})
}

// TestCoverProviders_pcStoreLinks checks that every store whose cover provider has art for PC
// editions reads a link key the domain counts as a PC store: console-only games linked to it borrow
// its PC box (game.Game.BorrowsPCBox), and the web shows it only when the server says so.
func TestCoverProviders_pcStoreLinks(t *testing.T) {
	t.Run("GIVEN the cover providers compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)

		for _, p := range coversOf(plugins) {
			linker, ok := p.(media.StoreLinker)
			if !ok {
				continue
			}

			key := linker.LinkStore().Key
			if !p.Applies(media.GameQuery{
				System: game.SystemPC,
				Links:  game.Links{key: "1"},
			}) {
				continue
			}

			t.Run("THEN "+string(p.Descriptor().ID)+"'s link is a PC store link", func(t *testing.T) {
				assert.True(t, game.IsPCStoreLink(key), key)
			})
		}
	})
}

// TestPlugins_linkKeysAreTheDomains checks that every store a source links its copies to, and every
// store a media provider reads links of, uses one of the domain's link keys (game.Link…): the keys
// are stored in every game's links, and sources and providers meet only through them. Link keys
// written as literals are also rejected by TestVocabulary_adaptersUseTheDomainNames.
func TestPlugins_linkKeysAreTheDomains(t *testing.T) {
	t.Run("GIVEN the plugins compiled into Game Vault", func(t *testing.T) {
		plugins, err := newPlugins(slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)

		all := plugins.Providers()
		linkers := make([]any, 0, len(plugins.Sources())+len(all))

		for _, s := range plugins.Sources() {
			linkers = append(linkers, s)
		}

		for _, p := range all {
			linkers = append(linkers, p)
		}

		t.Run("THEN every store they link to has a key of the domain", func(t *testing.T) {
			seen := 0

			for _, l := range linkers {
				// sync.StoreLinker and media.StoreLinker are the same method set.
				linker, ok := l.(interface{ LinkStore() game.Store })
				if !ok {
					continue
				}

				seen++

				key := linker.LinkStore().Key
				assert.Contains(t, game.StoreLinks(), key, "%T links to %q", l, key)
			}

			assert.NotZero(t, seen)
		})
	})
}

// coversOf returns the cover providers of every plugin, in registration order.
func coversOf(r *plugin.Registry) []media.CoverProvider {
	var out []media.CoverProvider
	for _, p := range r.Plugins() {
		out = append(out, p.Covers...)
	}

	return out
}

// metadataOf returns the metadata providers of every plugin, in registration order.
func metadataOf(r *plugin.Registry) []media.MetadataProvider {
	var out []media.MetadataProvider
	for _, p := range r.Plugins() {
		out = append(out, p.Metadata...)
	}

	return out
}
