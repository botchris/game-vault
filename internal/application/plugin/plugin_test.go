package plugin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

type fakeSource struct{ typ source.Type }

func (f fakeSource) Descriptor() source.TypeDescriptor { return source.TypeDescriptor{Type: f.typ} }
func (fakeSource) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	return nil, nil, nil
}
func (fakeSource) Test(context.Context, source.Settings) error { return nil }

type fakeCovers struct{ id provider.ID }

func (f fakeCovers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindCover,
	}
}
func (fakeCovers) Test(context.Context, schema.Settings) error { return nil }
func (fakeCovers) Applies(media.CoverQuery) bool               { return true }
func (fakeCovers) Covers(context.Context, media.CoverQuery, schema.Settings) ([]media.CoverCandidate, error) {
	return nil, nil
}

func TestRegistry(t *testing.T) {
	store := plugin.Plugin{
		ID:      "store",
		Sources: []sync.Provider{fakeSource{"store"}},
		Covers:  []media.CoverProvider{fakeCovers{"store"}},
	}
	db := plugin.Plugin{
		ID:     "db",
		Covers: []media.CoverProvider{fakeCovers{"db"}},
	}

	t.Run("GIVEN plugins with unique ids", func(t *testing.T) {
		r, err := plugin.NewRegistry(store, db)
		require.NoError(t, err)

		t.Run("THEN each piece goes to its service, in plugin order", func(t *testing.T) {
			require.Len(t, r.Sources(), 1)
			covers := r.Media().Covers
			require.Len(t, covers, 2)
			assert.Equal(t, provider.ID("store"), covers[0].Descriptor().ID)
			assert.Equal(t, provider.ID("db"), covers[1].Descriptor().ID)
		})
	})

	t.Run("GIVEN two plugins with a provider under the same id", func(t *testing.T) {
		_, err := plugin.NewRegistry(store, plugin.Plugin{
			ID:     "copy",
			Covers: []media.CoverProvider{fakeCovers{"store"}},
		})

		t.Run("THEN the registry refuses them, naming both", func(t *testing.T) {
			assert.ErrorIs(t, err, plugin.ErrDuplicate)
			assert.ErrorContains(t, err, `provider "store" in plugins "store" and "copy"`)
		})
	})

	t.Run("GIVEN two plugins with the same source type or id", func(t *testing.T) {
		_, errType := plugin.NewRegistry(store, plugin.Plugin{
			ID:      "other",
			Sources: []sync.Provider{fakeSource{"store"}},
		})
		_, errID := plugin.NewRegistry(store, plugin.Plugin{ID: "store"})

		t.Run("THEN both are refused", func(t *testing.T) {
			assert.ErrorIs(t, errType, plugin.ErrDuplicate)
			assert.ErrorIs(t, errID, plugin.ErrDuplicate)
		})
	})
}
