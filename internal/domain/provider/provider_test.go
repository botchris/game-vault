package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestConfigureRequiresSettingsToEnable(t *testing.T) {
	d := Descriptor{
		ID:   "thegamesdb",
		Kind: KindCover,
		Name: "TheGamesDB",
		Fields: schema.Fields{{
			Key:      "api_key",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}

	p := New(d, 0, now)
	assert.False(t, p.Enabled(), "providers needing credentials start disabled")
	require.Error(t, p.Configure(d, true, nil, now), "cannot enable without the api key")

	require.NoError(t, p.Configure(d, true, schema.Settings{"api_key": "k"}, now))
	assert.True(t, p.Enabled(), "enabled with the key")

	// Sending the placeholder back keeps the key.
	require.NoError(t, p.Configure(d, true, schema.Settings{"api_key": schema.SecretPlaceholder}, now))
	assert.Equal(t, "k", p.Settings()["api_key"], "the placeholder must keep the secret")
}

func TestReorder(t *testing.T) {
	a := Rehydrate("a", KindCover, true, 0, nil, now)
	b := Rehydrate("b", KindCover, true, 1, nil, now)
	c := Rehydrate("c", KindCover, true, 2, nil, now)
	list := []*Provider{a, b, c}
	Reorder(list, []ID{"c", "a"}, now)

	assert.Equal(t, []ID{"c", "a", "b"}, []ID{list[0].ID(), list[1].ID(), list[2].ID()})
	assert.Equal(t, 0, c.Priority())
	assert.Equal(t, 2, b.Priority())
}

func TestProvider_UpdateState(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	d := Descriptor{
		ID:   "cex",
		Kind: KindBarcode,
		Name: "CeX",
		Fields: schema.Fields{
			{
				Key:  "countries",
				Kind: schema.FieldText,
			},
			{
				Key:  "country_hits",
				Kind: schema.FieldState,
			},
		},
	}

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

func TestTemporaryErrors(t *testing.T) {
	assert.ErrorIs(t, ErrUnavailable, ErrRateLimited, "an outage is a kind of rate limit")
	assert.NotErrorIs(t, ErrRateLimited, ErrUnavailable)
	assert.ErrorIs(t, BadSettings("the key was rejected: check it"), ErrBadSettings)
	assert.Equal(t, "the key was rejected: check it", BadSettings("the key was rejected: check it").Error())
	assert.NotErrorIs(t, BadSettings("x"), ErrRateLimited)
}
