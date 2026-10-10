package sqlite

import (
	"encoding/json"
	"strconv"
	"strings"
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
		Title:      "Hades",
		Links:      game.Links{"steam": "1145360"},
		Notes:      "GOTY",
		CoverURL:   "https://example.test/hades.jpg",
		PlayStatus: game.PlayFinished,
		Rating:     5,
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
				assert.InDelta(t, 2, doc["v"], 0)

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
				assert.Equal(t, game.PlayNone, got.PlayStatus())
				assert.Zero(t, got.Rating())
				require.Len(t, got.Copies(), 1)
				assert.Equal(t, game.KindLibrary, got.Copies()[0].Kind)
				assert.Empty(t, got.Copies()[0].Location)
			})
		})
	})

	t.Run("GIVEN a document written by a newer Game Vault", func(t *testing.T) {
		t.Run("THEN it is refused, saying to update", func(t *testing.T) {
			_, err := decodeGame("g3", `{"v":3,"title":"x"}`)
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
			Excluded:    2,
			Warnings:    []string{"one skipped"},
		}
		exclusions := []source.Exclusion{{
			ExternalID: "humble:a",
			Title:      "A",
			At:         docTime,
		}}
		s := source.Rehydrate("src1", "humble", "Humble Bundle", false, 24*time.Hour,
			source.Settings{"session": "secret"}, report, exclusions, docTime, docTime)

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
				assert.Equal(t, 2, got.LastSync().Excluded)
				require.Len(t, got.Exclusions(), 1)
				assert.Equal(t, "humble:a", got.Exclusions()[0].ExternalID)
				assert.Equal(t, "A", got.Exclusions()[0].Title)
				assert.True(t, docTime.Equal(got.Exclusions()[0].At))
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

func TestDocuments_physicalFields(t *testing.T) {
	t.Run("GIVEN a physical copy with grade, contents and price", func(t *testing.T) {
		contents, _ := game.ContentsOf(game.ContentBox, game.ContentMedia)
		g := game.Rehydrate("g1", game.Info{Title: "Halo 3"}, []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Xbox 360",
				Status:   game.StatusOwned,
				Grade:    game.GradeGood,
				Contents: contents,
				Price: game.Money{
					Amount:   1500,
					Currency: "JPY",
				},
			},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the new fields come back, as version 2", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"v":2`)
				assert.Contains(t, raw, `"contents":["box","media"]`)
			})
		})
	})
}

