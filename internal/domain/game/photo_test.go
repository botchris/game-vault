package game

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pid returns a valid photo id for tests.
func pid(n int) PhotoID { return PhotoID(fmt.Sprintf("%064x", n)) }

func gameWithTwoCopies(t *testing.T) (*Game, ID, ID) {
	t.Helper()

	g, err := New("Halo 3", t0)
	require.NoError(t, err)

	a, err := g.AddCopy(CopyDetails{Kind: KindPhysical}, t0)
	require.NoError(t, err)

	b, err := g.AddCopy(CopyDetails{Kind: KindKey}, t0)
	require.NoError(t, err)

	return g, a.ID, b.ID
}

func TestPhotoID(t *testing.T) {
	t.Run("GIVEN some JPEG bytes", func(t *testing.T) {
		id := PhotoIDOf([]byte("jpeg"))

		t.Run("THEN the id is their SHA-256 in lowercase hex, and parses", func(t *testing.T) {
			assert.Equal(t, PhotoID(fmt.Sprintf("%x", sha256.Sum256([]byte("jpeg")))), id)
			_, err := ParsePhotoID(string(id))
			assert.NoError(t, err)
		})
	})

	for _, bad := range []string{"", "../etc/passwd", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		t.Run("GIVEN the id "+bad+" THEN it is refused", func(t *testing.T) {
			_, err := ParsePhotoID(bad)

			var ve *ValidationError
			assert.ErrorAs(t, err, &ve)
		})
	}
}

func TestPhotos_add(t *testing.T) {
	t.Run("GIVEN a game with a physical copy", func(t *testing.T) {
		g, copyID, _ := gameWithTwoCopies(t)
		now := t0.Add(time.Hour)

		t.Run("WHEN two photos are added, one of them twice", func(t *testing.T) {
			c, err := g.AddPhotos(copyID, []Photo{
				{
					ID:      pid(1),
					Caption: "  front  ",
				},
				{ID: pid(2)},
				{ID: pid(1)},
			}, now)
			require.NoError(t, err)

			t.Run("THEN the copy has them once, in order, with the caption trimmed and the date added", func(t *testing.T) {
				require.Len(t, c.Photos, 2)
				assert.Equal(t, pid(1), c.Photos[0].ID)
				assert.Equal(t, "front", c.Photos[0].Caption)
				assert.Equal(t, now, c.Photos[0].AddedAt)
				assert.Equal(t, now, g.UpdatedAt())
			})

			t.Run("AND adding a photo the copy already has changes nothing", func(t *testing.T) {
				c, err := g.AddPhotos(copyID, []Photo{{ID: pid(2)}}, now)
				require.NoError(t, err)
				assert.Len(t, c.Photos, 2)
			})
		})

		t.Run("WHEN photos would go over 50", func(t *testing.T) {
			many := make([]Photo, 0, MaxPhotosPerCopy)
			for i := 100; i < 100+MaxPhotosPerCopy-1; i++ {
				many = append(many, Photo{ID: pid(i)})
			}

			_, err := g.AddPhotos(copyID, many, now)

			t.Run("THEN they are refused and the copy keeps its photos", func(t *testing.T) {
				var ve *ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Len(t, g.Copies()[0].Photos, 2)
			})
		})

		t.Run("WHEN a caption is longer than 200 characters, or an id is not valid", func(t *testing.T) {
			_, errCaption := g.AddPhotos(copyID, []Photo{{
				ID:      pid(9),
				Caption: strings.Repeat("é", 201),
			}}, now)
			_, errID := g.AddPhotos(copyID, []Photo{{ID: "nope"}}, now)

			t.Run("THEN both are refused", func(t *testing.T) {
				var ve *ValidationError
				assert.ErrorAs(t, errCaption, &ve)
				assert.ErrorAs(t, errID, &ve)
			})
		})

		t.Run("WHEN the copy does not exist", func(t *testing.T) {
			_, err := g.AddPhotos("missing", []Photo{{ID: pid(3)}}, now)

			t.Run("THEN it says so", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrCopyNotFound)
			})
		})
	})
}

