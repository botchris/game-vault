package rpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()

	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil))

	return b.Bytes()
}

// upload posts a photo form; header false leaves out X-Gamevault-Upload.
func upload(ctx context.Context, t *testing.T, base string, photo, thumb []byte, header bool) *http.Response {
	t.Helper()

	var body bytes.Buffer

	mw := multipart.NewWriter(&body)

	for name, data := range map[string][]byte{"photo": photo, "thumb": thumb} {
		if data == nil {
			continue
		}

		fw, err := mw.CreateFormFile(name, name+".jpg")
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
	}

	require.NoError(t, mw.Close())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/media/photos", &body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	if header {
		req.Header.Set("X-Gamevault-Upload", "1")
	}

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })

	return res
}

func TestPhotoUpload(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		photo, thumb := encodeJPEG(t, 64, 48), encodeJPEG(t, 32, 24)

		t.Run("WHEN a JPEG photo and thumbnail are uploaded", func(t *testing.T) {
			res := upload(ctx, t, c.baseURL, photo, thumb, true)
			require.Equal(t, http.StatusOK, res.StatusCode)

			var got struct {
				ID      string `json:"id"`
				TakenAt string `json:"takenAt"`
			}
			require.NoError(t, json.NewDecoder(res.Body).Decode(&got))

			t.Run("THEN it answers the photo's id, and serves the photo and its thumbnail for long", func(t *testing.T) {
				assert.Len(t, got.ID, 64)
				assert.Empty(t, got.TakenAt)

				for path, want := range map[string][]byte{"/media/photos/" + got.ID: photo, "/media/photos/" + got.ID + "/thumb": thumb} {
					r, err := http.Get(c.baseURL + path)
					require.NoError(t, err)

					data, _ := io.ReadAll(r.Body)
					r.Body.Close()
					assert.Equal(t, want, data)
					assert.Equal(t, "image/jpeg", r.Header.Get("Content-Type"))
					assert.Contains(t, r.Header.Get("Cache-Control"), "immutable")
				}
			})

			t.Run("AND uploading it again gives the same id", func(t *testing.T) {
				again := upload(ctx, t, c.baseURL, photo, thumb, true)

				var second struct {
					ID string `json:"id"`
				}
				require.NoError(t, json.NewDecoder(again.Body).Decode(&second))
				assert.Equal(t, got.ID, second.ID)
			})
		})

		t.Run("WHEN the upload is wrong", func(t *testing.T) {
			var pngBuf bytes.Buffer
			require.NoError(t, png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 8, 8))))

			cases := map[string]*http.Response{
				"a PNG photo":           upload(ctx, t, c.baseURL, pngBuf.Bytes(), thumb, true),
				"a thumbnail too large": upload(ctx, t, c.baseURL, photo, encodeJPEG(t, 600, 10), true),
				"no thumbnail":          upload(ctx, t, c.baseURL, photo, nil, true),
			}

			t.Run("THEN each is refused with a 400 that says what is wrong", func(t *testing.T) {
				for name, res := range cases {
					assert.Equal(t, http.StatusBadRequest, res.StatusCode, name)

					msg, _ := io.ReadAll(res.Body)
					assert.Contains(t, string(msg), "invalid photo", name)
				}
			})
		})

		t.Run("WHEN a form posts without the upload header (a page on another site)", func(t *testing.T) {
			res := upload(ctx, t, c.baseURL, photo, thumb, false)

			t.Run("THEN it is forbidden", func(t *testing.T) {
				assert.Equal(t, http.StatusForbidden, res.StatusCode)
			})
		})

		t.Run("WHEN an unknown or malformed photo is asked for", func(t *testing.T) {
			r1, err := http.Get(c.baseURL + "/media/photos/" + string(bytes.Repeat([]byte("a"), 64)))
			require.NoError(t, err)
			r1.Body.Close()

			r2, err := http.Get(c.baseURL + "/media/photos/..%2F..%2Fgamevault.db")
			require.NoError(t, err)
			r2.Body.Close()

			t.Run("THEN it is not found", func(t *testing.T) {
				assert.Equal(t, http.StatusNotFound, r1.StatusCode)
				assert.Equal(t, http.StatusNotFound, r2.StatusCode)
			})
		})
	})
}

