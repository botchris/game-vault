package rpc_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func editionSystems(g *pb.Game) []string {
	out := make([]string, 0, len(g.Editions))
	for _, e := range g.Editions {
		out = append(out, e.System)
	}

	return out
}

func TestEditions_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	_, err := c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{
		Id:       "boxart",
		Enabled:  true,
		Settings: map[string]string{"api_key": "k"},
	}))
	require.NoError(t, err)

	t.Run("GIVEN a game with a PS3 disc, an Xbox 360 disc and a Steam copy", func(t *testing.T) {
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Halo 3",
			Links: map[string]string{"steam": "620"},
			Copies: []*pb.CopyDetails{
				{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "PS3",
				},
				{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "Xbox 360",
				},
				{
					Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
					Platform: "Steam",
				},
			},
		}))
		require.NoError(t, err)

		g := created.Msg.Game
		xbox := g.Copies[1]

		t.Run("THEN it lists an edition per system, the main one first, and each copy's system", func(t *testing.T) {
			assert.Equal(t, []string{"PS3", "PC", "Xbox 360"}, editionSystems(g))
			assert.True(t, g.Editions[0].Main)
			assert.Empty(t, g.MainSystem)
			assert.Equal(t, "PS3", g.Copies[0].EffectiveSystem)
			assert.Equal(t, "PC", g.Copies[2].EffectiveSystem)
		})

		t.Run("WHEN the candidates of each edition are listed THEN each gets its own providers", func(t *testing.T) {
			ps3, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{
				GameId: g.Id,
				System: "PS3",
			}))
			require.NoError(t, err)
			require.Len(t, ps3.Msg.Candidates, 1)
			assert.Equal(t, "Box art", ps3.Msg.Candidates[0].ProviderName)

			pc, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{
				GameId: g.Id,
				System: "PC",
			}))
			require.NoError(t, err)
			assert.Len(t, pc.Msg.Candidates, 2, "the Steam store's two images")

			_, err = c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{
				GameId: g.Id,
				System: "Wii",
			}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})

		t.Run("WHEN the Xbox 360 edition gets a cover and becomes the main one", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: g.Id,
				System: "Xbox 360",
				Cover:  &pb.SetEditionCoverRequest_Url{Url: "https://boxart.test/x360.png"},
			}))
			require.NoError(t, err)

			res, err := c.games.SetMainEdition(ctx, connect.NewRequest(&pb.SetMainEditionRequest{
				GameId: g.Id,
				System: "Xbox 360",
			}))
			require.NoError(t, err)

			t.Run("THEN the game says so", func(t *testing.T) {
				got := res.Msg.Game
				assert.Equal(t, "Xbox 360", got.MainSystem)
				assert.Equal(t, []string{"Xbox 360", "PC", "PS3"}, editionSystems(got))
				assert.Equal(t, "https://boxart.test/x360.png", got.Editions[0].CoverUrl)
			})

			t.Run("AND the main cover address serves that edition's image, also at its escaped address", func(t *testing.T) {
				mainStatus, main := getMedia(t, c.baseURL+"/media/covers/"+g.Id)
				editionStatus, edition := getMedia(t, c.baseURL+"/media/covers/"+g.Id+"/Xbox%20360")
				assert.Equal(t, http.StatusOK, mainStatus)
				assert.Equal(t, http.StatusOK, editionStatus)
				assert.Equal(t, edition, main)
			})
		})

		t.Run("WHEN a cover is chosen for a system the game has no copy on THEN it is refused, naming it", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: g.Id,
				System: "Wii",
				Cover:  &pb.SetEditionCoverRequest_Clear{Clear: true},
			}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			assert.Contains(t, err.Error(), "Wii")
		})

		t.Run("WHEN a request names no cover THEN it is refused", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: g.Id,
				System: "PS3",
			}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})

		t.Run("WHEN an override moves the Xbox 360 disc to a system typed with a slash", func(t *testing.T) {
			res, err := c.games.UpdateCopy(ctx, connect.NewRequest(&pb.UpdateCopyRequest{
				GameId: g.Id,
				CopyId: xbox.Id,
				Details: &pb.CopyDetails{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "Xbox 360",
					System:   "Xbox 360/S Slim",
				},
			}))
			require.NoError(t, err)

			got := res.Msg.Game

			t.Run("THEN the Xbox 360 edition, its cover and the main choice are gone, and the copy is on the typed system", func(t *testing.T) {
				assert.Equal(t, []string{"PS3", "PC", "Xbox 360/S Slim"}, editionSystems(got))
				assert.Empty(t, got.MainSystem)
				assert.Equal(t, "Xbox 360/S Slim", got.Copies[1].Details.System)
				assert.Equal(t, "Xbox 360/S Slim", got.Copies[1].EffectiveSystem)
			})

			t.Run("AND that edition's cover is served at its URL-escaped address", func(t *testing.T) {
				_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
					GameId: g.Id,
					System: "Xbox 360/S Slim",
					Cover:  &pb.SetEditionCoverRequest_Url{Url: "https://boxart.test/slim.png"},
				}))
				require.NoError(t, err)

				status, body := getMedia(t, c.baseURL+"/media/covers/"+g.Id+"/"+url.PathEscape("Xbox 360/S Slim"))
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, png1x1, body)
			})
		})
	})
}

func TestEditions_gameWithoutCopies(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})

	t.Run("GIVEN a game without copies", func(t *testing.T) {
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: "Halo 3"}))
		require.NoError(t, err)

		id := created.Msg.Game.Id

		t.Run("THEN it has one main edition on the empty system, without a cover", func(t *testing.T) {
			require.Len(t, created.Msg.Game.Editions, 1)

			e := created.Msg.Game.Editions[0]
			assert.Empty(t, e.System)
			assert.Empty(t, e.CoverUrl)
			assert.True(t, e.Main)
		})

		t.Run("WHEN a bad cover URL is chosen THEN it is refused", func(t *testing.T) {
			_, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: id,
				Cover:  &pb.SetEditionCoverRequest_Url{Url: "file:///etc/passwd"},
			}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})

		t.Run("WHEN a cover is chosen on the empty system", func(t *testing.T) {
			res, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: id,
				Cover:  &pb.SetEditionCoverRequest_Url{Url: "https://boxart.test/chosen.png"},
			}))
			require.NoError(t, err)

			t.Run("THEN the game's edition shows it, and the main address serves it", func(t *testing.T) {
				require.Len(t, res.Msg.Game.Editions, 1)
				assert.Equal(t, "https://boxart.test/chosen.png", res.Msg.Game.Editions[0].CoverUrl)

				status, body := getMedia(t, c.baseURL+"/media/covers/"+id)
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, png1x1, body)
			})
		})

		t.Run("WHEN it gets its first copy THEN that edition takes the cover", func(t *testing.T) {
			res, err := c.games.AddCopy(ctx, connect.NewRequest(&pb.AddCopyRequest{
				GameId: id,
				Details: &pb.CopyDetails{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "PS3",
				},
			}))
			require.NoError(t, err)

			require.Len(t, res.Msg.Game.Editions, 1)
			assert.Equal(t, "PS3", res.Msg.Game.Editions[0].System)
			assert.Equal(t, "https://boxart.test/chosen.png", res.Msg.Game.Editions[0].CoverUrl)
		})
	})
}
