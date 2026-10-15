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

	g := game.Rehydrate(game.Snapshot{
		ID: "g1",
		Info: game.Info{
			Title:      "Hades",
			Links:      game.Links{"steam": "1145360"},
			Notes:      "GOTY",
			PlayStatus: game.PlayFinished,
			Rating:     5,
		},
		Copies: []game.Copy{
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
					Barcode:    "2001234567893",
					Notes:      "signed",
				},
				CreatedAt: docTime,
				UpdatedAt: docTime,
			},
		},
		Covers:     map[string]game.EditionCover{"PS4": {URL: "https://example.test/hades.jpg"}},
		MainSystem: "PS4",
		CreatedAt:  docTime,
		UpdatedAt:  docTime,
	})

	return g
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
				assert.Equal(t, g.Covers(), got.Covers())
				assert.Equal(t, g.MainSystem(), got.MainSystem())
				assert.True(t, g.CreatedAt().Equal(got.CreatedAt()))
				assert.True(t, g.UpdatedAt().Equal(got.UpdatedAt()))
			})

			t.Run("AND the document has a version and no empty fields", func(t *testing.T) {
				var doc map[string]any
				require.NoError(t, json.Unmarshal([]byte(raw), &doc))
				assert.InDelta(t, 3, doc["v"], 0)

				physical := doc["copies"].([]any)[1].(map[string]any)
				assert.NotContains(t, physical, "key")
				assert.NotContains(t, physical, "sourceId")
			})
		})
	})

	t.Run("GIVEN a current document without some fields, and with unknown ones", func(t *testing.T) {
		raw := `{"v":3,"title":"Celeste","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z",
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
			_, err := decodeGame("g3", `{"v":4,"title":"x"}`)
			assert.ErrorIs(t, err, errNewerDocument)
		})
	})

	// Versions 1 (a free-text condition on copies) and 2 (one cover per game) are no longer read:
	// their old fields would be dropped without a word, so the document is refused instead.
	for _, v := range []int{1, 2} {
		t.Run("GIVEN a version-"+strconv.Itoa(v)+" document", func(t *testing.T) {
			raw := `{"v":` + strconv.Itoa(v) + `,"title":"Halo 3","coverUrl":"https://example.test/halo.jpg",` +
				`"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z",` +
				`"copies":[{"id":"c1","kind":"physical","status":"owned","condition":"Sealed"}]}`

			t.Run("WHEN it is decoded", func(t *testing.T) {
				_, err := decodeGame("g4", raw)

				t.Run("THEN it is refused, saying its version is no longer supported", func(t *testing.T) {
					require.ErrorIs(t, err, errOlderDocument)
					assert.Contains(t, err.Error(), "game document version "+strconv.Itoa(v)+" is no longer supported")
				})
			})
		})
	}
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
		s := source.Rehydrate(source.Snapshot{
			ID:           "src1",
			Type:         "humble",
			Name:         "Humble Bundle",
			SyncInterval: 24 * time.Hour,
			Settings:     schema.Settings{"session": "secret"},
			LastSync:     report,
			Exclusions:   exclusions,
			CreatedAt:    docTime,
			UpdatedAt:    docTime,
		})

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
		g := game.Rehydrate(game.Snapshot{
			ID:   "g1",
			Info: game.Info{Title: "Halo 3"},
			Copies: []game.Copy{{
				ID: "c1",
				CopyDetails: game.CopyDetails{
					Kind:     game.KindPhysical,
					Platform: "Xbox 360",
					Status:   game.StatusOwned,
					Grade:    game.GradeGood,
					Region:   game.RegionPAL,
					Contents: contents,
					Price: game.Money{
						Amount:   1500,
						Currency: "JPY",
					},
				},
				CreatedAt: docTime,
				UpdatedAt: docTime,
			}},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		})

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the new fields come back, as version 3", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"v":3`)
				assert.Contains(t, raw, `"region":"pal"`)
				assert.Contains(t, raw, `"contents":["box","media"]`)
			})
		})
	})
}

