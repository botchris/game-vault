package csvfile

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

func TestDecode_columns(t *testing.T) {
	t.Run("GIVEN an export with Game Vault's columns", func(t *testing.T) {
		in := "title,platform,kind,status,key,redeemBy,origin,acquiredOn,edition,grade,contents,location,price,currency,notes,links,externalId,barcode\n" +
			"Hades,Steam,library,owned,,,Steam,,,,,,,,,steam:1145360,steam:1145360,\n" +
			"Celeste,Steam,key,unrevealed,,2026-10-19,Humble – Indie Bundle,2023-05-01,,,,,,,,,humble:AAA:1,\n"

		t.Run("WHEN it is imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)

			t.Run("THEN every row keeps its id, so later scans update it instead of duplicating it", func(t *testing.T) {
				assert.Empty(t, warnings)
				require.Len(t, copies, 2)
				assert.Equal(t, "steam:1145360", copies[0].ExternalID)
				assert.Equal(t, game.KindLibrary, copies[0].Details.Kind)
				assert.Equal(t, game.Links{game.LinkSteam: "1145360"}, copies[0].Links)
				assert.Equal(t, "humble:AAA:1", copies[1].ExternalID)
				assert.Equal(t, game.Date("2026-10-19"), copies[1].Details.RedeemBy)
				assert.Equal(t, "Humble – Indie Bundle", copies[1].Details.Origin)
			})
		})
	})

	t.Run("GIVEN the column names of the first Game Vault export", func(t *testing.T) {
		in := "title,cdKey,deadline,source,purchaseDate,steamAppId\nCeleste,AAAA-BBBB,2026-10-19,Humble,2023-05-01,504230\n"

		t.Run("WHEN it is imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)

			t.Run("THEN those columns are not read, and each one is reported", func(t *testing.T) {
				require.Len(t, copies, 1)
				assert.Empty(t, copies[0].Details.Key)
				assert.Empty(t, copies[0].Links)
				require.Len(t, warnings, 5)
				assert.Contains(t, warnings[0], `"cdKey"`)
			})
		})
	})
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	g, _ := game.New("Halo 3", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g.AddCopy(game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "Xbox 360",
		Location: "Shelf",
	}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var b strings.Builder
	if err := (Codec{}).Encode(&b, []*game.Game{g}); err != nil {
		t.Fatal(err)
	}

	copies, _, err := Codec{}.Decode(strings.NewReader(b.String()))
	if err != nil || len(copies) != 1 || copies[0].Details.Location != "Shelf" || copies[0].Title != "Halo 3" {
		t.Fatalf("round trip failed: %+v %v", copies, err)
	}
}

func TestLinks_roundTrip(t *testing.T) {
	t.Run("GIVEN a game linked to two stores", func(t *testing.T) {
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		g, _ := game.New("Hades", now)
		_, err := g.UpdateInfo(game.Info{
			Title: "Hades",
			Links: game.Links{"steam": "1145360", "gog": "1207658924"},
		}, now)
		require.NoError(t, err)
		g.AddCopy(game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		}, now)

		t.Run("WHEN it is exported and imported again", func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, Codec{}.Encode(&b, []*game.Game{g}))

			copies, warnings, err := Codec{}.Decode(strings.NewReader(b.String()))
			require.NoError(t, err)

			t.Run("THEN the links column lists both, sorted, and they come back", func(t *testing.T) {
				assert.Contains(t, b.String(), "gog:1207658924 steam:1145360")
				require.Len(t, copies, 1)
				assert.Empty(t, warnings)
				assert.Equal(t, game.Links{"steam": "1145360", "gog": "1207658924"}, copies[0].Links)
			})
		})
	})

	t.Run("GIVEN a links cell with a pair that is not store:id", func(t *testing.T) {
		in := "title,links\nHades,steam:1145360 1207658924\n"

		t.Run("WHEN it is imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)

			t.Run("THEN the valid link is kept and the other is reported", func(t *testing.T) {
				require.Len(t, copies, 1)
				assert.Equal(t, game.Links{"steam": "1145360"}, copies[0].Links)
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], `"1207658924"`)
			})
		})
	})
}

func TestDecode_semicolonEnglish(t *testing.T) {
	in := "title;platform;kind;acquiredOn\nHalo 3;Xbox 360;physical;12/05/2008\n"

	copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, copies, 1)
	assert.Equal(t, game.KindPhysical, copies[0].Details.Kind)
	assert.Equal(t, game.Date("2008-05-12"), copies[0].Details.AcquiredOn)
}

func TestDecode_physicalColumns(t *testing.T) {
	t.Run("GIVEN rows with grade, contents and prices", func(t *testing.T) {
		in := "title,kind,grade,contents,price,currency\n" +
			"Halo 3,physical,very_good,media box,\"29,95\",eur\n" +
			"Zelda,physical,,,1500,JPY\n" +
			"Okami,physical,,,12.5,\n" +
			"Bad,physical,excellent,poster,abc,EURO\n"

		t.Run("WHEN they are imported", func(t *testing.T) {
			copies, warnings, err := Codec{}.Decode(strings.NewReader(in))
			require.NoError(t, err)
			require.Len(t, copies, 4)

			t.Run("THEN valid values are read, in the currency's own decimals", func(t *testing.T) {
				d := copies[0].Details
				assert.Equal(t, game.GradeVeryGood, d.Grade)
				assert.Equal(t, []game.Content{game.ContentBox, game.ContentMedia}, d.Contents.List())
				assert.Equal(t, game.Money{
					Amount:   2995,
					Currency: "EUR",
				}, d.Price)
				assert.Equal(t, game.Money{
					Amount:   1500,
					Currency: "JPY",
				}, copies[1].Details.Price)
			})

			t.Run("AND a price without currency waits for the default one", func(t *testing.T) {
				assert.Equal(t, game.Money{Amount: 1250}, copies[2].Details.Price)
			})

			t.Run("AND invalid values are reported and left out, keeping the row", func(t *testing.T) {
				assert.Empty(t, copies[3].Details.Grade)
				assert.Zero(t, copies[3].Details.Contents)
				assert.True(t, copies[3].Details.Price.IsZero())
				assert.Len(t, warnings, 4) // grade, content, price, currency
			})
		})
	})

	t.Run("GIVEN the Spanish headers older files used", func(t *testing.T) {
		_, warnings, err := Codec{}.Decode(strings.NewReader("title,plataforma,notas\nHalo 3,Xbox 360,x\n"))

		t.Run("THEN they are unknown columns", func(t *testing.T) {
			require.NoError(t, err)
			assert.Len(t, warnings, 2)
		})
	})
}

func TestEncode_physicalColumns(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g, _ := game.New("Halo 3", now)
	contents, _ := game.ContentsOf(game.ContentBox, game.ContentMedia)
	_, err := g.AddCopy(game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "Xbox 360",
		Grade:    game.GradeGood,
		Contents: contents,
		Price: game.Money{
			Amount:   1500,
			Currency: "JPY",
		},
	}, now)
	require.NoError(t, err)

	var b strings.Builder
	require.NoError(t, Codec{}.Encode(&b, []*game.Game{g}))

	copies, warnings, err := Codec{}.Decode(strings.NewReader(b.String()))
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Contains(t, b.String(), "good,box media,,1500,JPY")
	assert.Equal(t, game.Money{
		Amount:   1500,
		Currency: "JPY",
	}, copies[0].Details.Price)
}
