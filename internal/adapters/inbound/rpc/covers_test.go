package rpc_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

// getMedia fetches a media URL and returns its status and body.
func getMedia(t *testing.T, url string) (int, []byte) {
	t.Helper()

	res, err := http.Get(url)
	require.NoError(t, err)

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return res.StatusCode, body
}

func TestCovers_pcOnlyGame(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game owned only in PC stores: Steam, a Humble Steam key and GOG", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Hades",
			Links: map[string]string{"steam": "1145360"},
			Copies: []*pb.CopyDetails{
				{
					Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
					Platform: "Steam",
				},
				{
					Kind:     pb.CopyKind_COPY_KIND_KEY,
					Platform: "Steam",
					Status:   pb.CopyStatus_COPY_STATUS_REVEALED,
				},
				{
					Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
					Platform: "GOG",
				},
			},
		}))
		require.NoError(t, err)

		id := created.Msg.Game.Id

		t.Run("WHEN its covers are asked at both addresses", func(t *testing.T) {
			mainStatus, main := getMedia(t, c.baseURL+"/media/covers/"+id)
			pcStatus, pc := getMedia(t, c.baseURL+"/media/covers/"+id+"/PC")

			t.Run("THEN both are Steam's art, as before editions, and box art was never asked", func(t *testing.T) {
				assert.Equal(t, http.StatusOK, mainStatus)
				assert.Equal(t, http.StatusOK, pcStatus)
				assert.Equal(t, png1x1, main)
				assert.Equal(t, png1x1, pc)
				assert.Zero(t, c.boxart.asked)
			})
		})

		t.Run("WHEN a system it has no copy on is asked THEN it is a 404 that must not be cached", func(t *testing.T) {
			res, err := http.Get(c.baseURL + "/media/covers/" + id + "/PS3")
			require.NoError(t, err)
			res.Body.Close()
			assert.Equal(t, http.StatusNotFound, res.StatusCode)
			assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
		})
	})
}

func TestCovers_escapedSystem(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game whose only copy is on a system with a space and a slash", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:    "Halo 3",
			CoverUrl: "https://boxart.test/chosen.png",
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
				Platform: "Xbox 360/S",
			}},
		}))
		require.NoError(t, err)

		t.Run("WHEN its edition's cover is asked with the system escaped", func(t *testing.T) {
			status, body := getMedia(t, c.baseURL+"/media/covers/"+created.Msg.Game.Id+"/Xbox%20360%2FS")

			t.Run("THEN the address reaches that edition", func(t *testing.T) {
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, png1x1, body)
			})
		})
	})
}
