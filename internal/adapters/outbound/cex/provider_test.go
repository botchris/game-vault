package cex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

// Real answers of GET https://wss2.cex.{country}.webuy.io/v3/boxes/{ean}/detail (2026-10-08),
// trimmed to the fields that matter. Dead Space 3's Spanish edition is only in the Spanish
// catalog; unknown codes are a 200 with "data": null.
const (
	deadSpace3 = `{"response":{"ack":"Success","data":{"boxDetails":[{"boxId":"5030934110075",
		"boxName":"Dead Space 3 (2 Discs)","categoryId":827,"categoryName":"Xbox 360 Juegos",
		"categoryFriendlyName":"Xbox 360 Juegos","superCatId":1,"superCatName":"Juegos","sellPrice":20}],
		"masterBoxDetails":null},"error":{"code":"","internal_message":"","moreInfo":[]}}}`
	film = `{"response":{"ack":"Success","data":{"boxDetails":[{"boxId":"5051892123456",
		"boxName":"Inception (2 Discs)","categoryName":"Blu-Ray Peliculas","superCatId":2}]},"error":{}}}`
	notFound = `{"response":{"ack":"Success","data":null,"error":{"code":"","internal_message":"","moreInfo":[]}}}`
)

// fakeCeX serves every country under /{country}/boxes/{ean}/detail and records the countries asked.
type fakeCeX struct {
	asked   []string
	blocked bool
}

func (f *fakeCeX) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // country, boxes, ean, detail
	country := parts[0]
	f.asked = append(f.asked, country)

	if f.blocked {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<!DOCTYPE html><title>Just a moment...</title>`))

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")

	if parts[1] == "supercats" {
		w.Write([]byte(`{"response":{"ack":"Success","data":{"superCats":[{"superCatId":1,"superCatFriendlyName":"Juegos"}]},"error":{}}}`))
		return
	}

	ean := parts[2]

	switch {
	case ean == "5030934110075" && country == "es":
		w.Write([]byte(deadSpace3))
	case ean == "5051892123456":
		w.Write([]byte(film))
	default:
		w.Write([]byte(notFound))
	}
}

func TestLookup_learnsTheCountry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a provider that has learned nothing yet", func(t *testing.T) {
		fake := &fakeCeX{}
		srv := httptest.NewServer(fake)
		t.Cleanup(srv.Close)

		p := &Provider{BaseURL: srv.URL + "/%s", Client: srv.Client()}
		settings := schema.Settings{}

		t.Run("WHEN a Spanish edition is scanned", func(t *testing.T) {
			m, err := p.Lookup(ctx, "5030934110075", settings)
			require.NoError(t, err)
			require.Len(t, m, 1)

			t.Run("THEN the game and its platform are found in the Spanish catalog", func(t *testing.T) {
				assert.Equal(t, "Dead Space 3", m[0].Title)
				assert.Equal(t, "Xbox 360", m[0].Platform)
				assert.Equal(t, "Dead Space 3 (2 Discs)", m[0].Raw)
				assert.Empty(t, m[0].ImageURL)
			})

			t.Run("AND the countries are asked one by one until one knows it", func(t *testing.T) {
				assert.Equal(t, []string{"uk", "es"}, fake.asked)
			})

			t.Run("AND the answering country is remembered", func(t *testing.T) {
				assert.Equal(t, "es:1", settings[settingHits])
			})
		})

		t.Run("WHEN another code is scanned after that", func(t *testing.T) {
			fake.asked = nil
			_, err := p.Lookup(ctx, "5030934110075", settings)
			require.NoError(t, err)

			t.Run("THEN the remembered country is asked first", func(t *testing.T) {
				assert.Equal(t, []string{"es"}, fake.asked)
				assert.Equal(t, "es:2", settings[settingHits])
			})
		})

		t.Run("WHEN a code no country knows is scanned", func(t *testing.T) {
			fake.asked = nil
			m, err := p.Lookup(ctx, "0000000000000", settings)

			t.Run("THEN every country is asked once and there is no match, not an error", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, m)
				assert.ElementsMatch(t, Countries, fake.asked)
				assert.Equal(t, "es", fake.asked[0])
			})
		})
	})

	t.Run("GIVEN the user limited lookups to some countries", func(t *testing.T) {
		fake := &fakeCeX{}
		srv := httptest.NewServer(fake)
		t.Cleanup(srv.Close)

		p := &Provider{BaseURL: srv.URL + "/%s", Client: srv.Client()}

		t.Run("WHEN a code is scanned", func(t *testing.T) {
			_, err := p.Lookup(ctx, "0000000000000", schema.Settings{settingCountries: " PT, es, xx, pt "})
			require.NoError(t, err)

			t.Run("THEN only those countries are asked, unknown ones ignored", func(t *testing.T) {
				assert.Equal(t, []string{"pt", "es"}, fake.asked)
			})
		})
	})
}

func TestTest_asksTheFirstCountry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a provider that found games in Spain before", func(t *testing.T) {
		fake := &fakeCeX{}
		srv := httptest.NewServer(fake)
		t.Cleanup(srv.Close)

		p := &Provider{BaseURL: srv.URL + "/%s", Client: srv.Client()}

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			res, err := p.Test(ctx, schema.Settings{settingHits: "es:3"})

			t.Run("THEN one request goes to Spain and there is no quota", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, -1, res.RemainingQuota)
				assert.Equal(t, []string{"es"}, fake.asked)
			})
		})
	})
}

func TestLookup_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN CeX answers with a bot check page", func(t *testing.T) {
		fake := &fakeCeX{blocked: true}
		srv := httptest.NewServer(fake)
		t.Cleanup(srv.Close)

		p := &Provider{BaseURL: srv.URL + "/%s", Client: srv.Client()}

		t.Run("WHEN a code is scanned", func(t *testing.T) {
			_, err := p.Lookup(ctx, "5030934110075", schema.Settings{})

			t.Run("THEN it fails with ErrBlocked after one request, without trying every country", func(t *testing.T) {
				require.ErrorIs(t, err, ErrBlocked)
				assert.Len(t, fake.asked, 1)
			})
		})
	})

	t.Run("GIVEN CeX answers with a bot check page to the connection test", func(t *testing.T) {
		srv := httptest.NewServer(&fakeCeX{blocked: true})
		t.Cleanup(srv.Close)

		p := &Provider{BaseURL: srv.URL + "/%s", Client: srv.Client()}

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			_, err := p.Test(ctx, schema.Settings{})

			t.Run("THEN it reports ErrBlocked", func(t *testing.T) {
				require.ErrorIs(t, err, ErrBlocked)
			})
		})
	})

	t.Run("GIVEN a code that CeX sells as a film", func(t *testing.T) {
		srv := httptest.NewServer(&fakeCeX{})
		t.Cleanup(srv.Close)

		p := &Provider{BaseURL: srv.URL + "/%s", Client: srv.Client()}

		t.Run("WHEN it is scanned", func(t *testing.T) {
			m, err := p.Lookup(ctx, "5051892123456", schema.Settings{})

			t.Run("THEN it is not offered as a game", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, m)
			})
		})
	})
}
