package rpc_test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

func TestListSystems(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a signed-in caller", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		client := gamevaultv1connect.NewCatalogServiceClient(http.DefaultClient, c.baseURL)

		t.Run("WHEN it lists the systems", func(t *testing.T) {
			res, err := client.ListSystems(ctx, connect.NewRequest(&pb.ListSystemsRequest{}))
			require.NoError(t, err)

			t.Run("THEN it gets the whole catalog in order, PC first, with makers and aliases", func(t *testing.T) {
				require.Len(t, res.Msg.Systems, len(game.Systems()))
				assert.Equal(t, "PC", res.Msg.Systems[0].Name)

				i := slices.IndexFunc(res.Msg.Systems, func(s *pb.SystemInfo) bool { return s.Name == "SNES" })
				require.GreaterOrEqual(t, i, 0)
				assert.Equal(t, "Nintendo", res.Msg.Systems[i].Maker)
				assert.Contains(t, res.Msg.Systems[i].Aliases, "Super Famicom")
			})
		})
	})
}
