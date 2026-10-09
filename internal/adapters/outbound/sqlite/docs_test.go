package sqlite

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

var docTime = time.Date(2026, 10, 9, 18, 30, 0, 123456789, time.UTC)

func sampleGame(t *testing.T) *game.Game {
	t.Helper()

	return game.Rehydrate("g1", game.Info{
		Title:    "Hades",
		Links:    game.Links{"steam": "1145360"},
		Notes:    "GOTY",
		CoverURL: "https://example.test/hades.jpg",
	}, []game.Copy{
		{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindKey,
				Platform: "Steam",
				Status:   game.StatusRevealed,
				Key:      "AAAA-BBBB",
				RedeemBy: "2027-01-01",
				Origin:   "Humble – Indie Bundle",
			},
			SourceID:   "src1",
			ExternalID: "humble:A:hades:0",
			CreatedAt:  docTime,
			UpdatedAt:  docTime,
		},
		{
			ID: "c2",
			CopyDetails: game.CopyDetails{
				Kind:       game.KindPhysical,
				Platform:   "PS4",
				Status:     game.StatusOwned,
				AcquiredOn: "2021-07-11",
				Edition:    "Collector's",
				Location:   "Shelf",
				Barcode:    "5026555255042",
				Notes:      "signed",
			},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		},
	}, docTime, docTime)
}

func TestDocuments_game(t *testing.T) {
	t.Run("GIVEN a game with a key and a physical copy", func(t *testing.T) {
		g := sampleGame(t)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the aggregate is the same", func(t *testing.T) {
				assert.Equal(t, g.Info(), got.Info())
				assert.Equal(t, g.Copies(), got.Copies())
				assert.True(t, g.CreatedAt().Equal(got.CreatedAt()))
				assert.True(t, g.UpdatedAt().Equal(got.UpdatedAt()))
			})

			t.Run("AND the document has a version and no empty fields", func(t *testing.T) {
				var doc map[string]any
				require.NoError(t, json.Unmarshal([]byte(raw), &doc))
				assert.InDelta(t, 1, doc["v"], 0)

				physical := doc["copies"].([]any)[1].(map[string]any)
				assert.NotContains(t, physical, "key")
				assert.NotContains(t, physical, "sourceId")
			})
		})
	})

	t.Run("GIVEN a document from an older version without some fields, and with unknown ones", func(t *testing.T) {
		raw := `{"v":1,"title":"Celeste","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z",
			"futureField":42,"copies":[{"id":"c1","kind":"library","platform":"Steam","status":"owned","extra":"x"}]}`

		t.Run("WHEN it is decoded", func(t *testing.T) {
			got, err := decodeGame("g2", raw)
			require.NoError(t, err)

			t.Run("THEN missing fields are empty and unknown ones are ignored", func(t *testing.T) {
				assert.Equal(t, "Celeste", got.Title())
				assert.Empty(t, got.Links())
				require.Len(t, got.Copies(), 1)
				assert.Equal(t, game.KindLibrary, got.Copies()[0].Kind)
				assert.Empty(t, got.Copies()[0].Location)
			})
		})
	})

	t.Run("GIVEN a document written by a newer Game Vault", func(t *testing.T) {
		t.Run("THEN it is refused, saying to update", func(t *testing.T) {
			_, err := decodeGame("g3", `{"v":2,"title":"x"}`)
			assert.ErrorIs(t, err, errNewerDocument)
		})
	})
}

func TestDocuments_sourceAndProvider(t *testing.T) {
	t.Run("GIVEN a disabled source with settings and a sync report", func(t *testing.T) {
		report := &source.SyncReport{
			StartedAt:   docTime,
			FinishedAt:  docTime.Add(time.Minute),
			Fetched:     3,
			CopiesAdded: 2,
			Warnings:    []string{"one skipped"},
		}
		s := source.Rehydrate("src1", "humble", "Humble Bundle", false, 24*time.Hour,
			source.Settings{"session": "secret"}, report, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeSource(s)
			require.NoError(t, err)

			got, err := decodeSource(s.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN every field comes back", func(t *testing.T) {
				assert.Equal(t, s.Type(), got.Type())
				assert.Equal(t, s.Name(), got.Name())
				assert.False(t, got.Enabled())
				assert.Equal(t, s.SyncInterval(), got.SyncInterval())
				assert.Equal(t, s.Settings(), got.Settings())
				require.NotNil(t, got.LastSync())
				assert.Equal(t, report.Warnings, got.LastSync().Warnings)
				assert.True(t, report.FinishedAt.Equal(got.LastSync().FinishedAt))
			})
		})
	})

	t.Run("GIVEN a source never scanned, stored with an explicit null report", func(t *testing.T) {
		got, err := decodeSource("src2", `{"v":1,"type":"steam","name":"Steam","enabled":true,"lastSync":null}`)

		t.Run("THEN it has no report and empty settings, not nil ones", func(t *testing.T) {
			require.NoError(t, err)
			assert.Nil(t, got.LastSync())
			assert.NotNil(t, got.Settings())
		})
	})

	t.Run("GIVEN a disabled provider with settings", func(t *testing.T) {
		p := provider.Rehydrate("thegamesdb", provider.KindCover, false, 3, schema.Settings{"api_key": "k"}, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeProvider(p)
			require.NoError(t, err)

			got, err := decodeProvider(p.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN every field comes back", func(t *testing.T) {
				assert.Equal(t, provider.KindCover, got.Kind())
				assert.False(t, got.Enabled())
				assert.Equal(t, 3, got.Priority())
				assert.Equal(t, p.Settings(), got.Settings())
				assert.True(t, docTime.Equal(got.UpdatedAt()))
			})
		})
	})
}