func TestDocuments_photos(t *testing.T) {
	t.Run("GIVEN a game with a photographed copy, one photo without a date, and a cover photo", func(t *testing.T) {
		id1, id2 := game.PhotoID(strings.Repeat("a", 64)), game.PhotoID(strings.Repeat("b", 64))
		g := game.Rehydrate(game.Snapshot{
			ID:   "g1",
			Info: game.Info{Title: "Halo 3"},
			Copies: []game.Copy{{
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
			}},
			Covers:    map[string]game.EditionCover{game.SystemOther: {Photo: id2}},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		})

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the photos and the edition's cover photo come back, as version 3", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Equal(t, g.Covers(), got.Covers())
				assert.Contains(t, raw, `"v":3`)
			})

			t.Run("AND a photo without a date has no takenAt, and a copy without photos no photos field", func(t *testing.T) {
				assert.Equal(t, 1, strings.Count(raw, `"takenAt"`))

				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"photos"`)
				assert.NotContains(t, plain, `"photo"`)
			})
		})
	})
}

func TestDocuments_estimates(t *testing.T) {
	t.Run("GIVEN a physical copy with two estimates and a next date", func(t *testing.T) {
		next := docTime.Add(30 * 24 * time.Hour)
		g := game.Rehydrate(game.Snapshot{
			ID:   "g1",
			Info: game.Info{Title: "Dead Space 3"},
			Copies: []game.Copy{{
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
			}},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		})

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the estimates and the date come back, as version 3", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"v":3`)
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

		g := game.Rehydrate(game.Snapshot{
			ID:        base.ID(),
			Info:      info,
			Copies:    copies,
			CreatedAt: docTime,
			UpdatedAt: docTime,
		})

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

func TestDocuments_systems(t *testing.T) {
	t.Run("GIVEN a copy with the user's system and its source's", func(t *testing.T) {
		g := game.Rehydrate(game.Snapshot{
			ID:   "g1",
			Info: game.Info{Title: "Astro Bot"},
			Copies: []game.Copy{{
				ID: "c1",
				CopyDetails: game.CopyDetails{
					Kind:     game.KindLibrary,
					Platform: "PlayStation Store",
					Status:   game.StatusOwned,
					System:   "PS4",
				},
				SourceSystem: "PS5",
				CreatedAt:    docTime,
				UpdatedAt:    docTime,
			}},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		})

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN both come back", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"system":"PS4"`)
				assert.Contains(t, raw, `"sourceSystem":"PS5"`)
			})

			t.Run("AND a copy with neither writes neither", func(t *testing.T) {
				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"system"`)
				assert.NotContains(t, plain, `"sourceSystem"`)
			})
		})
	})
}

func TestDocuments_editions(t *testing.T) {
	photo := game.PhotoID(strings.Repeat("c", 64))

	t.Run("GIVEN a game with a chosen cover per edition and a main system", func(t *testing.T) {
		g := game.Rehydrate(game.Snapshot{
			ID:   "g1",
			Info: game.Info{Title: "Halo 3"},
			Copies: []game.Copy{
				{
					ID: "c1",
					CopyDetails: game.CopyDetails{
						Kind:     game.KindPhysical,
						Platform: "PS3",
						Status:   game.StatusOwned,
					},
					Photos: []game.Photo{{
						ID:      photo,
						AddedAt: docTime,
					}},
					CreatedAt: docTime,
					UpdatedAt: docTime,
				},
				{
					ID: "c2",
					CopyDetails: game.CopyDetails{
						Kind:     game.KindPhysical,
						Platform: "Xbox 360",
						Status:   game.StatusOwned,
					},
					CreatedAt: docTime,
					UpdatedAt: docTime,
				},
			},
			Covers:     map[string]game.EditionCover{"PS3": {Photo: photo}, "Xbox 360": {URL: "https://example.test/x360.jpg"}},
			MainSystem: "Xbox 360",
			CreatedAt:  docTime,
			UpdatedAt:  docTime,
		})

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the covers and the main system come back, in a version-3 document", func(t *testing.T) {
				assert.Equal(t, g.Covers(), got.Covers())
				assert.Equal(t, "Xbox 360", got.MainSystem())
				assert.Contains(t, raw, `"v":3`)
				assert.Contains(t, raw, `"covers":{`)
				assert.Contains(t, raw, `"mainSystem":"Xbox 360"`)
				assert.NotContains(t, raw, `"coverUrl"`)
				assert.NotContains(t, raw, `"coverPhoto"`)
			})

			t.Run("AND a game without chosen covers writes neither field", func(t *testing.T) {
				plain, err := encodeGame(game.Rehydrate(game.Snapshot{
					ID:        "g2",
					Info:      game.Info{Title: "Hades"},
					CreatedAt: docTime,
					UpdatedAt: docTime,
				}))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"covers"`)
				assert.NotContains(t, plain, `"mainSystem"`)
			})
		})
	})

	t.Run("GIVEN a game without copies with a chosen cover WHEN it is encoded and decoded THEN the cover comes back", func(t *testing.T) {
		g := game.Rehydrate(game.Snapshot{
			ID:        "g3",
			Info:      game.Info{Title: "Halo 3"},
			Covers:    map[string]game.EditionCover{"": {URL: "https://example.test/halo.jpg"}},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		})

		raw, err := encodeGame(g)
		require.NoError(t, err)

		got, err := decodeGame(g.ID(), raw)
		require.NoError(t, err)
		assert.Equal(t, map[string]game.EditionCover{"": {URL: "https://example.test/halo.jpg"}}, got.Covers())
	})

	t.Run("GIVEN a version-3 document without covers WHEN it is read THEN the game has none", func(t *testing.T) {
		got, err := decodeGame("g5", `{"v":3,"title":"Halo 3","createdAt":"2026-10-09T18:30:00Z","updatedAt":"2026-10-09T18:30:00Z"}`)
		require.NoError(t, err)
		assert.Nil(t, got.Covers())
	})
}

