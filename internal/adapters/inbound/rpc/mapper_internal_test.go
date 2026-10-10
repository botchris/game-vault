package rpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
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

func TestFieldValueFromPB_copiesTheRequest(t *testing.T) {
	t.Run("GIVEN request values holding a yes/no, a number and a duration", func(t *testing.T) {
		b := &pb.FieldValue_Bool{Bool: true}
		n := &pb.FieldValue_Number{Number: 5}
		m := &pb.FieldValue_Minutes{Minutes: 90}

		t.Run("WHEN they are mapped and the request changes afterwards", func(t *testing.T) {
			got := fieldValuesFromPB(map[string]*pb.FieldValue{
				"b": {Value: b},
				"n": {Value: n},
				"m": {Value: m},
			})
			b.Bool, n.Number, m.Minutes = false, 6, 91

			t.Run("THEN the domain values do not change", func(t *testing.T) {
				assert.True(t, *got["b"].Bool)
				assert.Equal(t, int64(5), *got["n"].Number)
				assert.Equal(t, int64(90), *got["m"].Minutes)
			})
		})
	})
}

func TestFieldValuesToPB_skipsEmptyValues(t *testing.T) {
	t.Run("GIVEN values with an empty one", func(t *testing.T) {
		values := game.FieldValues{
			"text":  {Text: "x"},
			"empty": {},
			"none":  {Choices: []string{}},
		}

		t.Run("WHEN they are mapped onto the API", func(t *testing.T) {
			got := fieldValuesToPB(values)

			t.Run("THEN only the value with a member is sent, never an empty oneof", func(t *testing.T) {
				require.Len(t, got, 1)
				assert.Equal(t, "x", got["text"].GetText())
			})
		})
	})
}