func TestPhotos_editReorderRemove(t *testing.T) {
	t.Run("GIVEN a copy with three photos", func(t *testing.T) {
		g, copyID, _ := gameWithTwoCopies(t)
		_, err := g.AddPhotos(copyID, []Photo{{ID: pid(1)}, {ID: pid(2)}, {ID: pid(3)}}, t0)
		require.NoError(t, err)

		t.Run("WHEN a caption is changed", func(t *testing.T) {
			c, err := g.UpdatePhoto(copyID, pid(2), " disc ", t0)
			require.NoError(t, err)

			t.Run("THEN only that photo changes", func(t *testing.T) {
				assert.Equal(t, "disc", c.Photos[1].Caption)
				assert.Empty(t, c.Photos[0].Caption)
			})

			t.Run("OR the photo is not on the copy, and it says so", func(t *testing.T) {
				_, err := g.UpdatePhoto(copyID, pid(9), "x", t0)
				assert.ErrorIs(t, err, ErrPhotoNotFound)
			})
		})

		t.Run("WHEN the photos are reordered", func(t *testing.T) {
			c, err := g.ReorderPhotos(copyID, []PhotoID{pid(3), pid(1), pid(2)}, t0)
			require.NoError(t, err)

			t.Run("THEN they keep their captions in the new order", func(t *testing.T) {
				assert.Equal(t, []PhotoID{pid(3), pid(1), pid(2)}, []PhotoID{c.Photos[0].ID, c.Photos[1].ID, c.Photos[2].ID})
				assert.Equal(t, "disc", c.Photos[2].Caption)
			})

			t.Run("AND an order that is not exactly the copy's photos is refused", func(t *testing.T) {
				for _, ids := range [][]PhotoID{
					{pid(1), pid(2)},
					{pid(1), pid(2), pid(2)},
					{pid(1), pid(2), pid(9)},
				} {
					_, err := g.ReorderPhotos(copyID, ids, t0)

					var ve *ValidationError
					assert.ErrorAs(t, err, &ve)
				}
			})
		})

		t.Run("WHEN a photo is removed", func(t *testing.T) {
			c, err := g.RemovePhoto(copyID, pid(1), t0)
			require.NoError(t, err)

			t.Run("THEN the copy no longer has it", func(t *testing.T) {
				assert.Len(t, c.Photos, 2)

				_, err := g.RemovePhoto(copyID, pid(1), t0)
				assert.ErrorIs(t, err, ErrPhotoNotFound)
			})
		})
	})
}

func TestPhotos_cover(t *testing.T) {
	t.Run("GIVEN a game whose two copies share a photo, and one copy has another", func(t *testing.T) {
		g, a, b := gameWithTwoCopies(t)
		_, err := g.AddPhotos(a, []Photo{{ID: pid(1)}, {ID: pid(2)}}, t0)
		require.NoError(t, err)
		_, err = g.AddPhotos(b, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)

		t.Run("WHEN a photo that no copy has is chosen as the cover", func(t *testing.T) {
			info := g.Info()
			info.CoverPhoto = pid(9)
			_, err := g.UpdateInfo(info, t0)

			t.Run("THEN it is refused", func(t *testing.T) {
				var ve *ValidationError
				assert.ErrorAs(t, err, &ve)
				assert.Empty(t, g.CoverPhoto())
			})
		})

		t.Run("WHEN the shared photo becomes the cover", func(t *testing.T) {
			info := g.Info()
			info.CoverPhoto = pid(1)
			changed, err := g.UpdateInfo(info, t0)
			require.NoError(t, err)

			t.Run("THEN the cover changed", func(t *testing.T) {
				assert.True(t, changed)
				assert.Equal(t, pid(1), g.CoverPhoto())
			})

			t.Run("AND removing it from one copy keeps it as the cover, since the other copy has it", func(t *testing.T) {
				_, err := g.RemovePhoto(a, pid(1), t0)
				require.NoError(t, err)
				assert.Equal(t, pid(1), g.CoverPhoto())
			})

			t.Run("AND removing the copy that still has it clears the cover", func(t *testing.T) {
				_, err := g.RemoveCopy(b, t0)
				require.NoError(t, err)
				assert.Empty(t, g.CoverPhoto())
			})
		})
	})

	t.Run("GIVEN a game without a cover photo and another whose copy has one", func(t *testing.T) {
		g, _, _ := gameWithTwoCopies(t)
		other, c, _ := gameWithTwoCopies(t)
		_, err := other.AddPhotos(c, []Photo{{ID: pid(5)}}, t0)
		require.NoError(t, err)

		info := other.Info()
		info.CoverPhoto = pid(5)
		_, err = other.UpdateInfo(info, t0)
		require.NoError(t, err)

		t.Run("WHEN the first absorbs the second", func(t *testing.T) {
			g.Absorb(other, t0)

			t.Run("THEN the photos come along and the cover photo is adopted", func(t *testing.T) {
				assert.Equal(t, []PhotoID{pid(5)}, g.PhotoIDs())
				assert.Equal(t, pid(5), g.CoverPhoto())
			})
		})
	})
}

func TestPhotos_aggregateIsolation(t *testing.T) {
	t.Run("GIVEN a copy with a photo", func(t *testing.T) {
		g, a, _ := gameWithTwoCopies(t)
		_, err := g.AddPhotos(a, []Photo{{ID: pid(1)}}, t0)
		require.NoError(t, err)

		t.Run("WHEN a caller changes the photos it was given", func(t *testing.T) {
			copies := g.Copies()
			copies[0].Photos[0].Caption = "hacked"

			t.Run("THEN the game is not changed", func(t *testing.T) {
				assert.Empty(t, g.Copies()[0].Photos[0].Caption)
			})
		})

		t.Run("WHEN the copy's details are edited", func(t *testing.T) {
			_, err := g.UpdateCopy(a, CopyDetails{
				Kind:  KindPhysical,
				Notes: "x",
			}, t0)
			require.NoError(t, err)

			t.Run("THEN its photos stay", func(t *testing.T) {
				assert.Len(t, g.Copies()[0].Photos, 1)
			})
		})
	})
}
