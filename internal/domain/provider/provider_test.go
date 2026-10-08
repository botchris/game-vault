package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/schema"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestConfigureRequiresSettingsToEnable(t *testing.T) {
	d := Descriptor{ID: "thegamesdb", Kind: KindCover, Name: "TheGamesDB",
		Fields: schema.Fields{{Key: "api_key", Kind: schema.FieldSecret, Required: true}}}

	p := New(d, 0, now)
	if p.Enabled() {
		t.Fatal("providers needing credentials start disabled")
	}

	if err := p.Configure(d, true, nil, now); err == nil {
		t.Fatal("cannot enable without the api key")
	}

	if err := p.Configure(d, true, schema.Settings{"api_key": "k"}, now); err != nil || !p.Enabled() {
		t.Fatalf("enable with key: %v", err)
	}
	// Sending the placeholder back keeps the key.
	if err := p.Configure(d, true, schema.Settings{"api_key": schema.SecretPlaceholder}, now); err != nil || p.Settings()["api_key"] != "k" {
		t.Fatalf("placeholder must keep the secret: %v %v", err, p.Settings())
	}
}

func TestReorder(t *testing.T) {
	a := Rehydrate("a", KindCover, true, 0, nil, now)
	b := Rehydrate("b", KindCover, true, 1, nil, now)
	c := Rehydrate("c", KindCover, true, 2, nil, now)
	list := []*Provider{a, b, c}
	Reorder(list, []ID{"c", "a"}, now)

	if list[0] != c || list[1] != a || list[2] != b || c.Priority() != 0 || b.Priority() != 2 {
		t.Fatalf("unexpected order: %s %s %s", list[0].ID(), list[1].ID(), list[2].ID())
	}
}

func TestProvider_UpdateState(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	d := Descriptor{ID: "cex", Kind: KindBarcode, Name: "CeX", Fields: schema.Fields{
		{Key: "countries", Kind: schema.FieldText},
		{Key: "country_hits", Kind: schema.FieldState},
	}}

	t.Run("GIVEN a provider the user limited to some countries", func(t *testing.T) {
		p := Rehydrate("cex", KindBarcode, true, 0, schema.Settings{"countries": "es,uk"}, now)

		t.Run("WHEN a lookup writes what it learned into a copy of the settings", func(t *testing.T) {
			settings := p.Settings()
			settings["country_hits"] = "es:1"
			settings["countries"] = "changed by mistake"

			changed := p.UpdateState(d, settings, now)

			t.Run("THEN the state is kept and the user's settings are not touched", func(t *testing.T) {
				assert.True(t, changed)
				assert.Equal(t, schema.Settings{"countries": "es,uk", "country_hits": "es:1"}, p.Settings())
			})

			t.Run("AND the same state again is not a change", func(t *testing.T) {
				assert.False(t, p.UpdateState(d, settings, now))
			})
		})
	})
}
