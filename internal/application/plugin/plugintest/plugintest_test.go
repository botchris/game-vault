package plugintest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// halfDone is a cover provider written in a hurry: listed as covers but described as barcodes,
// without a default place, with an untranslated field, linking to a store without a name.
type halfDone struct{}

func (halfDone) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             "half",
		Kind:           provider.KindBarcode,
		Name:           "Half",
		DescriptionKey: "providers.half.description",
		Fields: schema.Fields{{
			Key:      "api_key",
			LabelKey: "providers.half.apiKey",
			Kind:     schema.FieldSecret,
		}},
	}
}

func (halfDone) Test(context.Context, schema.Settings) error { return nil }
func (halfDone) Applies(media.CoverQuery) bool               { return true }
func (halfDone) LinkStore() game.Store                       { return game.Store{Key: "half"} }
func (halfDone) Covers(context.Context, media.CoverQuery, schema.Settings) ([]media.CoverCandidate, error) {
	return nil, nil
}

func TestProblems(t *testing.T) {
	tr := Translations{
		"en": {"providers.half.description": true, "providers.half.apiKey": true},
		"es": {"providers.half.description": true},
	}

	t.Run("GIVEN a plugin with an incomplete provider", func(t *testing.T) {
		p := plugin.Plugin{
			ID:     "half",
			Name:   "Half",
			Covers: []media.CoverProvider{halfDone{}},
		}

		t.Run("WHEN it is checked", func(t *testing.T) {
			problems := Problems(p, tr)

			t.Run("THEN each gap is reported with where it is", func(t *testing.T) {
				assert.Len(t, problems, 4)
				assert.Contains(t, problems, `plugin "half", provider "half": is listed as a cover provider but its descriptor says "barcode"`)
				assert.Contains(t, problems, `plugin "half", provider "half": needs a DefaultOrder (its place in the cover chain for new installs)`)
				assert.Contains(t, problems, `plugin "half", provider "half", field api_key: "providers.half.apiKey" is missing in es.json`)
				assert.Contains(t, problems, `plugin "half", provider "half": links to a store without a valid Key ("half") or a Name`)
			})
		})
	})

	t.Run("GIVEN a plugin that adds nothing", func(t *testing.T) {
		t.Run("THEN it is reported", func(t *testing.T) {
			assert.Equal(t, []string{`plugin "empty": adds nothing (no source and no provider)`}, Problems(plugin.Plugin{
				ID:   "empty",
				Name: "Empty",
			}, tr))
		})
	})
}