func TestCopyPhotos_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game with a physical copy and two uploaded photos", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Halo 3",
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
				Platform: "Xbox 360",
			}},
		}))
		require.NoError(t, err)

		g := created.Msg.Game
		copyID := g.Copies[0].Id

		ids := make([]string, 0, 2)

		for _, w := range []int{40, 50} {
			res := upload(ctx, t, c.baseURL, encodeJPEG(t, w, w), encodeJPEG(t, 10, 10), true)

			var up struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.NewDecoder(res.Body).Decode(&up))
			ids = append(ids, up.ID)
		}

		t.Run("WHEN they are attached, one with a caption and a date", func(t *testing.T) {
			taken := time.Date(2024, 3, 9, 18, 4, 5, 0, time.UTC)
			res, err := c.games.AddCopyPhotos(ctx, connect.NewRequest(&pb.AddCopyPhotosRequest{
				GameId: g.Id,
				CopyId: copyID,
				Photos: []*pb.NewPhoto{{
					Id:      ids[0],
					Caption: "box",
					TakenAt: timestamppb.New(taken),
				}, {Id: ids[1]}},
			}))
			require.NoError(t, err)

			t.Run("THEN the copy shows them in order", func(t *testing.T) {
				photos := res.Msg.Game.Copies[0].Photos
				require.Len(t, photos, 2)
				assert.Equal(t, "box", photos[0].Caption)
				assert.True(t, taken.Equal(photos[0].TakenAt.AsTime()))
				assert.Nil(t, photos[1].TakenAt)
			})
		})

		t.Run("WHEN a photo that was never uploaded is attached", func(t *testing.T) {
			_, err := c.games.AddCopyPhotos(ctx, connect.NewRequest(&pb.AddCopyPhotosRequest{
				GameId: g.Id,
				CopyId: copyID,
				Photos: []*pb.NewPhoto{{Id: strings.Repeat("c", 64)}},
			}))

			t.Run("THEN it is refused", func(t *testing.T) {
				assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
			})
		})

		t.Run("WHEN a caption is changed and the photos reordered", func(t *testing.T) {
			_, err := c.games.UpdateCopyPhoto(ctx, connect.NewRequest(&pb.UpdateCopyPhotoRequest{
				GameId:  g.Id,
				CopyId:  copyID,
				PhotoId: ids[1],
				Caption: "disc",
			}))
			require.NoError(t, err)

			res, err := c.games.ReorderCopyPhotos(ctx, connect.NewRequest(&pb.ReorderCopyPhotosRequest{
				GameId:   g.Id,
				CopyId:   copyID,
				PhotoIds: []string{ids[1], ids[0]},
			}))
			require.NoError(t, err)

			t.Run("THEN the new order and caption are kept", func(t *testing.T) {
				photos := res.Msg.Game.Copies[0].Photos
				assert.Equal(t, ids[1], photos[0].Id)
				assert.Equal(t, "disc", photos[0].Caption)
			})
		})

		t.Run("WHEN a photo becomes the cover", func(t *testing.T) {
			res, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
				GameId: g.Id,
				System: g.Copies[0].EffectiveSystem,
				Cover:  &pb.SetEditionCoverRequest_PhotoId{PhotoId: ids[0]},
			}))
			require.NoError(t, err)

			t.Run("THEN the game says so and its cover is that photo", func(t *testing.T) {
				assert.Equal(t, ids[0], res.Msg.Game.Editions[0].CoverPhotoId)

				r, err := http.Get(c.baseURL + "/media/covers/" + g.Id)
				require.NoError(t, err)

				data, _ := io.ReadAll(r.Body)
				r.Body.Close()

				p, err := http.Get(c.baseURL + "/media/photos/" + ids[0])
				require.NoError(t, err)

				want, _ := io.ReadAll(p.Body)
				p.Body.Close()
				assert.Equal(t, want, data)
			})

			t.Run("AND editing the title keeps it, but choosing a cover URL for the edition replaces it", func(t *testing.T) {
				kept, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
					Id:    g.Id,
					Title: "Halo 3 (2007)",
				}))
				require.NoError(t, err)
				assert.Equal(t, ids[0], kept.Msg.Game.Editions[0].CoverPhotoId)

				replaced, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
					GameId: g.Id,
					System: g.Copies[0].EffectiveSystem,
					Cover:  &pb.SetEditionCoverRequest_Url{Url: "https://example.test/halo.jpg"},
				}))
				require.NoError(t, err)
				assert.Empty(t, replaced.Msg.Game.Editions[0].CoverPhotoId)
			})
		})

		t.Run("WHEN a photo is removed", func(t *testing.T) {
			res, err := c.games.RemoveCopyPhoto(ctx, connect.NewRequest(&pb.RemoveCopyPhotoRequest{
				GameId:  g.Id,
				CopyId:  copyID,
				PhotoId: ids[1],
			}))
			require.NoError(t, err)

			t.Run("THEN the copy keeps the other one", func(t *testing.T) {
				require.Len(t, res.Msg.Game.Copies[0].Photos, 1)
				assert.Equal(t, ids[0], res.Msg.Game.Copies[0].Photos[0].Id)
			})
		})
	})
}