func TestDocuments_autoLinks(t *testing.T) {
	t.Run("GIVEN a game with an automatic link, a rejected one and an automatic search done", func(t *testing.T) {
		g := game.Rehydrate(game.Snapshot{
			ID: "g1",
			Info: game.Info{
				Title: "Console Quest",
				Links: game.Links{game.LinkSteam: "100", "psn": "PPSA0001"},
			},
			AutoLinks: []string{game.LinkSteam},
			RejectedLinks: []game.LinkRef{{
				Key: "gog",
				ID:  "200",
			}},
			AutoLinkTriedAt: docTime,
			CreatedAt:       docTime,
			UpdatedAt:       docTime,
		})

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the origins come back, in new fields of the same document version", func(t *testing.T) {
				assert.Equal(t, []string{game.LinkSteam}, got.AutoLinks())
				assert.Equal(t, g.RejectedLinks(), got.RejectedLinks())
				assert.True(t, got.AutoLinkTriedAt().Equal(docTime))
				assert.Contains(t, raw, `"v":3`)
			})

			t.Run("AND a game without them writes none of the fields", func(t *testing.T) {
				plain, err := encodeGame(game.Rehydrate(game.Snapshot{
					ID:        "g2",
					Info:      game.Info{Title: "Plain"},
					CreatedAt: docTime,
					UpdatedAt: docTime,
				}))
				require.NoError(t, err)
				assert.NotContains(t, plain, "autoLinks")
				assert.NotContains(t, plain, "rejectedLinks")
				assert.NotContains(t, plain, "autoLinkTriedAt")
			})
		})
	})
}
