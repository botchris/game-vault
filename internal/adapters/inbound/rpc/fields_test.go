package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func TestFields_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a physical-only copy field and a game list field with a value", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		sealed, err := c.fields.CreateField(ctx, connect.NewRequest(&pb.CreateFieldRequest{Field: &pb.FieldDefinition{
			Name:  "Sealed",
			Type:  "bool",
			Scope: "copy",
			Kinds: []pb.CopyKind{pb.CopyKind_COPY_KIND_PHYSICAL},
		}}))
		require.NoError(t, err)

		awards, err := c.fields.CreateField(ctx, connect.NewRequest(&pb.CreateFieldRequest{Field: &pb.FieldDefinition{
			Name:    "Awards",
			Type:    "multilist",
			Scope:   "game",
			Choices: []*pb.FieldChoice{{Name: "GOTY"}},
		}}))
		require.NoError(t, err)
		require.Len(t, awards.Msg.Field.Choices, 1)

		sealedID, awardsID, goty := sealed.Msg.Field.Id, awards.Msg.Field.Id, awards.Msg.Field.Choices[0].Id

		t.Run("WHEN a game is created with a sealed physical copy and an award", func(t *testing.T) {
			created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
				Title: "Halo 3",
				Fields: map[string]*pb.FieldValue{
					awardsID: {Value: &pb.FieldValue_Choices{Choices: &pb.ChoiceList{Ids: []string{goty}}}},
				},
				Copies: []*pb.CopyDetails{{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "Xbox 360",
					Fields: map[string]*pb.FieldValue{
						sealedID: {Value: &pb.FieldValue_Bool{Bool: true}},
					},
				}},
			}))
			require.NoError(t, err)

			g := created.Msg.Game

			t.Run("THEN the game and the copy carry their values", func(t *testing.T) {
				assert.Equal(t, []string{goty}, g.Fields[awardsID].GetChoices().GetIds())
				require.Len(t, g.Copies, 1)
				assert.True(t, g.Copies[0].Details.Fields[sealedID].GetBool())
			})

			t.Run("AND updating the game with its values keeps them", func(t *testing.T) {
				res, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
					Id:         g.Id,
					Title:      g.Title,
					PlayStatus: g.PlayStatus,
					Fields:     g.Fields,
				}))
				require.NoError(t, err)
				assert.Equal(t, []string{goty}, res.Msg.Game.Fields[awardsID].GetChoices().GetIds())
			})

			t.Run("AND a key copy refuses the physical-only field", func(t *testing.T) {
				added, err := c.games.AddCopy(ctx, connect.NewRequest(&pb.AddCopyRequest{
					GameId: g.Id,
					Details: &pb.CopyDetails{
						Kind:     pb.CopyKind_COPY_KIND_KEY,
						Platform: "Steam",
						Key:      "AAAAA-BBBBB",
					},
				}))
				require.NoError(t, err)

				keyCopy := added.Msg.Game.Copies[len(added.Msg.Game.Copies)-1]
				_, err = c.games.UpdateCopy(ctx, connect.NewRequest(&pb.UpdateCopyRequest{
					GameId: g.Id,
					CopyId: keyCopy.Id,
					Details: &pb.CopyDetails{
						Kind:     pb.CopyKind_COPY_KIND_KEY,
						Platform: "Steam",
						Key:      "AAAAA-BBBBB",
						Fields:   map[string]*pb.FieldValue{sealedID: {Value: &pb.FieldValue_Bool{Bool: true}}},
					},
				}))
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})

			t.Run("AND deleting the copy field reports its use and clears it", func(t *testing.T) {
				usage, err := c.fields.FieldUsage(ctx, connect.NewRequest(&pb.FieldUsageRequest{Id: sealedID}))
				require.NoError(t, err)
				assert.Equal(t, int32(1), usage.Msg.Copies)

				del, err := c.fields.DeleteField(ctx, connect.NewRequest(&pb.DeleteFieldRequest{Id: sealedID}))
				require.NoError(t, err)
				assert.Equal(t, int32(1), del.Msg.Copies)

				got, err := c.games.GetGame(ctx, connect.NewRequest(&pb.GetGameRequest{Id: g.Id}))
				require.NoError(t, err)
				assert.NotContains(t, got.Msg.Game.Copies[0].Details.Fields, sealedID)

				list, err := c.fields.ListFields(ctx, connect.NewRequest(&pb.ListFieldsRequest{}))
				require.NoError(t, err)
				require.Len(t, list.Msg.Fields, 1)
				assert.Equal(t, "Awards", list.Msg.Fields[0].Name)
			})
		})
	})
}

func TestFields_unknownCopyKind(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		t.Run("WHEN a copy field is created restricted to an unspecified copy kind", func(t *testing.T) {
			_, err := c.fields.CreateField(ctx, connect.NewRequest(&pb.CreateFieldRequest{Field: &pb.FieldDefinition{
				Name:  "Sealed",
				Type:  "bool",
				Scope: "copy",
				Kinds: []pb.CopyKind{pb.CopyKind_COPY_KIND_UNSPECIFIED},
			}}))

			t.Run("THEN it is refused instead of applying to every kind", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})
	})
}

func TestFields_updateConflictingWithValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a number field with a value in a game", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		created, err := c.fields.CreateField(ctx, connect.NewRequest(&pb.CreateFieldRequest{Field: &pb.FieldDefinition{
			Name:  "Weight",
			Type:  "number",
			Scope: "game",
		}}))
		require.NoError(t, err)

		weight := created.Msg.Field
		_, err = c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:  "Halo 3",
			Fields: map[string]*pb.FieldValue{weight.Id: {Value: &pb.FieldValue_Number{Number: 5}}},
		}))
		require.NoError(t, err)

		t.Run("WHEN its decimals change", func(t *testing.T) {
			weight.Decimals = 2
			_, err := c.fields.UpdateField(ctx, connect.NewRequest(&pb.UpdateFieldRequest{Field: weight}))

			t.Run("THEN it is refused as a failed precondition", func(t *testing.T) {
				assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
			})
		})
	})
}
