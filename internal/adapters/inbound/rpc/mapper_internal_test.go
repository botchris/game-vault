package rpc

import (
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"
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

func TestGameToPB_autoLinks(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a game with a link found automatically", func(t *testing.T) {
		g := game.Rehydrate(game.Snapshot{
			ID: "g1",
			Info: game.Info{
				Title: "Console Quest",
				Links: game.Links{"psn": "PPSA0001"},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
		g.AddAutoLinks(game.Links{game.LinkSteam: "100"}, now)

		t.Run("THEN the API names it, for the UI's badge", func(t *testing.T) {
			got := gameToPB(g)
			assert.Equal(t, []string{game.LinkSteam}, got.AutoLinks)
			assert.Equal(t, "100", got.Links[game.LinkSteam])
		})
	})
}

func TestDetailsFromPB_enums(t *testing.T) {
	t.Run("GIVEN copy details that leave every closed value unspecified", func(t *testing.T) {
		in := &pb.CopyDetails{Contents: []pb.CopyContent{pb.CopyContent_COPY_CONTENT_UNSPECIFIED}}

		t.Run("WHEN they are read", func(t *testing.T) {
			got, err := detailsFromPB(in)
			require.NoError(t, err)

			t.Run("THEN each value is left empty for the domain to fill in", func(t *testing.T) {
				assert.Empty(t, got.Kind)
				assert.Empty(t, got.Status)
				assert.Empty(t, got.Grade)
				assert.Empty(t, got.Region)
				assert.Empty(t, got.Contents.List())
			})
		})
	})

	// A tab left open across an upgrade may send values this server does not know: they are
	// refused, so the copy is not quietly changed to a default.
	for name, in := range map[string]*pb.CopyDetails{
		"kind":    {Kind: pb.CopyKind(99)},
		"status":  {Status: pb.CopyStatus(99)},
		"grade":   {Grade: pb.CopyGrade(99)},
		"region":  {Region: pb.CopyRegion(99)},
		"content": {Contents: []pb.CopyContent{pb.CopyContent_COPY_CONTENT_BOX, pb.CopyContent(99)}},
	} {
		t.Run("GIVEN copy details with an unknown "+name, func(t *testing.T) {
			_, err := detailsFromPB(in)

			t.Run("THEN they are refused as an invalid argument that asks to reload", func(t *testing.T) {
				require.Error(t, err)
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				assert.Contains(t, err.Error(), "reload the page")
			})
		})
	}
}