func TestDocuments_conditionConversion(t *testing.T) {
	cases := []struct {
		condition string
		grade     game.Grade
		contents  []game.Content
		note      string
	}{
		{"Sealed", game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{"precintado", game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{"Complete (case + manual)", "", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{"Completo (caja + manual)", "", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}, ""},
		{" Case and disc ", "", []game.Content{game.ContentBox, game.ContentMedia}, ""},
		{"Caja y disco", "", []game.Content{game.ContentBox, game.ContentMedia}, ""},
		{"Disc only", "", []game.Content{game.ContentMedia}, ""},
		{"Sólo disco", "", []game.Content{game.ContentMedia}, ""},
		{"Damaged", game.GradeDamaged, nil, ""},
		{"Dañado", game.GradeDamaged, nil, ""},
		{"Like new, no slip cover", "", nil, "signed\nCondition: Like new, no slip cover"},
	}

	for _, c := range cases {
		t.Run("GIVEN a version-1 copy whose condition is "+c.condition, func(t *testing.T) {
			raw := `{"v":1,"title":"Halo 3","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z",
				"copies":[{"id":"c1","kind":"physical","status":"owned","notes":"signed","condition":` + strconv.Quote(c.condition) + `}]}`

			got, err := decodeGame("g1", raw)
			require.NoError(t, err)

			t.Run("THEN it becomes a grade and contents, or a note", func(t *testing.T) {
				cp := got.Copies()[0]
				assert.Equal(t, c.grade, cp.Grade)
				assert.Equal(t, c.contents, cp.Contents.List())

				want := c.note
				if want == "" {
					want = "signed"
				}

				assert.Equal(t, want, cp.Notes)
			})
		})
	}
}

func TestDocuments_photos(t *testing.T) {
	t.Run("GIVEN a game with a photographed copy, one photo without a date, and a cover photo", func(t *testing.T) {
		id1, id2 := game.PhotoID(strings.Repeat("a", 64)), game.PhotoID(strings.Repeat("b", 64))
		g := game.Rehydrate("g1", game.Info{
			Title:      "Halo 3",
			CoverPhoto: id2,
		}, []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:   game.KindPhysical,
				Status: game.StatusOwned,
			},
			Photos: []game.Photo{
				{
					ID:      id1,
					Caption: "box",
					TakenAt: docTime.Add(-time.Hour),
					AddedAt: docTime,
				},
				{
					ID:      id2,
					AddedAt: docTime,
				},
			},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the photos and the cover photo come back, as version 2", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Equal(t, id2, got.CoverPhoto())
				assert.Contains(t, raw, `"v":2`)
			})

			t.Run("AND a photo without a date has no takenAt, and a copy without photos no photos field", func(t *testing.T) {
				assert.Equal(t, 1, strings.Count(raw, `"takenAt"`))

				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"photos"`)
				assert.NotContains(t, plain, `"coverPhoto"`)
			})
		})
	})
}

func TestDocuments_estimates(t *testing.T) {
	t.Run("GIVEN a physical copy with two estimates and a next date", func(t *testing.T) {
		next := docTime.Add(30 * 24 * time.Hour)
		g := game.Rehydrate("g1", game.Info{Title: "Dead Space 3"}, []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:    game.KindPhysical,
				Status:  game.StatusOwned,
				Barcode: "5030934110075",
			},
			Estimates: []game.Estimate{
				{
					Provider: "cex-prices",
					Sell: game.Money{
						Amount:   2000,
						Currency: "EUR",
					},
					BuyCash: game.Money{
						Amount:   600,
						Currency: "EUR",
					},
					BuyCredit: game.Money{
						Amount:   1000,
						Currency: "EUR",
					},
					URL:       "https://es.webuy.com/product-detail/?id=5030934110075",
					FetchedAt: docTime,
				},
				{
					Provider: "ebay-prices",
					Sell: game.Money{
						Amount:   1400,
						Currency: "EUR",
					},
					Listings:  9,
					FetchedAt: docTime,
				},
			},
			NextValuation: next,
			ValuedAt:      docTime,
			CreatedAt:     docTime,
			UpdatedAt:     docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the estimates and the date come back, as version 2", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"v":2`)
				assert.Contains(t, raw, `"currency":"EUR"`)
			})

			t.Run("AND zero amounts and empty fields are left out", func(t *testing.T) {
				assert.Equal(t, 1, strings.Count(raw, `"buyCash"`))
				assert.Equal(t, 1, strings.Count(raw, `"listings"`))

				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"estimates"`)
				assert.NotContains(t, plain, `"nextValuation"`)
			})
		})
	})
}

func TestDocuments_customFieldValues(t *testing.T) {
	t.Run("GIVEN a game and a copy with custom field values of every kind", func(t *testing.T) {
		n, m, yes := int64(1250), int64(90), true
		base := sampleGame(t)

		info := base.Info()
		info.Fields = game.FieldValues{
			"r": {Text: "Great"},
			"w": {Number: &n},
			"m": {Money: &game.Money{
				Amount:   499,
				Currency: "EUR",
			}},
			"d": {Date: "2024"},
			"t": {Choices: []string{"a", "b"}},
		}

		copies := base.Copies()
		copies[0].Fields = game.FieldValues{
			"s": {Bool: &yes},
			"l": {Choice: "x"},
			"h": {Minutes: &m},
		}

		g := game.Rehydrate(base.ID(), info, copies, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the values are the same", func(t *testing.T) {
				assert.Equal(t, g.Fields(), got.Fields())
				assert.Equal(t, g.Copies()[0].Fields, got.Copies()[0].Fields)
			})
		})
	})

	t.Run("GIVEN a game without custom field values", func(t *testing.T) {
		raw, err := encodeGame(sampleGame(t))
		require.NoError(t, err)

		t.Run("THEN the document has no fields member", func(t *testing.T) {
			assert.NotContains(t, raw, `"fields"`)
		})
	})
}
