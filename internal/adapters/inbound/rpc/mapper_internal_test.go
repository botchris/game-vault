package rpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

func TestFieldsToPB_signIn(t *testing.T) {
	t.Run("GIVEN a field whose recipe opens the field's own help link", func(t *testing.T) {
		fields := schema.Fields{{
			Key:     "code",
			Kind:    schema.FieldSecret,
			HelpURL: "https://www.amazon.com/ap/signin?run=42",
			SignIn: &schema.SignInRecipe{
				Version: 1,
				Capture: schema.Capture{Redirect: &schema.RedirectCapture{
					Prefix: "https://www.amazon.com/",
					Param:  "openid.oa2.authorization_code",
				}},
			},
		}, {
			Key:  "plain",
			Kind: schema.FieldText,
		}}

		t.Run("WHEN it is sent to the page", func(t *testing.T) {
			out := fieldsToPB(fields)

			t.Run("THEN the recipe travels as JSON with today's link, and plain fields have none", func(t *testing.T) {
				var r map[string]any
				require.NoError(t, json.Unmarshal([]byte(out[0].SignIn), &r))
				assert.Equal(t, "https://www.amazon.com/ap/signin?run=42", r["open"])
				assert.InDelta(t, 1, r["version"], 0)
				assert.Empty(t, out[1].SignIn)
			})
		})
	})
}
