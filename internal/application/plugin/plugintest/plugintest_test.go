package plugintest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
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

// misfiledPrices is a price provider described as a barcode provider, without a default place.
type misfiledPrices struct{}

func (misfiledPrices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             "misfiled",
		Kind:           provider.KindBarcode,
		Name:           "Misfiled",
		DescriptionKey: "providers.misfiled.description",
	}
}
func (misfiledPrices) Test(context.Context, schema.Settings) error { return nil }
func (misfiledPrices) Estimate(context.Context, schema.Settings, game.Barcode) (game.Estimate, error) {
	return game.Estimate{}, valuation.ErrNotListed
}

func TestProblems_valuations(t *testing.T) {
	t.Run("GIVEN a plugin whose price provider is misfiled", func(t *testing.T) {
		tr := Translations{"en": {"providers.misfiled.description": true}}
		problems := Problems(plugin.Plugin{
			ID:         "misfiled",
			Name:       "Misfiled",
			Valuations: []valuation.Provider{misfiledPrices{}},
		}, tr)

		t.Run("THEN the wrong kind and the missing default place are reported", func(t *testing.T) {
			assert.Len(t, problems, 2)
			assert.Contains(t, problems[0]+problems[1], "valuation")
		})
	})
}

type recipeSource struct{ recipe schema.SignInRecipe }

func (r recipeSource) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           "recipe",
		Name:           "Recipe",
		DescriptionKey: "sources.recipe.description",
		Fields: schema.Fields{{
			Key:      "session",
			LabelKey: "sources.recipe.session",
			Kind:     schema.FieldSecret,
			Required: true,
			SignIn:   &r.recipe,
		}},
	}
}
func (recipeSource) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	return nil, nil, nil
}
func (recipeSource) Test(context.Context, source.Settings) error { return nil }

func TestProblems_recipes(t *testing.T) {
	tr := Translations{"en": {"sources.recipe.description": true, "sources.recipe.session": true}}
	check := func(r schema.SignInRecipe) []string {
		return Problems(plugin.Plugin{
			ID:      "recipe",
			Name:    "Recipe",
			Sources: []sync.Provider{recipeSource{r}},
		}, tr)
	}

	t.Run("GIVEN a recipe reading another host THEN it is reported", func(t *testing.T) {
		r := schema.SignInRecipe{
			Version: 1,
			Open:    "https://a.example.com/",
			When:    &schema.When{URLPrefix: "https://a.example.com/"},
			Capture: schema.Capture{Cookie: &schema.CookieCapture{
				URL:  "https://b.example.com/",
				Name: "s",
			}},
		}
		assert.NotEmpty(t, check(r))
	})

	t.Run("GIVEN a cookie recipe with no readiness condition THEN it is reported", func(t *testing.T) {
		r := schema.SignInRecipe{
			Version: 1,
			Open:    "https://a.example.com/",
			Capture: schema.Capture{Cookie: &schema.CookieCapture{
				URL:  "https://a.example.com/",
				Name: "s",
			}},
		}
		assert.NotEmpty(t, check(r))
	})

	t.Run("GIVEN a sound recipe THEN nothing is reported", func(t *testing.T) {
		r := schema.SignInRecipe{
			Version: 1,
			Open:    "https://a.example.com/",
			When:    &schema.When{URLPrefix: "https://a.example.com/"},
			Capture: schema.Capture{Cookie: &schema.CookieCapture{
				URL:  "https://a.example.com/",
				Name: "s",
			}},
		}
		assert.Empty(t, check(r))
	})
}
