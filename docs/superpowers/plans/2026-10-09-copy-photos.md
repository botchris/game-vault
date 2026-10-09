# Photos of copies — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users attach photos to any copy (caption, order, date taken, one as the game's cover), keep them in a content-addressed store that backups share without duplicating, and rebuild the image viewer (buttons, zoom, full screen, swipe) for screenshots and photos alike.

**Architecture:** The domain gains `PhotoID`, `Photo`, `Copy.Photos` and `Info.CoverPhoto`, changed only through `Game` methods. A new `photostore` adapter keeps `config/photos/<2 hex>/<sha256>.jpg` (+ `-thumb.jpg`) and an `Archive` that hard-links them into `config/backups/photos/`. The media service validates uploads (JPEG, sizes, EXIF date), serves photos, prunes unreferenced files and uses a cover photo as the cover; the catalog service attaches, captions, reorders and removes photos; the system service writes a `.photos` list per backup and trims the shared store on rotation. The browser resizes photos on a canvas and copies the original EXIF segment into the result.

**Tech Stack:** Go 1.26 (stdlib `image/jpeg`, `crypto/sha256`, `encoding/binary`), SQLite JSON documents, ConnectRPC/buf, React 19 + TypeScript, Node 24 test runner (type stripping) for the pure EXIF helpers, i18next, testify.

**Spec:** `docs/superpowers/specs/2026-10-09-copy-photos-design.md`

## Global Constraints

- Everything runs in the toolchain container through Task (`task lint`, `task test`, `task generate`, `task go -- test ./pkg/ -run X -v`); never Go or Node on the host.
- Load the `write-go` skill before writing Go. `task lint` enforces one struct field per line (`tools/fieldlines`), blank lines between documented members and docs on interface methods (`tools/docspacing`), golangci-lint.
- Go tests: GIVEN / WHEN / THEN subtests with testify; `context.WithTimeout(t.Context(), 10*time.Second)`, never a bare context passed to the code under test; files under `t.TempDir()`.
- No SQL migration: the game document stays at version 2 (fields are only added).
- Never open `config/`; real-data checks use `task test-server` copies.
- Repository text is English; every UI string goes through `t()` with keys in both `en.json` and `es.json` (`task i18n`).
- Photo limits (verbatim from the spec): at most 50 photos per copy; caption at most 200 characters; browser output at most 2560 px JPEG quality 0.85 and a 400 px thumbnail quality 0.8; server accepts a photo up to 8000 px and 15 MB and a thumbnail up to 512 px; unreferenced files are pruned when older than one day.
- Uploads require the `X-Gamevault-Upload` header.
- Motion: open/close fade + scale 0.97 → 1, ≤ 240 ms, `--ease-out`; image changes not animated; `prefers-reduced-motion` collapses it.
- Branch `feature/copy-photos`, one PR to `main`, no tags.

## Review Focus

- A photo already stored long ago, unreferenced, and uploaded again must not be pruned between the upload and the attach: `Put` refreshes the files' modification time (test in Task 3).
- Removing a photo (or a copy) that is the cover must clear the cover photo only when no other copy of the game still has that photo (test in Task 1).
- Editing the game's title or links must keep its cover photo; changing the custom cover URL must clear it (test in Task 5).
- A cross-site form post to `/media/photos` from a browser on a trusted network must be refused: no `X-Gamevault-Upload` header → 403 (test in Task 4).
- A malformed, truncated or hostile JPEG/EXIF (bad offsets, huge counts, `0000:00:00` dates) must never panic the server or the browser helper and simply yields "no date" (tests in Task 4 and Task 7).

---

## File structure

| File | Responsibility |
| --- | --- |
| `internal/domain/game/photo.go` (create), `photo_test.go` (create) | `PhotoID`, `Photo`, photo methods of `Game`, cover photo rules |
| `internal/domain/game/game.go`, `copy.go`, `values.go` (modify) | `Copy.Photos`, `Info.CoverPhoto`, deep-copied `Copies()`, `ErrPhotoNotFound` |
| `internal/adapters/outbound/sqlite/docs.go`, `docs_test.go` (modify) | `photos` and `coverPhoto` in the game document |
| `internal/adapters/outbound/photostore/store.go`, `archive.go`, `store_test.go` (create) | Content-addressed files, prune, size, backup archive |
| `internal/application/media/photos.go`, `exif.go`, `exif_test.go`, `photos_test.go` (create) | `PhotoStore` port, upload checks, EXIF date, prune job |
| `internal/application/media/service.go` (modify) | `photos` dependency, cover from the cover photo |
| `internal/adapters/inbound/rpc/photo_handler.go` (create), `server.go`, `server_test.go`, `photos_test.go` (create) | `POST /media/photos`, `GET /media/photos/{id}[/thumb]`, tests |
| `proto/gamevault/v1/game.proto`, `system.proto` (modify) + generated code | Photo API, backup photo counts |
| `internal/application/catalog/service.go`, `service_test.go` (modify/create) | Photo use cases, cover photo kept on edit |
| `internal/adapters/inbound/rpc/game_handler.go`, `mapper.go`, `system_handler.go` (modify) | RPC handlers and mapping |
| `internal/application/system/service.go`, `service_test.go` (modify) | Backup photo lists, shared store trimming |
| `internal/config/config.go`, `cmd/gamevault/main.go` (modify) | `PhotosDir`, wiring, cleanup job |
| `web/src/lib/exif.ts`, `web/src/lib/photos.ts`, `web/tests/exif.test.mjs` (create), `Taskfile.yml` (modify) | EXIF splicing, resize + upload, Node test |
| `web/src/components/Lightbox.tsx` (rewrite), `Icon.tsx` (modify), `web/src/features/library/GameSheet.tsx` (modify) | The viewer |
| `web/src/features/library/CopyPhotos.tsx` (create), `GameDetail.tsx`, `web/src/features/system/SystemPage.tsx`, `web/src/api/client.ts`, `web/src/styles.css`, `web/src/i18n/locales/{en,es}.json` (modify) | Copy photo UI, backups column |
| `docs/technical.md`, `.claude/docs/ui.md`, `.claude/memory/data-model.md`, `README.md` (modify) | Docs |

---

### Task 1: Domain — photos on copies and the cover photo

**Files:**
- Create: `internal/domain/game/photo.go`, `internal/domain/game/photo_test.go`
- Modify: `internal/domain/game/game.go`, `internal/domain/game/copy.go`, `internal/domain/game/values.go`

**Interfaces:**
- Produces:
  - `type PhotoID string`; `func PhotoIDOf(jpeg []byte) PhotoID`; `func ParsePhotoID(s string) (PhotoID, error)`.
  - `type Photo struct { ID PhotoID; Caption string; TakenAt time.Time; AddedAt time.Time }`.
  - `const MaxPhotosPerCopy = 50`.
  - `Copy.Photos []Photo`; `Info.CoverPhoto PhotoID`; `func (g *Game) CoverPhoto() PhotoID`.
  - `func (g *Game) AddPhotos(copyID ID, photos []Photo, now time.Time) (Copy, error)`
  - `func (g *Game) UpdatePhoto(copyID ID, photoID PhotoID, caption string, now time.Time) (Copy, error)`
  - `func (g *Game) RemovePhoto(copyID ID, photoID PhotoID, now time.Time) (Copy, error)`
  - `func (g *Game) ReorderPhotos(copyID ID, ids []PhotoID, now time.Time) (Copy, error)`
  - `func (g *Game) PhotoIDs() []PhotoID` (distinct, in copy order).
  - `var ErrPhotoNotFound = errors.New("photo not found")`.

- [ ] **Step 1: Write the failing tests** — `internal/domain/game/photo_test.go`:

```go
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
				{ID: pid(1), Caption: "  front  "},
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
			_, errCaption := g.AddPhotos(copyID, []Photo{{ID: pid(9), Caption: strings.Repeat("é", 201)}}, now)
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
			_, err := g.UpdateCopy(a, CopyDetails{Kind: KindPhysical, Notes: "x"}, t0)
			require.NoError(t, err)

			t.Run("THEN its photos stay", func(t *testing.T) {
				assert.Len(t, g.Copies()[0].Photos, 1)
			})
		})
	})
}
```


- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/domain/game/ -run 'TestPhoto' -v`
Expected: FAIL (build errors: `PhotoID`, `AddPhotos`… undefined).

- [ ] **Step 3: Implement** — `internal/domain/game/photo.go`:

```go
package game

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxPhotosPerCopy is how many photos one copy can have.
const MaxPhotosPerCopy = 50

// maxCaptionLen is the longest photo caption, in characters.
const maxCaptionLen = 200

var rePhotoID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PhotoID identifies a photo by the SHA-256 of its stored JPEG, in lowercase hex. The photo's files
// are named after it, so the same image is stored once however many copies show it.
type PhotoID string

// PhotoIDOf returns the id of a stored JPEG.
func PhotoIDOf(jpeg []byte) PhotoID {
	sum := sha256.Sum256(jpeg)

	return PhotoID(hex.EncodeToString(sum[:]))
}

// ParsePhotoID validates a photo id received from outside.
func ParsePhotoID(s string) (PhotoID, error) {
	if !rePhotoID.MatchString(s) {
		return "", invalid("photo id %q is not valid", s)
	}

	return PhotoID(s), nil
}

// Photo is a picture of a copy the user uploaded.
type Photo struct {
	ID PhotoID

	// Caption is optional, at most 200 characters.
	Caption string

	// TakenAt is the camera's clock reading from the photo's metadata (EXIF has no zone), stored as
	// UTC; zero when unknown.
	TakenAt time.Time
	AddedAt time.Time
}

func normalizeCaption(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxCaptionLen {
		return "", invalid("a caption has at most %d characters", maxCaptionLen)
	}

	return s, nil
}

func photoIndex(photos []Photo, id PhotoID) int {
	return slices.IndexFunc(photos, func(p Photo) bool { return p.ID == id })
}

// AddPhotos appends photos to a copy. A photo the copy already has is not added again; a copy has
// at most MaxPhotosPerCopy photos.
func (g *Game) AddPhotos(copyID ID, photos []Photo, now time.Time) (Copy, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return Copy{}, ErrCopyNotFound
	}

	all := slices.Clone(g.copies[i].Photos)

	for _, p := range photos {
		if _, err := ParsePhotoID(string(p.ID)); err != nil {
			return Copy{}, err
		}

		if photoIndex(all, p.ID) >= 0 {
			continue
		}

		caption, err := normalizeCaption(p.Caption)
		if err != nil {
			return Copy{}, err
		}

		all = append(all, Photo{
			ID:      p.ID,
			Caption: caption,
			TakenAt: p.TakenAt.UTC(),
			AddedAt: now,
		})
	}

	if len(all) > MaxPhotosPerCopy {
		return Copy{}, invalid("a copy can have at most %d photos", MaxPhotosPerCopy)
	}

	g.copies[i].Photos = all

	return g.touchCopy(i, now), nil
}

// UpdatePhoto changes the caption of one of a copy's photos.
func (g *Game) UpdatePhoto(copyID ID, photoID PhotoID, caption string, now time.Time) (Copy, error) {
	i, j, err := g.photoAt(copyID, photoID)
	if err != nil {
		return Copy{}, err
	}

	caption, err = normalizeCaption(caption)
	if err != nil {
		return Copy{}, err
	}

	g.copies[i].Photos[j].Caption = caption

	return g.touchCopy(i, now), nil
}

// RemovePhoto removes a photo from a copy. The game's cover photo is cleared when no copy has it
// any more.
func (g *Game) RemovePhoto(copyID ID, photoID PhotoID, now time.Time) (Copy, error) {
	i, j, err := g.photoAt(copyID, photoID)
	if err != nil {
		return Copy{}, err
	}

	g.copies[i].Photos = slices.Delete(slices.Clone(g.copies[i].Photos), j, j+1)
	g.dropOrphanCover()

	return g.touchCopy(i, now), nil
}

// ReorderPhotos puts a copy's photos in the given order, which must name each of them exactly once.
func (g *Game) ReorderPhotos(copyID ID, ids []PhotoID, now time.Time) (Copy, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return Copy{}, ErrCopyNotFound
	}

	current := g.copies[i].Photos
	if len(ids) != len(current) {
		return Copy{}, invalid("the new order must list each of the copy's photos once")
	}

	ordered := make([]Photo, 0, len(ids))

	for _, id := range ids {
		j := photoIndex(current, id)
		if j < 0 || photoIndex(ordered, id) >= 0 {
			return Copy{}, invalid("the new order must list each of the copy's photos once")
		}

		ordered = append(ordered, current[j])
	}

	g.copies[i].Photos = ordered

	return g.touchCopy(i, now), nil
}

// PhotoIDs returns every photo of the game's copies, each once, in copy order.
func (g *Game) PhotoIDs() []PhotoID {
	var out []PhotoID

	for _, c := range g.copies {
		for _, p := range c.Photos {
			if !slices.Contains(out, p.ID) {
				out = append(out, p.ID)
			}
		}
	}

	return out
}

func (g *Game) hasPhoto(id PhotoID) bool {
	for _, c := range g.copies {
		if photoIndex(c.Photos, id) >= 0 {
			return true
		}
	}

	return false
}

// dropOrphanCover clears the cover photo when no copy has that photo any more.
func (g *Game) dropOrphanCover() {
	if g.coverPhoto != "" && !g.hasPhoto(g.coverPhoto) {
		g.coverPhoto = ""
	}
}

func (g *Game) photoAt(copyID ID, photoID PhotoID) (int, int, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return 0, 0, ErrCopyNotFound
	}

	j := photoIndex(g.copies[i].Photos, photoID)
	if j < 0 {
		return 0, 0, ErrPhotoNotFound
	}

	return i, j, nil
}

func (g *Game) touchCopy(i int, now time.Time) Copy {
	g.copies[i].UpdatedAt = now
	g.updatedAt = now

	return g.copies[i].clone()
}

// clone returns the copy with its own photo slice, so callers cannot change the aggregate.
func (c Copy) clone() Copy {
	c.Photos = slices.Clone(c.Photos)

	return c
}
```

In `values.go`, add to the error block (after `ErrCopyNotFound`):

```go
	ErrPhotoNotFound = errors.New("photo not found")
```

In `copy.go`, add the field to `Copy` (after `CopyDetails`, one field per line):

```go
	// Photos are the user's pictures of the copy, in the order they chose.
	Photos     []Photo
```

In `game.go`:
- Add `coverPhoto PhotoID` to `Game` (after `coverURL`), and to `Info` after `CoverURL`:

```go
	// CoverPhoto is one of the copies' photos used as the cover. It wins over CoverURL.
	CoverPhoto PhotoID
```

- `Rehydrate`: `coverPhoto: info.CoverPhoto,`. `Info()`: `CoverPhoto: g.coverPhoto,`.
- Add the getter:

```go
// CoverPhoto returns the photo used as the cover, empty when there is none.
func (g *Game) CoverPhoto() PhotoID { return g.coverPhoto }
```

- `Copies()`:

```go
// Copies returns a copy of the game's copies, so callers cannot bypass the aggregate.
func (g *Game) Copies() []Copy {
	out := make([]Copy, len(g.copies))
	for i, c := range g.copies {
		out[i] = c.clone()
	}

	return out
}
```

- `UpdateInfo`, after normalizing and before assigning:

```go
	if i.CoverPhoto != "" && !g.hasPhoto(i.CoverPhoto) {
		return false, invalid("the cover photo must be a photo of one of the game's copies")
	}

	coverChanged = i.CoverURL != g.coverURL || i.CoverPhoto != g.coverPhoto || !i.Links.Equal(g.links)
	g.title, g.links, g.notes, g.coverURL, g.coverPhoto = i.Title, i.Links, i.Notes, i.CoverURL, i.CoverPhoto
```

- `RemoveCopy`: after removing, `g.dropOrphanCover()`. `RemoveCopiesFromSource`: after `g.copies = kept`, `g.dropOrphanCover()`.
- `Absorb`: after the `coverURL` fallback:

```go
	if g.coverPhoto == "" {
		g.coverPhoto = other.coverPhoto
	}
```

- [ ] **Step 4: Run the domain tests**

Run: `task go -- test ./internal/domain/game/ -v`
Expected: PASS (new and old tests).

- [ ] **Step 5: Lint and commit**

Run: `task lint` (0 issues), then:

```bash
git add internal/domain/game
git commit -m "Domain: photos on copies and a cover photo"
```

---

### Task 2: Documents — store photos and the cover photo

**Files:**
- Modify: `internal/adapters/outbound/sqlite/docs.go`, `internal/adapters/outbound/sqlite/docs_test.go`

**Interfaces:**
- Consumes: `game.Photo`, `game.PhotoID`, `Copy.Photos`, `Info.CoverPhoto` (Task 1).
- Produces: game documents with `coverPhoto` and per-copy `photos` (`[{id, caption, takenAt, addedAt}]`), still version 2.

- [ ] **Step 1: Write the failing test** — append to `docs_test.go`:

```go
func TestDocuments_photos(t *testing.T) {
	t.Run("GIVEN a game with a photographed copy, one photo without a date, and a cover photo", func(t *testing.T) {
		id1, id2 := game.PhotoID(strings.Repeat("a", 64)), game.PhotoID(strings.Repeat("b", 64))
		g := game.Rehydrate("g1", game.Info{
			Title:      "Halo 3",
			CoverPhoto: id2,
		}, []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:   game.KindPhysical,
				Status: game.StatusOwned,
			},
			Photos: []game.Photo{
				{
					ID:      id1,
					Caption: "box",
					TakenAt: docTime.Add(-time.Hour),
					AddedAt: docTime,
				},
				{
					ID:      id2,
					AddedAt: docTime,
				},
			},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the photos and the cover photo come back, as version 2", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Equal(t, id2, got.CoverPhoto())
				assert.Contains(t, raw, `"v":2`)
			})

			t.Run("AND a photo without a date has no takenAt, and a copy without photos no photos field", func(t *testing.T) {
				assert.Equal(t, 1, strings.Count(raw, `"takenAt"`))

				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"photos"`)
				assert.NotContains(t, plain, `"coverPhoto"`)
			})
		})
	})
}
```

Add `"strings"` to the imports.

- [ ] **Step 2: Run it to see it fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestDocuments_photos -v`
Expected: FAIL (photos and cover photo are lost).

- [ ] **Step 3: Implement** in `docs.go`:

`gameDoc`, after `CoverURL`:

```go
	CoverPhoto string     `json:"coverPhoto,omitempty"`
```

`copyDoc`, after `Notes`:

```go
	Photos        []photoDoc `json:"photos,omitempty"`
```

New type after `copyDoc`:

```go
// photoDoc is the stored form of a game.Photo.
type photoDoc struct {
	ID      string `json:"id"`
	Caption string `json:"caption,omitempty"`
	TakenAt string `json:"takenAt,omitempty"`
	AddedAt string `json:"addedAt"`
}
```

`encodeGame`: `CoverPhoto: string(info.CoverPhoto),` in `gameDoc`, and `Photos: photoDocs(c.Photos),` in each `copyDoc`. `decodeGame`: `Photos: photosOf(c.Photos),` in each `game.Copy` and `CoverPhoto: game.PhotoID(doc.CoverPhoto),` in `info`. Helpers at the end of the file:

```go
func photoDocs(photos []game.Photo) []photoDoc {
	if len(photos) == 0 {
		return nil
	}

	out := make([]photoDoc, 0, len(photos))
	for _, p := range photos {
		d := photoDoc{
			ID:      string(p.ID),
			Caption: p.Caption,
			AddedAt: formatTime(p.AddedAt),
		}
		if !p.TakenAt.IsZero() {
			d.TakenAt = formatTime(p.TakenAt)
		}

		out = append(out, d)
	}

	return out
}

func photosOf(docs []photoDoc) []game.Photo {
	if len(docs) == 0 {
		return nil
	}

	out := make([]game.Photo, 0, len(docs))
	for _, d := range docs {
		out = append(out, game.Photo{
			ID:      game.PhotoID(d.ID),
			Caption: d.Caption,
			TakenAt: parseTime(d.TakenAt),
			AddedAt: parseTime(d.AddedAt),
		})
	}

	return out
}
```

Update the comment on `gameDocVersion` only if needed: adding fields keeps version 2 (no change).

- [ ] **Step 4: Run the sqlite tests**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -v`
Expected: PASS. (If `parseTime` returns a non-UTC location for a round trip, compare with `assert.True(t, …Equal…)` per photo instead; `formatTime` writes UTC and `time.Parse` of a `Z` time gives UTC, so `assert.Equal` holds.)

- [ ] **Step 5: Lint and commit**

```bash
git add internal/adapters/outbound/sqlite
git commit -m "Store copy photos and the cover photo in game documents"
```

---

### Task 3: Photo store adapter and backup archive

**Files:**
- Create: `internal/adapters/outbound/photostore/store.go`, `internal/adapters/outbound/photostore/archive.go`, `internal/adapters/outbound/photostore/store_test.go`
- Create: `internal/application/media/photos.go` (only the port, error and `Image` reuse in this task; Task 4 adds the use cases)

**Interfaces:**
- Consumes: `game.PhotoID`, `game.ParsePhotoID` (Task 1); `media.Image` (existing).
- Produces:
  - In `media`: `var ErrNoPhoto = errors.New("no such photo")`; `type PhotoStore interface { Put(id game.PhotoID, photo, thumb []byte) error; Has(id game.PhotoID) bool; Open(id game.PhotoID, thumb bool) (Image, error); Prune(keep map[game.PhotoID]bool, before time.Time) (int, error) }`.
  - `photostore.Open(root string) (*Store, error)`; `(*Store).Put/Has/Open/Prune` (implements `media.PhotoStore`; `Prune` with a zero `before` deletes regardless of age); `(*Store).Size() (int64, error)`.
  - `photostore.NewArchive(live, backup *Store) *Archive`; `(*Archive).Add(ids []game.PhotoID) error`; `(*Archive).Retain(keep map[game.PhotoID]bool) error`; `(*Archive).Size() (int64, error)` (implements `system.PhotoArchive`, Task 6).

- [ ] **Step 1: Add the port** — `internal/application/media/photos.go`:

```go
package media

import (
	"errors"
	"time"

	"gamevault/internal/domain/game"
)

// ErrNoPhoto means there is no stored photo with that id.
var ErrNoPhoto = errors.New("no such photo")

// PhotoStore is the port that keeps the photos users upload of their copies, named after their
// content so each image is stored once.
type PhotoStore interface {
	// Put stores a photo and its thumbnail under id. When they are already stored, it keeps them and
	// marks them as just written, so Prune does not delete them before they are attached.
	Put(id game.PhotoID, photo, thumb []byte) error

	// Has reports whether the photo and its thumbnail are stored.
	Has(id game.PhotoID) bool

	// Open returns the photo, or its thumbnail, or ErrNoPhoto.
	Open(id game.PhotoID, thumb bool) (Image, error)

	// Prune deletes the photos not in keep whose files were written before before (any age when
	// before is zero), and returns how many photos it deleted.
	Prune(keep map[game.PhotoID]bool, before time.Time) (int, error)
}
```

- [ ] **Step 2: Write the failing tests** — `internal/adapters/outbound/photostore/store_test.go`:

```go
package photostore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

var _ media.PhotoStore = (*Store)(nil)

func put(t *testing.T, s *Store, data string) game.PhotoID {
	t.Helper()

	id := game.PhotoIDOf([]byte(data))
	require.NoError(t, s.Put(id, []byte(data), []byte("thumb of "+data)))

	return id
}

func age(t *testing.T, s *Store, id game.PhotoID, at time.Time) {
	t.Helper()

	for _, thumb := range []bool{false, true} {
		require.NoError(t, os.Chtimes(s.path(id, thumb), at, at))
	}
}

func TestStore(t *testing.T) {
	t.Run("GIVEN an empty store", func(t *testing.T) {
		s, err := Open(t.TempDir())
		require.NoError(t, err)

		t.Run("WHEN a photo is put", func(t *testing.T) {
			id := put(t, s, "photo one")

			t.Run("THEN it is stored under its id's first two characters, with its thumbnail", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(s.root, string(id)[:2], string(id)+".jpg"))
				assert.FileExists(t, filepath.Join(s.root, string(id)[:2], string(id)+"-thumb.jpg"))
				assert.True(t, s.Has(id))

				img, err := s.Open(id, true)
				require.NoError(t, err)
				assert.Equal(t, "thumb of photo one", string(img.Data))
				assert.Equal(t, "image/jpeg", img.ContentType)
			})

			t.Run("AND no temporary file is left behind", func(t *testing.T) {
				entries, err := os.ReadDir(filepath.Join(s.root, string(id)[:2]))
				require.NoError(t, err)
				assert.Len(t, entries, 2)
			})
		})

		t.Run("WHEN an unknown or malformed id is opened", func(t *testing.T) {
			_, errUnknown := s.Open(game.PhotoIDOf([]byte("nothing")), false)
			_, errBad := s.Open("../../etc/passwd", false)

			t.Run("THEN there is no photo", func(t *testing.T) {
				assert.ErrorIs(t, errUnknown, media.ErrNoPhoto)
				assert.ErrorIs(t, errBad, media.ErrNoPhoto)
				assert.False(t, s.Has("../../etc/passwd"))
			})
		})
	})
}

func TestStore_prune(t *testing.T) {
	t.Run("GIVEN a kept photo, an old unreferenced one, a recent unreferenced one, and an old one uploaded again", func(t *testing.T) {
		s, err := Open(t.TempDir())
		require.NoError(t, err)

		old := time.Now().Add(-48 * time.Hour)
		kept, stale, fresh, again := put(t, s, "kept"), put(t, s, "stale"), put(t, s, "fresh"), put(t, s, "again")
		age(t, s, kept, old)
		age(t, s, stale, old)
		age(t, s, again, old)
		put(t, s, "again") // uploaded again: about to be attached

		t.Run("WHEN files older than a day that nothing references are pruned", func(t *testing.T) {
			n, err := s.Prune(map[game.PhotoID]bool{kept: true}, time.Now().Add(-24*time.Hour))
			require.NoError(t, err)

			t.Run("THEN only the old unreferenced photo goes, thumbnail included", func(t *testing.T) {
				assert.Equal(t, 1, n)
				assert.False(t, s.Has(stale))
				assert.NoFileExists(t, s.path(stale, true))
				assert.True(t, s.Has(kept))
				assert.True(t, s.Has(fresh))
				assert.True(t, s.Has(again))
			})
		})

		t.Run("WHEN pruning with no age limit", func(t *testing.T) {
			_, err := s.Prune(map[game.PhotoID]bool{kept: true}, time.Time{})
			require.NoError(t, err)

			t.Run("THEN every unreferenced photo goes", func(t *testing.T) {
				assert.False(t, s.Has(fresh))
				assert.True(t, s.Has(kept))
			})
		})
	})
}

func TestArchive(t *testing.T) {
	t.Run("GIVEN a live store with two photos and an empty backup store", func(t *testing.T) {
		live, err := Open(t.TempDir())
		require.NoError(t, err)

		backup, err := Open(t.TempDir())
		require.NoError(t, err)

		a, b := put(t, live, "a"), put(t, live, "b")
		arch := NewArchive(live, backup)

		t.Run("WHEN both are archived, and then again", func(t *testing.T) {
			require.NoError(t, arch.Add([]game.PhotoID{a, b}))
			require.NoError(t, arch.Add([]game.PhotoID{a, b}))

			t.Run("THEN the backup store has each once, as hard links of the live files", func(t *testing.T) {
				assert.True(t, backup.Has(a))

				li, err := os.Stat(live.path(a, false))
				require.NoError(t, err)

				bi, err := os.Stat(backup.path(a, false))
				require.NoError(t, err)
				assert.True(t, os.SameFile(li, bi))
			})

			t.Run("AND a photo no backup lists any more is removed from the backup store only", func(t *testing.T) {
				require.NoError(t, arch.Retain(map[game.PhotoID]bool{a: true}))
				assert.False(t, backup.Has(b))
				assert.True(t, live.Has(b))
			})
		})

		t.Run("WHEN hard links are not possible (another file system)", func(t *testing.T) {
			c := put(t, live, "c")
			arch.link = func(string, string) error { return errors.New("cross-device link") }
			require.NoError(t, arch.Add([]game.PhotoID{c}))

			t.Run("THEN the photo is copied", func(t *testing.T) {
				img, err := backup.Open(c, false)
				require.NoError(t, err)
				assert.Equal(t, "c", string(img.Data))
			})
		})

		t.Run("WHEN a listed photo is missing from the live store", func(t *testing.T) {
			err := arch.Add([]game.PhotoID{game.PhotoIDOf([]byte("gone"))})

			t.Run("THEN it is skipped without failing the backup", func(t *testing.T) {
				assert.NoError(t, err)
			})
		})

		t.Run("WHEN the size is asked", func(t *testing.T) {
			n, err := arch.Size()
			require.NoError(t, err)

			t.Run("THEN it counts the backup store's files", func(t *testing.T) {
				assert.Positive(t, n)
			})
		})
	})
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/photostore/ -v`
Expected: FAIL (package does not build).

- [ ] **Step 4: Implement** — `internal/adapters/outbound/photostore/store.go`:

```go
// Package photostore keeps the photos users upload of their copies, named after their content, and
// implements media.PhotoStore:
//
//	config/photos/
//	  3f/
//	    3fa9…e1.jpg        the photo (at most 2560 px), named after its SHA-256
//	    3fa9…e1-thumb.jpg  its thumbnail (at most 400 px)
//
// A stored file never changes (its name is its content's hash), so the same layout also serves as
// the backups' shared store, filled with hard links (see Archive).
package photostore

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

var reFile = regexp.MustCompile(`^([0-9a-f]{64})(-thumb)?\.jpg$`)

// Store implements media.PhotoStore on a directory.
type Store struct {
	root string
}

// Open returns the store in root, creating the directory if needed.
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	return &Store{root: root}, nil
}

func (s *Store) path(id game.PhotoID, thumb bool) string {
	name := string(id) + ".jpg"
	if thumb {
		name = string(id) + "-thumb.jpg"
	}

	return filepath.Join(s.root, string(id)[:2], name)
}

func valid(id game.PhotoID) bool {
	_, err := game.ParsePhotoID(string(id))

	return err == nil
}

// Put implements media.PhotoStore.
func (s *Store) Put(id game.PhotoID, photo, thumb []byte) error {
	if !valid(id) {
		return media.ErrNoPhoto
	}

	now := time.Now()

	for _, f := range []struct {
		thumb bool
		data  []byte
	}{{false, photo}, {true, thumb}} {
		p := s.path(id, f.thumb)
		if _, err := os.Stat(p); err == nil {
			// Already stored: mark it as just written so a prune does not delete it before it is
			// attached (it may be an old photo that nothing referenced any more).
			if err := os.Chtimes(p, now, now); err != nil {
				return err
			}

			continue
		}

		if err := writeAtomic(p, f.data); err != nil {
			return err
		}
	}

	return nil
}

// writeAtomic writes data to a temporary file next to path and renames it, so a reader never sees
// a half-written photo.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one worth reporting
		_ = os.Remove(tmp.Name())

		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())

		return err
	}

	return os.Rename(tmp.Name(), path)
}

// Has implements media.PhotoStore.
func (s *Store) Has(id game.PhotoID) bool {
	if !valid(id) {
		return false
	}

	for _, thumb := range []bool{false, true} {
		if _, err := os.Stat(s.path(id, thumb)); err != nil {
			return false
		}
	}

	return true
}

// Open implements media.PhotoStore.
func (s *Store) Open(id game.PhotoID, thumb bool) (media.Image, error) {
	if !valid(id) {
		return media.Image{}, media.ErrNoPhoto
	}

	data, err := os.ReadFile(s.path(id, thumb))
	if os.IsNotExist(err) {
		return media.Image{}, media.ErrNoPhoto
	}

	if err != nil {
		return media.Image{}, err
	}

	return media.Image{
		Data:        data,
		ContentType: "image/jpeg",
	}, nil
}

// Prune implements media.PhotoStore. Stray temporary files older than before go too.
func (s *Store) Prune(keep map[game.PhotoID]bool, before time.Time) (int, error) {
	n := 0
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		m := reFile.FindStringSubmatch(d.Name())
		temp := strings.HasPrefix(d.Name(), ".tmp-")

		if (m == nil && !temp) || (m != nil && keep[game.PhotoID(m[1])]) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if !before.IsZero() && !info.ModTime().Before(before) {
			return nil
		}

		if err := os.Remove(path); err != nil {
			return err
		}

		if m != nil && m[2] == "" {
			n++
		}

		return nil
	})

	return n, err
}

// Size returns the total size of the stored files in bytes.
func (s *Store) Size() (int64, error) {
	var total int64
	err := filepath.WalkDir(s.root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		total += info.Size()

		return nil
	})

	return total, err
}
```


`internal/adapters/outbound/photostore/archive.go`:

```go
package photostore

import (
	"os"
	"path/filepath"
	"time"

	"gamevault/internal/domain/game"
)

// Archive fills the backups' shared photo store from the live one and implements
// system.PhotoArchive. Each photo is stored there once, however many backups list it.
type Archive struct {
	live   *Store
	backup *Store

	// link makes dst another name of src; os.Link, replaced in tests.
	link func(src, dst string) error
}

// NewArchive returns the archive that copies photos from live into backup.
func NewArchive(live, backup *Store) *Archive {
	return &Archive{
		live:   live,
		backup: backup,
		link:   os.Link,
	}
}

// Add implements system.PhotoArchive. Photos are hard-linked (they never change, so the backup
// costs no space while the live photo exists) and copied when linking is not possible. A photo
// missing from the live store is skipped: there is nothing to keep.
func (a *Archive) Add(ids []game.PhotoID) error {
	for _, id := range ids {
		if !valid(id) {
			continue
		}

		for _, thumb := range []bool{false, true} {
			src, dst := a.live.path(id, thumb), a.backup.path(id, thumb)
			if _, err := os.Stat(dst); err == nil {
				continue
			}

			if _, err := os.Stat(src); os.IsNotExist(err) {
				continue
			}

			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}

			if err := a.link(src, dst); err == nil {
				continue
			}

			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}

			if err := writeAtomic(dst, data); err != nil {
				return err
			}
		}
	}

	return nil
}

// Retain implements system.PhotoArchive.
func (a *Archive) Retain(keep map[game.PhotoID]bool) error {
	_, err := a.backup.Prune(keep, time.Time{})

	return err
}

// Size implements system.PhotoArchive.
func (a *Archive) Size() (int64, error) { return a.backup.Size() }
```


- [ ] **Step 5: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/photostore/ -v`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
git add internal/adapters/outbound/photostore internal/application/media/photos.go
git commit -m "Photo store: content-addressed photos, prune and the backups' shared store"
```

---

### Task 4: Upload, serve and prune photos; cover from a photo

**Files:**
- Create: `internal/application/media/exif.go`, `internal/application/media/exif_test.go`, `internal/adapters/inbound/rpc/photo_handler.go`, `internal/adapters/inbound/rpc/photos_test.go`
- Modify: `internal/application/media/photos.go`, `internal/application/media/service.go`, `internal/adapters/inbound/rpc/server.go`, `internal/adapters/inbound/rpc/server_test.go`, `internal/config/config.go`, `cmd/gamevault/main.go`

**Interfaces:**
- Consumes: `media.PhotoStore`, `media.ErrNoPhoto` (Task 3), `photostore.Open` (Task 3), `game.PhotoIDOf`, `(*game.Game).PhotoIDs`, `(*game.Game).CoverPhoto` (Task 1).
- Produces:
  - `media.NewService(games, providers, store AssetStore, photos PhotoStore, details, fetch, now, log, impls)` — `photos` is new, after `store`; nil disables photos.
  - `var media.ErrInvalidPhoto`; `const media.MaxPhotoBytes = 15 << 20`, `media.MaxThumbBytes = 1 << 20`.
  - `type media.PhotoUpload struct { ID game.PhotoID; TakenAt time.Time }`.
  - `func (s *Service) UploadPhoto(photo, thumb []byte) (PhotoUpload, error)`; `Photo(id game.PhotoID, thumb bool) (Image, error)`; `PrunePhotos(ctx) (int, error)`; `RunPhotoCleanup(ctx, first, every time.Duration)`.
  - HTTP: `POST /media/photos` → `{"id": "…", "takenAt": "RFC 3339"}`; `GET /media/photos/{id}`, `GET /media/photos/{id}/thumb`.
  - `config.Config.PhotosDir()` = `<config>/photos`.
  - Test helper `clients.photosDir` in `server_test.go`.

- [ ] **Step 1: Write the failing EXIF tests** — `internal/application/media/exif_test.go`:

```go
package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tiffWithDates builds the TIFF part of an EXIF segment: IFD0 with DateTime (0x0132) and a pointer
// to the Exif IFD (0x8769), which holds DateTimeOriginal (0x9003). An empty original leaves that
// field zeroed, as some cameras do.
//
//	0  header | 8 IFD0 (2 entries) | 38 Exif IFD (1 entry) | 56 DateTime | 76 DateTimeOriginal
func tiffWithDates(bo binary.ByteOrder, dateTime, original string) []byte {
	b := make([]byte, 96)
	if bo == binary.LittleEndian {
		copy(b, "II")
	} else {
		copy(b, "MM")
	}

	bo.PutUint16(b[2:], 42)
	bo.PutUint32(b[4:], 8)

	entry := func(at int, tag, typ uint16, count, value uint32) {
		bo.PutUint16(b[at:], tag)
		bo.PutUint16(b[at+2:], typ)
		bo.PutUint32(b[at+4:], count)
		bo.PutUint32(b[at+8:], value)
	}

	bo.PutUint16(b[8:], 2)
	entry(10, 0x0132, 2, 20, 56)
	entry(22, 0x8769, 4, 1, 38)
	bo.PutUint16(b[38:], 1)
	entry(40, 0x9003, 2, 20, 76)
	copy(b[56:], dateTime)
	copy(b[76:], original)

	return b
}

// jpegWithExif encodes a small image and inserts an EXIF segment holding tiff after its SOI.
func jpegWithExif(t *testing.T, tiff []byte) []byte {
	t.Helper()

	var enc bytes.Buffer
	require.NoError(t, jpeg.Encode(&enc, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil))

	if tiff == nil {
		return enc.Bytes()
	}

	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	out = append(out, seg...)

	return append(out, enc.Bytes()[2:]...)
}

func TestExifTakenAt(t *testing.T) {
	want := time.Date(2024, 3, 9, 18, 4, 5, 0, time.UTC)

	cases := []struct {
		name string
		tiff []byte
		want time.Time
		ok   bool
	}{
		{"little-endian, with DateTimeOriginal", tiffWithDates(binary.LittleEndian, "2001:01:01 00:00:00", "2024:03:09 18:04:05"), want, true},
		{"big-endian, with DateTimeOriginal", tiffWithDates(binary.BigEndian, "2001:01:01 00:00:00", "2024:03:09 18:04:05"), want, true},
		{"no DateTimeOriginal: falls back to DateTime", tiffWithDates(binary.LittleEndian, "2024:03:09 18:04:05", ""), want, true},
		{"dates unset by the camera", tiffWithDates(binary.BigEndian, "0000:00:00 00:00:00", "0000:00:00 00:00:00"), time.Time{}, false},
		{"no EXIF at all", nil, time.Time{}, false},
	}

	for _, c := range cases {
		t.Run("GIVEN a JPEG with "+c.name+" THEN the date is read as expected", func(t *testing.T) {
			got, ok := exifTakenAt(jpegWithExif(t, c.tiff))
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("GIVEN truncated or corrupted files THEN reading never panics", func(t *testing.T) {
		full := jpegWithExif(t, tiffWithDates(binary.LittleEndian, "2024:03:09 18:04:05", "2024:03:09 18:04:05"))
		for n := 0; n < 200 && n < len(full); n++ {
			assert.NotPanics(t, func() { exifTakenAt(full[:n]) })
		}

		bad := bytes.Clone(full)
		binary.LittleEndian.PutUint32(bad[6+4+6+4:], 0xFFFFFFF0) // IFD0 offset far outside
		assert.NotPanics(t, func() { exifTakenAt(bad) })

		huge := bytes.Clone(full)
		binary.LittleEndian.PutUint16(huge[6+4+6+8:], 0xFFFF) // 65535 IFD0 entries
		assert.NotPanics(t, func() { exifTakenAt(huge) })
	})
}
```

(Offsets in the corruption cases: SOI 2 + APP1 marker 2 + length 2 = 6, then "Exif\0\0" 6 → TIFF starts at 12; IFD0 offset field at TIFF+4 = 16, IFD0 entry count at TIFF+8 = 20. Write them as `12+4` and `12+8` with that comment.)

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/application/media/ -run TestExifTakenAt -v`
Expected: FAIL (`exifTakenAt` undefined).

- [ ] **Step 3: Implement the EXIF reader** — `internal/application/media/exif.go`:

```go
package media

import (
	"encoding/binary"
	"time"
)

// EXIF tags read to date a photo.
const (
	tagDateTime         = 0x0132
	tagExifIFD          = 0x8769
	tagDateTimeOriginal = 0x9003
)

// exifTakenAt reads when a JPEG photo was taken from its EXIF metadata: DateTimeOriginal, or the
// file's DateTime when the camera did not record it. EXIF dates have no zone (cameras write their
// local clock), so the reading is returned as UTC. Malformed metadata yields false, never a panic:
// every offset is checked against the segment.
func exifTakenAt(jpeg []byte) (time.Time, bool) {
	tiff, ok := exifSegment(jpeg)
	if !ok || len(tiff) < 8 {
		return time.Time{}, false
	}

	r := tiffReader{b: tiff}

	switch string(tiff[:2]) {
	case "II":
		r.bo = binary.LittleEndian
	case "MM":
		r.bo = binary.BigEndian
	default:
		return time.Time{}, false
	}

	ifd0, ok := r.u32(4)
	if !ok {
		return time.Time{}, false
	}

	if e, ok := r.entry(ifd0, tagExifIFD); ok {
		if sub, ok := r.u32(e + 8); ok {
			if t, ok := r.dateTime(sub, tagDateTimeOriginal); ok {
				return t, true
			}
		}
	}

	return r.dateTime(ifd0, tagDateTime)
}

// exifSegment returns the TIFF data of the JPEG's EXIF (APP1) segment.
func exifSegment(b []byte) ([]byte, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil, false
	}

	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return nil, false
		}

		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data or end: metadata comes before
			return nil, false
		}

		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return nil, false
		}

		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return seg[6:], true
		}

		i += 2 + n
	}

	return nil, false
}

// tiffReader reads the TIFF structure inside an EXIF segment with bounds checks.
type tiffReader struct {
	b  []byte
	bo binary.ByteOrder
}

func (r tiffReader) u16(off int) (int, bool) {
	if off < 0 || off+2 > len(r.b) {
		return 0, false
	}

	return int(r.bo.Uint16(r.b[off:])), true
}

func (r tiffReader) u32(off int) (int, bool) {
	if off < 0 || off+4 > len(r.b) {
		return 0, false
	}

	v := r.bo.Uint32(r.b[off:])
	if v > uint32(len(r.b)) {
		return 0, false
	}

	return int(v), true
}

// entry returns the position of tag's 12-byte entry in the IFD at ifd.
func (r tiffReader) entry(ifd, tag int) (int, bool) {
	n, ok := r.u16(ifd)
	if !ok {
		return 0, false
	}

	for i := range n {
		e := ifd + 2 + 12*i

		t, ok := r.u16(e)
		if !ok {
			return 0, false
		}

		if t == tag {
			return e, true
		}
	}

	return 0, false
}

// dateTime reads an ASCII date ("2006:01:02 15:04:05") stored out of line by tag in the IFD.
func (r tiffReader) dateTime(ifd, tag int) (time.Time, bool) {
	e, ok := r.entry(ifd, tag)
	if !ok {
		return time.Time{}, false
	}

	off, ok := r.u32(e + 8)
	if !ok || off+19 > len(r.b) {
		return time.Time{}, false
	}

	t, err := time.Parse("2006:01:02 15:04:05", string(r.b[off:off+19]))
	if err != nil {
		return time.Time{}, false
	}

	return t, true
}
```

Note on `u32`: a value larger than the segment can never be a valid offset; returning false there keeps `int` conversion safe on 32-bit builds too. (`DateTimeOriginal` counts are 20 > 4, so the value is always an offset.)

- [ ] **Step 4: Run the EXIF tests**

Run: `task go -- test ./internal/application/media/ -run TestExifTakenAt -v`
Expected: PASS.

- [ ] **Step 5: Add the use cases** — append to `internal/application/media/photos.go` (add imports `bytes`, `context`, `fmt`, `image`, `_ "image/jpeg"`):

```go
// ErrInvalidPhoto wraps the reasons an uploaded photo is refused; the message says what is wrong.
var ErrInvalidPhoto = errors.New("invalid photo")

// Limits of an upload. The browser sends at most 2560 px and 400 px; the margins allow other clients.
const (
	MaxPhotoBytes = 15 << 20
	MaxThumbBytes = 1 << 20
	maxPhotoSide  = 8000
	maxThumbSide  = 512

	// unattachedGrace is how long an unreferenced photo is kept: it may be about to be attached, or
	// have been removed by mistake and be added back.
	unattachedGrace = 24 * time.Hour
)

// PhotoUpload is a stored photo, ready to be attached to copies.
type PhotoUpload struct {
	ID game.PhotoID

	// TakenAt comes from the photo's metadata; zero when unknown.
	TakenAt time.Time
}

// UploadPhoto checks and stores a photo and its thumbnail, both JPEG, and reads when the photo was
// taken. The id is the photo's SHA-256, so uploading the same photo again gives the same id.
func (s *Service) UploadPhoto(photo, thumb []byte) (PhotoUpload, error) {
	if s.photos == nil {
		return PhotoUpload{}, errors.New("photos are not available on this server")
	}

	if err := checkJPEG("photo", photo, MaxPhotoBytes, maxPhotoSide); err != nil {
		return PhotoUpload{}, err
	}

	if err := checkJPEG("thumbnail", thumb, MaxThumbBytes, maxThumbSide); err != nil {
		return PhotoUpload{}, err
	}

	id := game.PhotoIDOf(photo)
	if err := s.photos.Put(id, photo, thumb); err != nil {
		return PhotoUpload{}, fmt.Errorf("storing the photo: %w", err)
	}

	taken, _ := exifTakenAt(photo)

	return PhotoUpload{
		ID:      id,
		TakenAt: taken,
	}, nil
}

func checkJPEG(what string, data []byte, maxBytes, maxSide int) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: the %s is missing", ErrInvalidPhoto, what)
	}

	if len(data) > maxBytes {
		return fmt.Errorf("%w: the %s is larger than %d MB", ErrInvalidPhoto, what, maxBytes>>20)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		return fmt.Errorf("%w: the %s is not a JPEG image", ErrInvalidPhoto, what)
	}

	if cfg.Width > maxSide || cfg.Height > maxSide {
		return fmt.Errorf("%w: the %s is larger than %d pixels", ErrInvalidPhoto, what, maxSide)
	}

	return nil
}

// Photo returns a stored photo or its thumbnail, or ErrNoPhoto.
func (s *Service) Photo(id game.PhotoID, thumb bool) (Image, error) {
	if s.photos == nil {
		return Image{}, ErrNoPhoto
	}

	return s.photos.Open(id, thumb)
}

// PrunePhotos deletes the stored photos no copy shows any more, once they are a day old.
func (s *Service) PrunePhotos(ctx context.Context) (int, error) {
	if s.photos == nil {
		return 0, nil
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return 0, err
	}

	keep := map[game.PhotoID]bool{}

	for _, g := range games {
		for _, id := range g.PhotoIDs() {
			keep[id] = true
		}
	}

	return s.photos.Prune(keep, s.now().Add(-unattachedGrace))
}

// RunPhotoCleanup prunes unused photos after first and then every interval until ctx ends.
func (s *Service) RunPhotoCleanup(ctx context.Context, first, every time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if n, err := s.PrunePhotos(ctx); err != nil {
				s.log.Warn("pruning unused photos", "error", err)
			} else if n > 0 {
				s.log.Info("unused photos deleted", "count", n)
			}

			timer.Reset(every)
		}
	}
}
```

- [ ] **Step 6: Wire `photos` into the media service and the cover** — in `service.go`:
- `Service` gains `photos PhotoStore` (after `store`).
- `NewService(games game.Repository, providers provider.Repository, store AssetStore, photos PhotoStore, details DetailsStore, fetch ImageFetcher, now port.Clock, log *slog.Logger, impls Providers)`; set `photos: photos`; update the doc comment: "photos may be nil: uploads are refused and cover photos ignored."
- In `resolve`, before the custom cover URL block:

```go
	// A photo of the user's own copy chosen as the cover wins over everything.
	if id := g.CoverPhoto(); id != "" && s.photos != nil {
		img, err := s.photos.Open(id, false)
		if err == nil {
			if err := s.store.PutCover(ref, img); err != nil {
				return Image{}, fmt.Errorf("caching cover: %w", err)
			}

			return img, nil
		}

		s.log.Warn("cover photo unavailable, falling back", "game", g.Title(), "error", err)
	}
```

- Update the two callers: `cmd/gamevault/main.go` and `internal/adapters/inbound/rpc/server_test.go` (pass the photo store after the asset store).

- [ ] **Step 7: Config and wiring** — `internal/config/config.go`, after `GameDataDir`:

```go
// PhotosDir holds the photos users upload of their copies (see photostore).
func (c Config) PhotosDir() string { return filepath.Join(c.ConfigDir, "photos") }
```

`main.go`, after `assets` is opened:

```go
	photos, err := photostore.Open(cfg.PhotosDir())
	if err != nil {
		return err
	}
```

pass `photos` to `media.NewService`, and after the details scanner:

```go
	go mediaSvc.RunPhotoCleanup(ctx, time.Hour, 24*time.Hour)
```

Import `gamevault/internal/adapters/outbound/photostore`.

- [ ] **Step 8: Write the failing HTTP tests** — in `server_test.go`, `newServer` opens `photos, err := photostore.Open(filepath.Join(dir, "photos"))`, passes it to `media.NewService`, and `clients` gains `photosDir string` set to that path. Then `internal/adapters/inbound/rpc/photos_test.go`:

```go
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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
```

Check how existing tests in `server_test.go` call `newServer` (the `p sync.Provider` argument) and reuse the same fake (`&fakeProvider{}` or whatever the file defines).

- [ ] **Step 9: Run them to see them fail**

Run: `task go -- test ./internal/adapters/inbound/rpc/ -run TestPhotoUpload -v`
Expected: FAIL (404 on `/media/photos`).

- [ ] **Step 10: Implement the handlers** — `internal/adapters/inbound/rpc/photo_handler.go`:

```go
package rpc

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

// photoUploadHeader must be on every upload. A page on another site cannot add it without a CORS
// preflight this server never grants, so it cannot upload photos through the browser of someone on
// a trusted network (where no session cookie is needed, so SameSite cookies do not protect).
const photoUploadHeader = "X-Gamevault-Upload"

// photoUploadHandler serves POST /media/photos: a multipart form with the parts "photo" and
// "thumb" (JPEG). It answers {"id": "…", "takenAt": "…"}; takenAt is left out when unknown.
func photoUploadHandler(m *media.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(photoUploadHeader) == "" {
			http.Error(w, "uploads need the "+photoUploadHeader+" header", http.StatusForbidden)
			return
		}

		const maxForm = media.MaxPhotoBytes + media.MaxThumbBytes + 1<<20 // parts plus the form's own overhead

		r.Body = http.MaxBytesReader(w, r.Body, maxForm)
		if err := r.ParseMultipartForm(maxForm); err != nil {
			http.Error(w, "invalid photo: the upload is not a form or is too large", http.StatusBadRequest)
			return
		}

		defer func() { _ = r.MultipartForm.RemoveAll() }() // temporary files only; nothing to report

		up, err := m.UploadPhoto(formPart(r, "photo"), formPart(r, "thumb"))
		switch {
		case errors.Is(err, media.ErrInvalidPhoto):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case err != nil:
			http.Error(w, "the photo could not be stored: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp := struct {
			ID      string `json:"id"`
			TakenAt string `json:"takenAt,omitempty"`
		}{
			ID: string(up.ID),
		}
		if !up.TakenAt.IsZero() {
			resp.TakenAt = up.TakenAt.Format(time.RFC3339)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp) // the client went away; nothing to do
	})
}

// formPart returns the content of a form file, nil when it is missing or unreadable (the use case
// then says which part is missing).
func formPart(r *http.Request, name string) []byte {
	f, _, err := r.FormFile(name)
	if err != nil {
		return nil
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	return data
}

// photoHandler serves GET /media/photos/{id} and, with thumb, GET /media/photos/{id}/thumb. A
// photo never changes (its id is its content's hash), so it is cached for good.
func photoHandler(m *media.Service, thumb bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img, err := m.Photo(game.PhotoID(r.PathValue("id")), thumb)
		switch {
		case errors.Is(err, media.ErrNoPhoto):
			http.Error(w, "no such photo", http.StatusNotFound)
			return
		case err != nil:
			http.Error(w, "photo unavailable", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", img.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(img.Data)
	})
}
```

If `errcheck` flags `f.Close()` on a file that was only read, it is excluded like `res.Body.Close`; otherwise write `defer func() { _ = f.Close() }()`. In `server.go`, after the asset route:

```go
	mux.Handle("POST /media/photos", photoUploadHandler(h.Media))
	mux.Handle("GET /media/photos/{id}", photoHandler(h.Media, false))
	mux.Handle("GET /media/photos/{id}/thumb", photoHandler(h.Media, true))
```

The cover served from a cover photo is checked end to end in Task 5 (`SetCoverPhoto`, then `GET /media/covers/{id}`).

- [ ] **Step 11: Run all Go tests and lint**

Run: `task go -- test ./internal/... ./cmd/... ` then `task lint`
Expected: PASS, 0 issues.

- [ ] **Step 12: Commit**

```bash
git add internal cmd
git commit -m "Upload, serve and prune copy photos; a cover photo becomes the cover"
```

---

### Task 5: Photo API — attach, caption, reorder, remove, cover photo

**Files:**
- Modify: `proto/gamevault/v1/game.proto` (+ `task generate` output in `internal/gen` and `web/src/gen`), `internal/application/catalog/service.go`, `internal/adapters/inbound/rpc/game_handler.go`, `internal/adapters/inbound/rpc/mapper.go`, `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`, `internal/adapters/inbound/rpc/photos_test.go`

**Interfaces:**
- Consumes: domain photo methods (Task 1), `PhotoStore.Has` (Task 3), upload endpoint (Task 4).
- Produces:
  - Proto: `message Photo { string id = 1; string caption = 2; google.protobuf.Timestamp taken_at = 3; google.protobuf.Timestamp added_at = 4; }`; `Copy.photos = 8`; `Game.cover_photo_id = 12`; `message NewPhoto { string id = 1; string caption = 2; google.protobuf.Timestamp taken_at = 3; }`; RPCs `AddCopyPhotos`, `UpdateCopyPhoto`, `RemoveCopyPhoto`, `ReorderCopyPhotos`, `SetCoverPhoto`, each returning `{ Game game = 1; }`.
  - `catalog.NewService(games, tx, now, covers CoverCache, photos PhotoFiles)`; `type catalog.PhotoFiles interface { Has(id game.PhotoID) bool }`; `var catalog.ErrPhotoNotUploaded`.
  - `(*catalog.Service).AddCopyPhotos(ctx, id, copyID game.ID, photos []game.Photo) (*game.Game, error)`, `UpdateCopyPhoto(ctx, id, copyID game.ID, photoID game.PhotoID, caption string)`, `RemoveCopyPhoto(ctx, id, copyID game.ID, photoID game.PhotoID)`, `ReorderCopyPhotos(ctx, id, copyID game.ID, ids []game.PhotoID)`, `SetCoverPhoto(ctx, id game.ID, photoID game.PhotoID)`.

- [ ] **Step 1: Proto** — in `game.proto`, before `message Copy`:

```proto
// Photo is a picture of a copy the user uploaded (POST /media/photos). The image is served at
// GET /media/photos/{id} and its thumbnail at GET /media/photos/{id}/thumb.
message Photo {
  // SHA-256 of the stored JPEG, lowercase hex.
  string id = 1;
  string caption = 2;
  // From the photo's metadata: the camera's clock reading, as UTC. Absent when unknown.
  google.protobuf.Timestamp taken_at = 3;
  google.protobuf.Timestamp added_at = 4;
}
```

In `Copy`: `// The user's photos of the copy, in their order.` `repeated Photo photos = 8;`. In `Game`: `// One of the copies' photos used as the cover (it wins over cover_url); empty when none.` `string cover_photo_id = 12;`. New messages before `service GameService`:

```proto
// NewPhoto is an uploaded photo to attach to a copy.
message NewPhoto {
  // The id POST /media/photos answered.
  string id = 1;
  string caption = 2;
  // The takenAt POST /media/photos answered, if any.
  google.protobuf.Timestamp taken_at = 3;
}

message AddCopyPhotosRequest {
  string game_id = 1;
  string copy_id = 2;
  repeated NewPhoto photos = 3;
}
message AddCopyPhotosResponse {
  Game game = 1;
}

message UpdateCopyPhotoRequest {
  string game_id = 1;
  string copy_id = 2;
  string photo_id = 3;
  string caption = 4;
}
message UpdateCopyPhotoResponse {
  Game game = 1;
}

message RemoveCopyPhotoRequest {
  string game_id = 1;
  string copy_id = 2;
  string photo_id = 3;
}
message RemoveCopyPhotoResponse {
  Game game = 1;
}

// ReorderCopyPhotosRequest gives the copy's photos in their new order; it must list each once.
message ReorderCopyPhotosRequest {
  string game_id = 1;
  string copy_id = 2;
  repeated string photo_ids = 3;
}
message ReorderCopyPhotosResponse {
  Game game = 1;
}

// SetCoverPhotoRequest makes one of the copies' photos the game's cover; an empty photo_id stops
// using a photo as the cover.
message SetCoverPhotoRequest {
  string game_id = 1;
  string photo_id = 2;
}
message SetCoverPhotoResponse {
  Game game = 1;
}
```

and in the service:

```proto
  // Attaches uploaded photos to a copy (at most 50 per copy; a photo already on it is skipped).
  rpc AddCopyPhotos(AddCopyPhotosRequest) returns (AddCopyPhotosResponse);
  rpc UpdateCopyPhoto(UpdateCopyPhotoRequest) returns (UpdateCopyPhotoResponse);
  rpc RemoveCopyPhoto(RemoveCopyPhotoRequest) returns (RemoveCopyPhotoResponse);
  rpc ReorderCopyPhotos(ReorderCopyPhotosRequest) returns (ReorderCopyPhotosResponse);
  rpc SetCoverPhoto(SetCoverPhotoRequest) returns (SetCoverPhotoResponse);
```

Run: `task generate`. Expected: generated Go and TS updated; the build fails until the handler implements the new methods (next steps).

- [ ] **Step 2: Write the failing end-to-end test** — append to `photos_test.go` (imports: `connectrpc.com/connect`, `pb "gamevault/internal/gen/gamevault/v1"`, `google.golang.org/protobuf/types/known/timestamppb`):

```go
func TestCopyPhotos_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game with a physical copy and two uploaded photos", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:  "Halo 3",
			Copies: []*pb.CopyDetails{{Kind: pb.CopyKind_COPY_KIND_PHYSICAL, Platform: "Xbox 360"}},
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
				Photos: []*pb.NewPhoto{{Id: ids[0], Caption: "box", TakenAt: timestamppb.New(taken)}, {Id: ids[1]}},
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
			_, err := c.games.UpdateCopyPhoto(ctx, connect.NewRequest(&pb.UpdateCopyPhotoRequest{GameId: g.Id, CopyId: copyID, PhotoId: ids[1], Caption: "disc"}))
			require.NoError(t, err)

			res, err := c.games.ReorderCopyPhotos(ctx, connect.NewRequest(&pb.ReorderCopyPhotosRequest{GameId: g.Id, CopyId: copyID, PhotoIds: []string{ids[1], ids[0]}}))
			require.NoError(t, err)

			t.Run("THEN the new order and caption are kept", func(t *testing.T) {
				photos := res.Msg.Game.Copies[0].Photos
				assert.Equal(t, ids[1], photos[0].Id)
				assert.Equal(t, "disc", photos[0].Caption)
			})
		})

		t.Run("WHEN a photo becomes the cover", func(t *testing.T) {
			res, err := c.games.SetCoverPhoto(ctx, connect.NewRequest(&pb.SetCoverPhotoRequest{GameId: g.Id, PhotoId: ids[0]}))
			require.NoError(t, err)

			t.Run("THEN the game says so and its cover is that photo", func(t *testing.T) {
				assert.Equal(t, ids[0], res.Msg.Game.CoverPhotoId)

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

			t.Run("AND editing the title keeps it, but a new custom cover URL replaces it", func(t *testing.T) {
				kept, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{Id: g.Id, Title: "Halo 3 (2007)"}))
				require.NoError(t, err)
				assert.Equal(t, ids[0], kept.Msg.Game.CoverPhotoId)

				replaced, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{Id: g.Id, Title: "Halo 3 (2007)", CoverUrl: "https://example.test/halo.jpg"}))
				require.NoError(t, err)
				assert.Empty(t, replaced.Msg.Game.CoverPhotoId)
			})
		})

		t.Run("WHEN a photo is removed", func(t *testing.T) {
			res, err := c.games.RemoveCopyPhoto(ctx, connect.NewRequest(&pb.RemoveCopyPhotoRequest{GameId: g.Id, CopyId: copyID, PhotoId: ids[1]}))
			require.NoError(t, err)

			t.Run("THEN the copy keeps the other one", func(t *testing.T) {
				require.Len(t, res.Msg.Game.Copies[0].Photos, 1)
				assert.Equal(t, ids[0], res.Msg.Game.Copies[0].Photos[0].Id)
			})
		})
	})
}
```

- [ ] **Step 3: Catalog use cases** — in `catalog/service.go`:

```go
// PhotoFiles is the port that tells whether an uploaded photo's files are stored.
type PhotoFiles interface {
	// Has reports whether the photo and its thumbnail are stored.
	Has(id game.PhotoID) bool
}

// ErrPhotoNotUploaded means a photo to attach is not stored: it was never uploaded, or it was
// pruned before being attached.
var ErrPhotoNotUploaded = errors.New("photo not uploaded: upload it again")
```

`Service` gains `photos PhotoFiles`; `NewService(games game.Repository, tx port.TxManager, now port.Clock, covers CoverCache, photos PhotoFiles)` — "photos may be nil: attached photos are then not checked". Update both callers (`main.go` passes `photos`, the `*photostore.Store`; `server_test.go` passes its photo store).

`UpdateGame` keeps the cover photo:

```go
	g, err := s.mutate(ctx, id, func(g *game.Game) error {
		// The request does not carry the cover photo: keep it, unless the user chose another
		// custom cover.
		if info.CoverURL == g.CoverURL() {
			info.CoverPhoto = g.CoverPhoto()
		}

		var err error

		coverChanged, err = g.UpdateInfo(info, s.now())

		return err
	})
```

Photo use cases (each invalidates the cached cover when the cover photo changed):

```go
// AddCopyPhotos attaches uploaded photos to a copy and returns the updated game.
func (s *Service) AddCopyPhotos(ctx context.Context, id, copyID game.ID, photos []game.Photo) (*game.Game, error) {
	for _, p := range photos {
		if s.photos != nil && !s.photos.Has(p.ID) {
			return nil, fmt.Errorf("%w (%s)", ErrPhotoNotUploaded, p.ID)
		}
	}

	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.AddPhotos(copyID, photos, s.now())
		return err
	})
}

// UpdateCopyPhoto changes a photo's caption and returns the updated game.
func (s *Service) UpdateCopyPhoto(ctx context.Context, id, copyID game.ID, photoID game.PhotoID, caption string) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.UpdatePhoto(copyID, photoID, caption, s.now())
		return err
	})
}

// RemoveCopyPhoto removes a photo from a copy and returns the updated game. The file stays until
// the daily cleanup, so a mistake can be undone by uploading it again.
func (s *Service) RemoveCopyPhoto(ctx context.Context, id, copyID game.ID, photoID game.PhotoID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.RemovePhoto(copyID, photoID, s.now())
		return err
	})
}

// ReorderCopyPhotos puts a copy's photos in a new order and returns the updated game.
func (s *Service) ReorderCopyPhotos(ctx context.Context, id, copyID game.ID, ids []game.PhotoID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.ReorderPhotos(copyID, ids, s.now())
		return err
	})
}

// SetCoverPhoto makes one of the copies' photos the cover; an empty id stops using a photo.
func (s *Service) SetCoverPhoto(ctx context.Context, id game.ID, photoID game.PhotoID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		info := g.Info()
		info.CoverPhoto = photoID
		_, err := g.UpdateInfo(info, s.now())

		return err
	})
}

// mutatePhotos is mutate, dropping the cached cover when the change touched the cover photo.
func (s *Service) mutatePhotos(ctx context.Context, id game.ID, fn func(*game.Game) error) (*game.Game, error) {
	var before game.PhotoID

	g, err := s.mutate(ctx, id, func(g *game.Game) error {
		before = g.CoverPhoto()
		return fn(g)
	})
	if err == nil && g.CoverPhoto() != before {
		s.invalidateCover(ctx, id)
	}

	return g, err
}
```

Also: `DeleteCopy` uses `mutatePhotos` instead of `mutate` (removing a copy may clear the cover photo). In `MoveCopy`, record `before := src.CoverPhoto()` after loading `src`, and after the transaction, if `src != nil && src.CoverPhoto() != before`, call `s.invalidateCover(ctx, from)`.

- [ ] **Step 4: Handlers and mapping** — in `mapper.go`:

```go
func photosToPB(photos []game.Photo) []*pb.Photo {
	out := make([]*pb.Photo, 0, len(photos))
	for _, p := range photos {
		ph := &pb.Photo{
			Id:      string(p.ID),
			Caption: p.Caption,
			AddedAt: ts(p.AddedAt),
		}
		if !p.TakenAt.IsZero() {
			ph.TakenAt = ts(p.TakenAt)
		}

		out = append(out, ph)
	}

	return out
}
```

`gameToPB`: `CoverPhotoId: string(g.CoverPhoto()),` and `Photos: photosToPB(c.Photos),` per copy. `toConnectError`: add `errors.Is(err, game.ErrPhotoNotFound)` to the NotFound case and a new case before `default`:

```go
	case errors.Is(err, catalog.ErrPhotoNotUploaded):
		return connect.NewError(connect.CodeFailedPrecondition, err)
```

(import `gamevault/internal/application/catalog` if the mapper does not have it). In `game_handler.go`:

```go
// AddCopyPhotos attaches uploaded photos to a copy.
func (h *GameHandler) AddCopyPhotos(ctx context.Context, req *connect.Request[pb.AddCopyPhotosRequest]) (*connect.Response[pb.AddCopyPhotosResponse], error) {
	photos := make([]game.Photo, 0, len(req.Msg.Photos))

	for _, p := range req.Msg.Photos {
		id, err := game.ParsePhotoID(p.Id)
		if err != nil {
			return nil, toConnectError(err)
		}

		ph := game.Photo{
			ID:      id,
			Caption: p.Caption,
		}
		if p.TakenAt != nil {
			ph.TakenAt = p.TakenAt.AsTime()
		}

		photos = append(photos, ph)
	}

	g, err := h.catalog.AddCopyPhotos(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId), photos)

	return gameResp(g, err, func(g *pb.Game) *pb.AddCopyPhotosResponse { return &pb.AddCopyPhotosResponse{Game: g} })
}

// UpdateCopyPhoto changes a photo's caption.
func (h *GameHandler) UpdateCopyPhoto(ctx context.Context, req *connect.Request[pb.UpdateCopyPhotoRequest]) (*connect.Response[pb.UpdateCopyPhotoResponse], error) {
	m := req.Msg
	g, err := h.catalog.UpdateCopyPhoto(ctx, game.ID(m.GameId), game.ID(m.CopyId), game.PhotoID(m.PhotoId), m.Caption)

	return gameResp(g, err, func(g *pb.Game) *pb.UpdateCopyPhotoResponse { return &pb.UpdateCopyPhotoResponse{Game: g} })
}

// RemoveCopyPhoto removes a photo from a copy.
func (h *GameHandler) RemoveCopyPhoto(ctx context.Context, req *connect.Request[pb.RemoveCopyPhotoRequest]) (*connect.Response[pb.RemoveCopyPhotoResponse], error) {
	m := req.Msg
	g, err := h.catalog.RemoveCopyPhoto(ctx, game.ID(m.GameId), game.ID(m.CopyId), game.PhotoID(m.PhotoId))

	return gameResp(g, err, func(g *pb.Game) *pb.RemoveCopyPhotoResponse { return &pb.RemoveCopyPhotoResponse{Game: g} })
}

// ReorderCopyPhotos puts a copy's photos in a new order.
func (h *GameHandler) ReorderCopyPhotos(ctx context.Context, req *connect.Request[pb.ReorderCopyPhotosRequest]) (*connect.Response[pb.ReorderCopyPhotosResponse], error) {
	m := req.Msg
	ids := make([]game.PhotoID, 0, len(m.PhotoIds))

	for _, id := range m.PhotoIds {
		ids = append(ids, game.PhotoID(id))
	}

	g, err := h.catalog.ReorderCopyPhotos(ctx, game.ID(m.GameId), game.ID(m.CopyId), ids)

	return gameResp(g, err, func(g *pb.Game) *pb.ReorderCopyPhotosResponse { return &pb.ReorderCopyPhotosResponse{Game: g} })
}

// SetCoverPhoto makes one of the copies' photos the cover, or stops using one.
func (h *GameHandler) SetCoverPhoto(ctx context.Context, req *connect.Request[pb.SetCoverPhotoRequest]) (*connect.Response[pb.SetCoverPhotoResponse], error) {
	g, err := h.catalog.SetCoverPhoto(ctx, game.ID(req.Msg.GameId), game.PhotoID(req.Msg.PhotoId))

	return gameResp(g, err, func(g *pb.Game) *pb.SetCoverPhotoResponse { return &pb.SetCoverPhotoResponse{Game: g} })
}
```

- [ ] **Step 5: Run the tests**

Run: `task go -- test ./internal/... ./cmd/...`
Expected: PASS, including `TestCopyPhotos_endToEnd`.

- [ ] **Step 6: Lint, type check and commit**

Run: `task lint && task test` (the TS type check covers the regenerated `web/src/gen`).

```bash
git add proto internal web/src/gen cmd
git commit -m "Photo API: attach, caption, reorder and remove copy photos; choose a cover photo"
```

---

### Task 6: Backups keep photos, each once

**Files:**
- Modify: `internal/application/system/service.go`, `internal/application/system/service_test.go`, `proto/gamevault/v1/system.proto` (+ generated), `internal/adapters/inbound/rpc/system_handler.go`, `internal/adapters/inbound/rpc/server_test.go`, `cmd/gamevault/main.go`

**Interfaces:**
- Consumes: `photostore.NewArchive` (Task 3), `(*game.Game).PhotoIDs` (Task 1).
- Produces:
  - `type system.PhotoArchive interface { Add(ids []game.PhotoID) error; Retain(keep map[game.PhotoID]bool) error; Size() (int64, error) }`.
  - `system.NewService(games, db, prefs, photos PhotoArchive, now, log, status, backupDir, keep)` — `photos` new after `prefs`; nil: backups without photos.
  - `system.Backup.Photos int`; `func (s *Service) PhotoStoreSize(ctx) (int64, error)`.
  - Proto: `Backup.photo_count = 4` (`int32`); `ListBackupsResponse.photo_store_bytes = 2` (`int64`).
  - Files: `backups/gamevault-<ts>.photos` (one id per line, sorted) next to `gamevault-<ts>.db`; shared store `backups/photos/`.

- [ ] **Step 1: Write the failing tests** — append to `service_test.go` (imports `strings`, `gamevault/internal/domain/game`, `fmt`):

```go
// photoGames is a game.Repository whose List returns games with the given photos on one copy.
type photoGames struct {
	game.Repository

	ids []game.PhotoID
}

func (r *photoGames) List(context.Context) ([]*game.Game, error) {
	photos := make([]game.Photo, 0, len(r.ids))
	for _, id := range r.ids {
		photos = append(photos, game.Photo{ID: id})
	}

	return []*game.Game{game.Rehydrate("g1", game.Info{Title: "Halo 3"}, []game.Copy{{
		ID: "c1",
		CopyDetails: game.CopyDetails{
			Kind: game.KindPhysical,
		},
		Photos: photos,
	}}, time.Now(), time.Now())}, nil
}

// fakeArchive records what the backups put in and keep in the shared photo store.
type fakeArchive struct {
	stored map[game.PhotoID]bool
	adds   int
}

func (a *fakeArchive) Add(ids []game.PhotoID) error {
	for _, id := range ids {
		if !a.stored[id] {
			a.stored[id] = true
			a.adds++
		}
	}

	return nil
}

func (a *fakeArchive) Retain(keep map[game.PhotoID]bool) error {
	for id := range a.stored {
		if !keep[id] {
			delete(a.stored, id)
		}
	}

	return nil
}

func (a *fakeArchive) Size() (int64, error) { return int64(len(a.stored)) * 100, nil }

func photoID(n int) game.PhotoID { return game.PhotoID(fmt.Sprintf("%064x", n)) }

func TestBackups_photos(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a catalog with two photos and room for two backups", func(t *testing.T) {
		dir := t.TempDir()
		games := &photoGames{ids: []game.PhotoID{photoID(1), photoID(2)}}
		archive := &fakeArchive{stored: map[game.PhotoID]bool{}}
		clock := time.Now()
		svc := system.NewService(games, fileBackup{}, nil, archive, func() time.Time { clock = clock.Add(time.Second); return clock },
			slog.New(slog.NewTextHandler(io.Discard, nil)), system.Status{}, dir, 2)

		t.Run("WHEN a backup is made", func(t *testing.T) {
			b, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN its photo list sits next to it and the photos are in the shared store", func(t *testing.T) {
				list, err := os.ReadFile(filepath.Join(dir, strings.TrimSuffix(b.Name, ".db")+".photos"))
				require.NoError(t, err)
				assert.Equal(t, string(photoID(1))+"\n"+string(photoID(2))+"\n", string(list))
				assert.Len(t, archive.stored, 2)
				assert.Equal(t, 2, b.Photos)
			})
		})

		t.Run("WHEN a second backup is made with the same photos", func(t *testing.T) {
			_, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN nothing new is stored", func(t *testing.T) {
				assert.Equal(t, 2, archive.adds)
			})
		})

		t.Run("WHEN one photo is gone from the catalog and two more backups rotate the first two out", func(t *testing.T) {
			games.ids = []game.PhotoID{photoID(2)}
			for range 2 {
				_, err := svc.CreateBackup(ctx)
				require.NoError(t, err)
			}

			t.Run("THEN the old lists went with their backups, and the photo no backup lists is removed", func(t *testing.T) {
				lists, err := filepath.Glob(filepath.Join(dir, "*.photos"))
				require.NoError(t, err)
				assert.Len(t, lists, 2)
				assert.Equal(t, map[game.PhotoID]bool{photoID(2): true}, archive.stored)
			})

			t.Run("AND the list of backups says how many photos each covers, and the store's size", func(t *testing.T) {
				list, err := svc.ListBackups(ctx)
				require.NoError(t, err)
				require.Len(t, list, 2)
				assert.Equal(t, 1, list[0].Photos)

				size, err := svc.PhotoStoreSize(ctx)
				require.NoError(t, err)
				assert.Equal(t, int64(100), size)
			})
		})
	})
}
```

Update the existing call in `TestBackups_rotation`: `system.NewService(nil, fileBackup{}, nil, nil, …)`.

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/application/system/ -v`
Expected: FAIL (build: `NewService` arity, `Backup.Photos`, `PhotoStoreSize`).

- [ ] **Step 3: Implement** — in `system/service.go`:

```go
// PhotoArchive is the port that keeps the backups' photos in one shared store, each photo once,
// however many backups list it.
type PhotoArchive interface {
	// Add puts the photos into the backups' store, skipping those already there.
	Add(ids []game.PhotoID) error

	// Retain deletes from the backups' store every photo not in keep.
	Retain(keep map[game.PhotoID]bool) error

	// Size returns the total size of the backups' store in bytes.
	Size() (int64, error)
}
```

`Backup` gains:

```go
	// Photos is how many photos the backup's list names; 0 for backups without a list (older ones
	// and pre-migration copies).
	Photos int
```

`Service` gains `photos PhotoArchive`; `NewService(games, db, prefs, photos PhotoArchive, now, log, status, backupDir, keep)` ("photos may be nil: backups then hold only the database").

`CreateBackup`, after `BackupTo` succeeds:

```go
	photos, err := s.backupPhotos(ctx, path)
	if err != nil {
		return Backup{}, fmt.Errorf("backing up photos: %w", err)
	}
```

and set `Photos: photos` in the returned `Backup`. Helpers:

```go
// photoList is the file next to a backup that names the photos it needs.
func photoList(dbPath string) string { return strings.TrimSuffix(dbPath, ".db") + ".photos" }

// backupPhotos writes the backup's photo list and puts the photos in the shared store.
func (s *Service) backupPhotos(ctx context.Context, dbPath string) (int, error) {
	if s.photos == nil {
		return 0, nil
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return 0, err
	}

	var ids []game.PhotoID

	for _, g := range games {
		for _, id := range g.PhotoIDs() {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}

	slices.Sort(ids)

	var list strings.Builder
	for _, id := range ids {
		list.WriteString(string(id) + "\n")
	}

	if err := os.WriteFile(photoList(dbPath), []byte(list.String()), 0o600); err != nil {
		return 0, err
	}

	return len(ids), s.photos.Add(ids)
}

// readPhotoList returns the photos a backup's list names; none when it has no list.
func readPhotoList(dbPath string) []game.PhotoID {
	data, err := os.ReadFile(photoList(dbPath))
	if err != nil {
		return nil
	}

	var ids []game.PhotoID

	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, game.PhotoID(line))
		}
	}

	return ids
}
```

(Use a `map` for the uniqueness check if `slices.Contains` over many photos is slow; at 50 photos × a few thousand copies a map is the safe choice: `seen := map[game.PhotoID]bool{}`.)

`ListBackups`: set `Photos: len(readPhotoList(filepath.Join(s.backupDir, e.Name())))`.

`prune`:

```go
func (s *Service) prune() error {
	backups, err := s.ListBackups(context.Background())
	if err != nil || len(backups) <= s.keep {
		return err
	}

	for _, b := range backups[s.keep:] {
		path := filepath.Join(s.backupDir, b.Name)
		if err := os.Remove(path); err != nil {
			return err
		}

		if err := os.Remove(photoList(path)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if s.photos == nil {
		return nil
	}

	keep := map[game.PhotoID]bool{}

	for _, b := range backups[:s.keep] {
		for _, id := range readPhotoList(filepath.Join(s.backupDir, b.Name)) {
			keep[id] = true
		}
	}

	return s.photos.Retain(keep)
}

// PhotoStoreSize returns the size of the backups' shared photo store in bytes.
func (s *Service) PhotoStoreSize(context.Context) (int64, error) {
	if s.photos == nil {
		return 0, nil
	}

	return s.photos.Size()
}
```

- [ ] **Step 4: Proto and handler** — `system.proto`: in `Backup`, `// How many photos the backup's list names (0 for older backups).` `int32 photo_count = 4;`; in `ListBackupsResponse`, `// Size of the photo store all backups share (each photo stored once).` `int64 photo_store_bytes = 2;`. Run `task generate`. In `system_handler.go`, `backupToPB` sets `PhotoCount: int32(b.Photos)`, and `ListBackups` sets `out.PhotoStoreBytes` from `h.system.PhotoStoreSize(ctx)` (return its error through `toConnectError`).

- [ ] **Step 5: Wiring** — `main.go`:

```go
	backupPhotos, err := photostore.Open(filepath.Join(cfg.BackupDir(), "photos"))
	if err != nil {
		return err
	}
```

and pass `photostore.NewArchive(photos, backupPhotos)` to `system.NewService` after `settingsRepo`. Import `path/filepath`. `server_test.go`: pass `nil` (the rpc tests do not cover backups' photos; the system tests do).

- [ ] **Step 6: Run the tests**

Run: `task go -- test ./internal/... ./cmd/...`
Expected: PASS.

- [ ] **Step 7: Lint, test, commit**

Run: `task lint && task test`

```bash
git add internal proto web/src/gen cmd
git commit -m "Backups: a photo list per backup and one shared store of photos"
```

---

### Task 7: Browser — resize photos, keep their metadata, upload

**Files:**
- Create: `web/src/lib/exif.ts`, `web/src/lib/photos.ts`, `web/tests/exif.test.mjs`
- Modify: `web/src/api/client.ts`, `Taskfile.yml`

**Interfaces:**
- Consumes: `POST /media/photos`, `GET /media/photos/{id}[/thumb]` (Task 4).
- Produces:
  - `exif.ts`: `readExifSegment(jpeg: Uint8Array): Uint8Array | null`; `resetOrientation(segment: Uint8Array): Uint8Array`; `insertSegment(jpeg: Uint8Array, segment: Uint8Array): Uint8Array`.
  - `photos.ts`: `interface PreparedPhoto { photo: Blob; thumb: Blob }`; `class UnreadablePhotoError extends Error`; `preparePhoto(file: File): Promise<PreparedPhoto>`; `interface UploadedPhoto { id: string; takenAt?: Date }`; `uploadPhoto(p: PreparedPhoto, onProgress: (fraction: number) => void): Promise<UploadedPhoto>`.
  - `client.ts`: `export const baseUrl`; `photoUrl(id: string, thumb?: boolean): string`.

- [ ] **Step 1: Write the failing Node test** — `web/tests/exif.test.mjs` (Node 24 strips the types of the imported `.ts`; `exif.ts` must stay import-free and use only erasable syntax: no `enum`, no `namespace`, no parameter properties):

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { insertSegment, readExifSegment, resetOrientation } from '../src/lib/exif.ts';

/** A TIFF block with IFD0 holding Make (0x010F, a short string inline) and Orientation (0x0112). */
function tiff(little, orientation) {
  const b = new Uint8Array(8 + 2 + 2 * 12 + 4);
  const v = new DataView(b.buffer);
  b.set(little ? [0x49, 0x49] : [0x4d, 0x4d]);
  v.setUint16(2, 42, little);
  v.setUint32(4, 8, little);
  v.setUint16(8, 2, little);
  v.setUint16(10, 0x010f, little); v.setUint16(12, 2, little); v.setUint32(14, 4, little); b.set([0x41, 0x43, 0x4d, 0], 18);
  v.setUint16(22, 0x0112, little); v.setUint16(24, 3, little); v.setUint32(26, 1, little); v.setUint16(30, orientation, little);
  return b;
}

/** A JPEG-shaped byte string: SOI, optional APP0, optional APP1 Exif, SOS with one byte, EOI. */
function jpeg({ app0 = false, exif = null } = {}) {
  const parts = [[0xff, 0xd8]];
  if (app0) parts.push([0xff, 0xe0, 0, 7, 0x4a, 0x46, 0x49, 0x46, 0]);
  if (exif) {
    const body = [0x45, 0x78, 0x69, 0x66, 0, 0, ...exif];
    parts.push([0xff, 0xe1, (body.length + 2) >> 8, (body.length + 2) & 0xff, ...body]);
  }
  parts.push([0xff, 0xda, 0, 3, 0x00, 0x11, 0xff, 0xd9]);
  return new Uint8Array(parts.flat());
}

const orientationOf = (segment, little) => new DataView(segment.buffer, segment.byteOffset).getUint16(10 + 22 + 8, little);

for (const little of [true, false]) {
  test(`a ${little ? 'little' : 'big'}-endian EXIF segment is found and its orientation reset to upright`, () => {
    const original = jpeg({ exif: tiff(little, 6) });
    const segment = readExifSegment(original);
    assert.ok(segment);
    assert.equal(segment[0], 0xff);
    assert.equal(segment[1], 0xe1);
    assert.equal(orientationOf(segment, little), 6);

    const upright = resetOrientation(segment);
    assert.equal(orientationOf(upright, little), 1);
    assert.equal(orientationOf(segment, little), 6, 'the original segment is not changed');
    assert.deepEqual(upright.subarray(10 + 18, 10 + 22), new Uint8Array([0x41, 0x43, 0x4d, 0]), 'other tags are kept');
  });
}

test('the segment goes after the APP0 header of the encoded image, or right after SOI without one', () => {
  const segment = readExifSegment(jpeg({ exif: tiff(true, 1) }));
  const withApp0 = insertSegment(jpeg({ app0: true }), segment);
  assert.deepEqual([...withApp0.subarray(0, 2)], [0xff, 0xd8]);
  assert.deepEqual([...withApp0.subarray(2, 4)], [0xff, 0xe0]);
  assert.deepEqual([...withApp0.subarray(11, 13)], [0xff, 0xe1]);
  assert.ok(readExifSegment(withApp0));

  const bare = insertSegment(jpeg(), segment);
  assert.deepEqual([...bare.subarray(2, 4)], [0xff, 0xe1]);
});

test('files without EXIF, truncated or not JPEG give null and never throw', () => {
  assert.equal(readExifSegment(jpeg()), null);
  assert.equal(readExifSegment(new Uint8Array([0x89, 0x50, 0x4e, 0x47])), null);
  const full = jpeg({ exif: tiff(true, 6) });
  for (let n = 0; n < full.length; n++) {
    assert.doesNotThrow(() => readExifSegment(full.subarray(0, n)));
  }
  const broken = readExifSegment(full).slice();
  new DataView(broken.buffer).setUint32(10 + 4, 0xfffffff0, true); // IFD0 offset far outside
  assert.doesNotThrow(() => resetOrientation(broken));
});
```

In `Taskfile.yml`, the `test` task command becomes:

```yaml
      - '{{.IN_TOOLCHAIN}} sh -c "go vet ./... && go test ./... && cd web && npm run typecheck && node --test tests/*.test.mjs && node ../.claude/scripts/check-i18n.mjs"'
```

- [ ] **Step 2: Run it to see it fail**

Run: `task test`
Expected: FAIL at `node --test` (cannot find `../src/lib/exif.ts`).

- [ ] **Step 3: Implement** — `web/src/lib/exif.ts`:

```ts
// EXIF helpers for photo uploads. The browser re-encodes photos on a canvas, which drops their
// metadata; these copy the original's EXIF segment into the reduced JPEG. Pure functions without
// imports, so Node runs their test directly (web/tests/exif.test.mjs).

const EXIF_ID = [0x45, 0x78, 0x69, 0x66, 0, 0]; // "Exif\0\0"

/** Returns the JPEG's EXIF segment (APP1, marker and length included), or null when it has none. */
export function readExifSegment(jpeg: Uint8Array): Uint8Array | null {
  if (jpeg.length < 4 || jpeg[0] !== 0xff || jpeg[1] !== 0xd8) return null;
  let i = 2;
  while (i + 4 <= jpeg.length) {
    if (jpeg[i] !== 0xff) return null;
    const marker = jpeg[i + 1]!;
    if (marker === 0xda || marker === 0xd9) return null; // image data: the metadata came before
    const len = (jpeg[i + 2]! << 8) | jpeg[i + 3]!;
    if (len < 2 || i + 2 + len > jpeg.length) return null;
    if (marker === 0xe1 && len >= 8 && EXIF_ID.every((b, k) => jpeg[i + 4 + k] === b)) {
      return jpeg.slice(i, i + 2 + len);
    }
    i += 2 + len;
  }
  return null;
}

/**
 * Returns a copy of an EXIF segment whose orientation (tag 0x0112 of IFD0) says upright: the
 * canvas already drew the pixels rotated, so keeping the old value would rotate them twice.
 */
export function resetOrientation(segment: Uint8Array): Uint8Array {
  const out = segment.slice();
  const tiff = 10; // FF E1, length (2 bytes), "Exif\0\0"
  if (out.length < tiff + 8) return out;
  const little = out[tiff] === 0x49;
  const view = new DataView(out.buffer, out.byteOffset, out.byteLength);
  const ifd0 = tiff + view.getUint32(tiff + 4, little);
  if (ifd0 + 2 > out.length) return out;
  const entries = view.getUint16(ifd0, little);
  for (let k = 0; k < entries; k++) {
    const e = ifd0 + 2 + 12 * k;
    if (e + 12 > out.length) break;
    if (view.getUint16(e, little) === 0x0112) {
      view.setUint16(e + 8, 1, little);
      break;
    }
  }
  return out;
}

/** Returns the JPEG with the segment inserted after its JFIF header (APP0), or after SOI. */
export function insertSegment(jpeg: Uint8Array, segment: Uint8Array): Uint8Array {
  let at = 2;
  if (jpeg.length >= 6 && jpeg[2] === 0xff && jpeg[3] === 0xe0) at = 4 + ((jpeg[4]! << 8) | jpeg[5]!);
  const out = new Uint8Array(jpeg.length + segment.length);
  out.set(jpeg.subarray(0, at), 0);
  out.set(segment, at);
  out.set(jpeg.subarray(at), at + segment.length);
  return out;
}
```

In `client.ts`: change `const baseUrl` to `export const baseUrl` and add:

```ts
/** A copy photo (or its thumbnail). Photos never change, so the URL needs no version. */
export function photoUrl(id: string, thumb = false): string {
  return `${baseUrl}/media/photos/${id}${thumb ? '/thumb' : ''}`;
}
```

`web/src/lib/photos.ts`:

```ts
import { UNAUTHENTICATED_EVENT, baseUrl } from '../api/client';
import { insertSegment, readExifSegment, resetOrientation } from './exif';

const MAX_SIDE = 2560;
const THUMB_SIDE = 400;

/** A photo ready to upload: the reduced JPEG, with the original's metadata, and its thumbnail. */
export interface PreparedPhoto {
  photo: Blob;
  thumb: Blob;
}

/** The browser cannot decode the picked file (HEIC outside Safari, a broken file…). */
export class UnreadablePhotoError extends Error {}

/** Reduces a picked image to at most 2560 px (JPEG 0.85) plus a 400 px thumbnail (JPEG 0.8). */
export async function preparePhoto(file: File): Promise<PreparedPhoto> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  } catch {
    throw new UnreadablePhotoError(file.name);
  }
  try {
    const photo = await encode(bitmap, MAX_SIDE, 0.85);
    const thumb = await encode(bitmap, THUMB_SIDE, 0.8);
    // The metadata (date, camera, location) is kept on purpose: it is the user's own record.
    const exif = readExifSegment(new Uint8Array(await file.arrayBuffer()));
    if (!exif) return { photo, thumb };
    const withExif = insertSegment(new Uint8Array(await photo.arrayBuffer()), resetOrientation(exif));
    return { photo: new Blob([withExif], { type: 'image/jpeg' }), thumb };
  } finally {
    bitmap.close();
  }
}

async function encode(bitmap: ImageBitmap, maxSide: number, quality: number): Promise<Blob> {
  const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(bitmap.width * scale));
  canvas.height = Math.max(1, Math.round(bitmap.height * scale));
  const ctx = canvas.getContext('2d')!;
  ctx.fillStyle = '#fff'; // transparent PNGs would turn black in a JPEG
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('the image could not be encoded'))), 'image/jpeg', quality));
}

/** The server's answer to an upload. */
export interface UploadedPhoto {
  id: string;
  takenAt?: Date;
}

/** Uploads a prepared photo; XMLHttpRequest because fetch cannot report upload progress. */
export function uploadPhoto(p: PreparedPhoto, onProgress: (fraction: number) => void): Promise<UploadedPhoto> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${baseUrl}/media/photos`);
    xhr.withCredentials = true;
    xhr.setRequestHeader('X-Gamevault-Upload', '1');
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
    xhr.onerror = () => reject(new Error(xhr.statusText || 'network error'));
    xhr.onload = () => {
      if (xhr.status === 401) window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT));
      if (xhr.status !== 200) {
        reject(new Error(xhr.responseText.trim() || `HTTP ${xhr.status}`));
        return;
      }
      const res = JSON.parse(xhr.responseText) as { id: string; takenAt?: string };
      resolve({ id: res.id, takenAt: res.takenAt ? new Date(res.takenAt) : undefined });
    };
    const form = new FormData();
    form.append('photo', p.photo, 'photo.jpg');
    form.append('thumb', p.thumb, 'thumb.jpg');
    xhr.send(form);
  });
}
```

Note: `web/src/lib/photos.ts` imports `./exif` without extension (Vite/tsc); only the Node test imports `exif.ts` with the extension. If `tsc` complains that `tests/` is outside `include`, it will not — `tests/` is not included.

- [ ] **Step 4: Run the checks**

Run: `task test`
Expected: PASS, including the 4 Node tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib web/tests web/src/api/client.ts Taskfile.yml
git commit -m "Browser: reduce photos, keep their metadata, upload with progress"
```

---

### Task 8: The image viewer

**Files:**
- Rewrite: `web/src/components/Lightbox.tsx`
- Modify: `web/src/components/Icon.tsx`, `web/src/features/library/GameSheet.tsx`, `web/src/styles.css`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`

**Interfaces:**
- Produces: `export default function Lightbox(props: { images: string[]; index: number; onIndex: (i: number) => void; onClose: () => void; label?: string; footer?: ReactNode })` — `images` are ready-to-use URLs (callers apply `mediaUrl` / `photoUrl`).

- [ ] **Step 1: Icons** — add to `paths` in `Icon.tsx`:

```ts
  prev: 'M15 5l-7 7 7 7',
  next: 'M9 5l7 7-7 7',
  zoomIn: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4M8 11h6M11 8v6',
  zoomOut: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4M8 11h6',
  expand: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5',
  shrink: 'M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5',
  star: 'M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1-4.4-4.3 6.1-.9z',
  arrowLeft: 'M19 12H5M11 6l-6 6 6 6',
  arrowRight: 'M5 12h14M13 6l6 6-6 6',
  photo: 'M4 7h3l2-3h6l2 3h3v12H4zM12 17a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
```

- [ ] **Step 2: Rewrite `Lightbox.tsx`**:

```tsx
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from './Icon';

const MAX_SCALE = 5;
const STEP = 1.5;
const DOUBLE_TAP_MS = 300;
const SWIPE_PX = 50;

interface View { scale: number; x: number; y: number }
const FIT: View = { scale: 1, x: 0, y: 0 };

/**
 * Full-screen image viewer for every gallery (screenshots, copy photos): previous / next, zoom
 * (buttons, double click or tap, Ctrl/⌘ + wheel, trackpad or touch pinch, drag to pan), full
 * screen, swipe on touch screens and the keyboard (← → Esc + − 0 F). `footer` shows per-image
 * content under the image.
 */
export default function Lightbox({ images, index, onIndex, onClose, label, footer }: {
  images: string[];
  index: number;
  onIndex: (i: number) => void;
  onClose: () => void;
  label?: string;
  footer?: ReactNode;
}) {
  const { t } = useTranslation();
  const root = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const img = useRef<HTMLImageElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const [view, setView] = useState<View>(FIT);
  const [fullscreen, setFullscreen] = useState(false);
  const [closing, setClosing] = useState(false);
  const many = images.length > 1;
  const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled;

  const go = useCallback((delta: number) => {
    if (many) onIndex((index + delta + images.length) % images.length);
  }, [many, index, images.length, onIndex]);

  const close = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduce) { onClose(); return; }
    setClosing(true);
    window.setTimeout(onClose, 160);
  }, [onClose]);

  // Keeps the image inside the stage: it can only be dragged as far as its zoomed edges.
  const clamp = useCallback((v: View): View => {
    const s = stage.current, i = img.current;
    if (!s || !i) return v;
    const scale = Math.min(MAX_SCALE, Math.max(1, v.scale));
    const maxX = Math.max(0, (i.offsetWidth * scale - s.clientWidth) / 2);
    const maxY = Math.max(0, (i.offsetHeight * scale - s.clientHeight) / 2);
    return { scale, x: Math.min(maxX, Math.max(-maxX, v.x)), y: Math.min(maxY, Math.max(-maxY, v.y)) };
  }, []);

  /** Zooms to scale keeping the point (px, py), relative to the stage's centre, still. */
  const zoomAt = useCallback((scale: number, px = 0, py = 0) => {
    setView((v) => {
      const next = Math.min(MAX_SCALE, Math.max(1, scale));
      const k = next / v.scale;
      return clamp({ scale: next, x: px - (px - v.x) * k, y: py - (py - v.y) * k });
    });
  }, [clamp]);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void root.current?.requestFullscreen();
  }, []);

  // A new image starts fitted.
  useEffect(() => setView(FIT), [index]);

  // Neighbouring images are loaded ahead, so moving through the gallery feels instant.
  useEffect(() => {
    if (!many) return;
    for (const d of [-1, 1]) new Image().src = images[(index + d + images.length) % images.length]!;
  }, [index, images, many]);

  // Focus goes into the viewer and back where it was on close.
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    closeButton.current?.focus();
    return () => before?.focus();
  }, []);

  useEffect(() => {
    const onChange = () => setFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', onChange);
    return () => document.removeEventListener('fullscreenchange', onChange);
  }, []);

  // Capture phase + stopPropagation: these keys act on the viewer, not on the dialog behind it.
  // Keys typed into a field (the caption) are left alone.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (target?.closest('input, textarea, [contenteditable="true"]')) return;
      const actions: Record<string, () => void> = {
        Escape: close,
        ArrowLeft: () => go(-1),
        ArrowRight: () => go(1),
        '+': () => zoomAt(view.scale * STEP),
        '=': () => zoomAt(view.scale * STEP),
        '-': () => zoomAt(view.scale / STEP),
        '0': () => setView(FIT),
        f: () => canFullscreen && toggleFullscreen(),
      };
      const action = actions[e.key];
      if (!action) return;
      e.stopPropagation();
      e.preventDefault();
      action();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [close, go, zoomAt, view.scale, canFullscreen, toggleFullscreen]);

  // Ctrl/⌘ + wheel and trackpad pinches (which arrive as Ctrl + wheel) zoom at the pointer; a plain
  // wheel pans a zoomed image. Not passive, to keep the page from zooming or scrolling.
  useEffect(() => {
    const s = stage.current;
    if (!s) return;
    const onWheel = (e: WheelEvent) => {
      const r = s.getBoundingClientRect();
      const px = e.clientX - r.left - r.width / 2, py = e.clientY - r.top - r.height / 2;
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        setView((v) => {
          const next = Math.min(MAX_SCALE, Math.max(1, v.scale * Math.exp(-e.deltaY * 0.01)));
          const k = next / v.scale;
          return clamp({ scale: next, x: px - (px - v.x) * k, y: py - (py - v.y) * k });
        });
      } else if (view.scale > 1) {
        e.preventDefault();
        setView((v) => clamp({ ...v, x: v.x - e.deltaX, y: v.y - e.deltaY }));
      }
    };
    s.addEventListener('wheel', onWheel, { passive: false });
    return () => s.removeEventListener('wheel', onWheel);
  }, [clamp, view.scale]);

  // Pointers: one drags (pans when zoomed, swipes otherwise), two pinch; a double tap or click
  // toggles the zoom. `moved` keeps the click that ends a drag from closing the viewer.
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef<{ startX: number; startY: number; view: View; dist: number; moved: boolean }>({ startX: 0, startY: 0, view: FIT, dist: 0, moved: false });
  const lastTap = useRef({ time: 0, x: 0, y: 0 });

  const relative = (x: number, y: number) => {
    const r = stage.current!.getBoundingClientRect();
    return { px: x - r.left - r.width / 2, py: y - r.top - r.height / 2 };
  };
  const spread = () => {
    const [a, b] = [...pointers.current.values()];
    return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0;
  };

  const onPointerDown = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest('button')) return;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    gesture.current = { startX: e.clientX, startY: e.clientY, view, dist: spread(), moved: false };
  };

  const onPointerMove = (e: React.PointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const g = gesture.current;
    const dx = e.clientX - g.startX, dy = e.clientY - g.startY;
    if (Math.hypot(dx, dy) > 4) g.moved = true;
    if (pointers.current.size === 2 && g.dist > 0) {
      const [a, b] = [...pointers.current.values()];
      const { px, py } = relative((a!.x + b!.x) / 2, (a!.y + b!.y) / 2);
      const next = Math.min(MAX_SCALE, Math.max(1, g.view.scale * (spread() / g.dist)));
      const k = next / g.view.scale;
      setView(clamp({ scale: next, x: px - (px - g.view.x) * k, y: py - (py - g.view.y) * k }));
    } else if (pointers.current.size === 1 && g.view.scale > 1) {
      setView(clamp({ ...g.view, x: g.view.x + dx, y: g.view.y + dy }));
    }
  };

  const onPointerUp = (e: React.PointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    const wasPinch = pointers.current.size > 1;
    pointers.current.delete(e.pointerId);
    const g = gesture.current;
    if (wasPinch) { g.moved = true; return; }
    const dx = e.clientX - g.startX, dy = e.clientY - g.startY;
    if (g.view.scale === 1 && Math.abs(dx) > SWIPE_PX && Math.abs(dx) > Math.abs(dy)) {
      go(dx < 0 ? 1 : -1);
      return;
    }
    if (g.moved) return;
    const now = performance.now();
    const tap = lastTap.current;
    if (now - tap.time < DOUBLE_TAP_MS && Math.hypot(e.clientX - tap.x, e.clientY - tap.y) < 30) {
      const { px, py } = relative(e.clientX, e.clientY);
      if (view.scale > 1) setView(FIT);
      else zoomAt(2.5, px, py);
      lastTap.current = { time: 0, x: 0, y: 0 };
      g.moved = true; // not a click on the backdrop
      return;
    }
    lastTap.current = { time: now, x: e.clientX, y: e.clientY };
  };

  // A click on the dark backdrop closes; on the image (to zoom or pan) it never does.
  const onStageClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget && !gesture.current.moved) close();
  };

  return (
    <div ref={root} className={`lightbox ${closing ? 'closing' : ''}`} role="dialog" aria-modal="true" aria-label={label ?? t('viewer.label')}>
      <div className="lightbox-bar">
        <span className="lightbox-count">{many ? `${index + 1} / ${images.length}` : ''}</span>
        <span className="spacer" />
        <button className="lightbox-button" onClick={() => zoomAt(view.scale / STEP)} disabled={view.scale <= 1} aria-label={t('viewer.zoomOut')} title={t('viewer.zoomOut')}><Icon name="zoomOut" size={20} /></button>
        <button className="lightbox-button" onClick={() => setView(FIT)} disabled={view.scale === 1} aria-label={t('viewer.resetZoom')} title={t('viewer.resetZoom')}><span className="lightbox-zoom">{Math.round(view.scale * 100)}%</span></button>
        <button className="lightbox-button" onClick={() => zoomAt(view.scale * STEP)} disabled={view.scale >= MAX_SCALE} aria-label={t('viewer.zoomIn')} title={t('viewer.zoomIn')}><Icon name="zoomIn" size={20} /></button>
        {canFullscreen && (
          <button className="lightbox-button" onClick={toggleFullscreen} aria-label={t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')} title={t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')}>
            <Icon name={fullscreen ? 'shrink' : 'expand'} size={20} />
          </button>
        )}
        <button ref={closeButton} className="lightbox-button" onClick={close} aria-label={t('viewer.close')} title={t('viewer.close')}><Icon name="close" size={22} /></button>
      </div>
      <div ref={stage} className={`lightbox-stage ${view.scale > 1 ? 'zoomed' : ''}`} onClick={onStageClick}
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp}>
        <img ref={img} src={images[index]} alt="" draggable={false}
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }} />
        {many && (
          <>
            <button className="lightbox-nav prev" onClick={() => go(-1)} aria-label={t('viewer.previous')} title={t('viewer.previous')}><Icon name="prev" size={28} /></button>
            <button className="lightbox-nav next" onClick={() => go(1)} aria-label={t('viewer.next')} title={t('viewer.next')}><Icon name="next" size={28} /></button>
          </>
        )}
      </div>
      {footer && <div className="lightbox-footer">{footer}</div>}
    </div>
  );
}
```

Import `React` types: use `import type { PointerEvent as ReactPointerEvent, MouseEvent as ReactMouseEvent } from 'react'` instead of `React.PointerEvent` if the project does not have the `React` namespace in scope (check another component; with `jsx: react-jsx` the global `React` namespace types are available through `@types/react`, but an explicit type import is clearer).

- [ ] **Step 3: Styles** — replace the three `.lightbox` rules in `styles.css` with:

```css
/* Image viewer (components/Lightbox.tsx): always dark, whatever the theme, like a photo app. */
.lightbox {
  position: fixed; inset: 0; z-index: 40; display: grid; grid-template-rows: auto 1fr auto;
  background: rgb(8 10 16 / .96); color: #fff;
  transition: opacity 200ms var(--ease-out), transform 200ms var(--ease-out);
  @starting-style { opacity: 0; transform: scale(0.97); }
}
.lightbox.closing { opacity: 0; transform: scale(0.97); transition-duration: 160ms; }
.lightbox-bar {
  display: flex; align-items: center; gap: 4px;
  padding: max(8px, env(safe-area-inset-top)) max(12px, env(safe-area-inset-right)) 8px max(12px, env(safe-area-inset-left));
}
.lightbox-count { font-size: 14px; font-variant-numeric: tabular-nums; opacity: .8; }
.lightbox-button {
  display: inline-grid; place-items: center; min-width: 44px; height: 44px; padding: 0 8px; border: 0; border-radius: 10px;
  background: transparent; color: inherit;
}
.lightbox-button:disabled { opacity: .35; }
.lightbox-zoom { font-size: 13px; font-variant-numeric: tabular-nums; }
@media (hover: hover) and (pointer: fine) { .lightbox-button:not(:disabled):hover, .lightbox-nav:hover { background: rgb(255 255 255 / .12); } }
.lightbox-stage { position: relative; overflow: hidden; display: grid; place-items: center; touch-action: none; user-select: none; }
.lightbox-stage img {
  max-width: calc(100% - 32px); max-height: 100%; border-radius: 4px; transform-origin: center;
  cursor: zoom-in; will-change: transform;
}
.lightbox-stage.zoomed img { cursor: grab; }
.lightbox-nav {
  position: absolute; top: 50%; translate: 0 -50%; display: grid; place-items: center; width: 48px; height: 72px;
  border: 0; border-radius: 12px; background: rgb(0 0 0 / .35); color: #fff;
}
.lightbox-nav.prev { left: max(8px, env(safe-area-inset-left)); }
.lightbox-nav.next { right: max(8px, env(safe-area-inset-right)); }
.lightbox-footer { padding: 10px max(16px, env(safe-area-inset-left)) max(12px, env(safe-area-inset-bottom)); }
@media (max-width: 859px) { .lightbox-nav { width: 40px; height: 56px; background: rgb(0 0 0 / .25); } }
@media (prefers-reduced-motion: reduce) { .lightbox, .lightbox.closing { transition: none; } }
```

Check that a global `prefers-reduced-motion` rule does not already cover `.lightbox` (search `prefers-reduced-motion` in `styles.css`); if it does, drop the last line.

- [ ] **Step 4: Screenshots use it** — in `GameSheet.tsx`:

```tsx
        <Lightbox images={details.screenshots.map((s) => mediaUrl(s.fullUrl))} index={shot} onIndex={setShot}
          onClose={() => setShot(null)} label={t('details.screenshots')} />
```

- [ ] **Step 5: Translations** — `en.json`, a new top-level `viewer` object: `"label": "Image viewer"`, `"close": "Close"`, `"previous": "Previous image"`, `"next": "Next image"`, `"zoomIn": "Zoom in"`, `"zoomOut": "Zoom out"`, `"resetZoom": "Fit to screen"`, `"fullscreen": "Full screen"`, `"exitFullscreen": "Exit full screen"`. `es.json`: `"label": "Visor de imágenes"`, `"close": "Cerrar"`, `"previous": "Imagen anterior"`, `"next": "Imagen siguiente"`, `"zoomIn": "Ampliar"`, `"zoomOut": "Reducir"`, `"resetZoom": "Ajustar a la pantalla"`, `"fullscreen": "Pantalla completa"`, `"exitFullscreen": "Salir de pantalla completa"`.

- [ ] **Step 6: Check**

Run: `task test` (types, i18n). Then `task test-server`, open `http://127.0.0.1:8093/?v=<n>` in the browser pane, open a game with screenshots, click one and check: buttons visible; ← → and the side buttons change image; Esc and the close button close; a click on the backdrop closes, a click on the image does not; double click zooms at the pointer and again fits; the zoom buttons and the percentage; drag to pan stays within the image; full screen toggles. Mobile preset: swipe changes image, double tap zooms, the bar respects the safe area. Reset the viewport to desktop; `task test-server:stop`.

- [ ] **Step 7: Commit**

```bash
git add web/src
git commit -m "A better image viewer: buttons, zoom, pan, full screen and swipe"
```

---

### Task 9: Copy photos in the game sheet and the backups table

**Files:**
- Create: `web/src/features/library/CopyPhotos.tsx`
- Modify: `web/src/features/library/GameDetail.tsx`, `web/src/features/system/SystemPage.tsx`, `web/src/styles.css`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`

**Interfaces:**
- Consumes: `gameClient.addCopyPhotos / updateCopyPhoto / removeCopyPhoto / reorderCopyPhotos / setCoverPhoto` (Task 5), `preparePhoto`, `uploadPhoto`, `UnreadablePhotoError`, `photoUrl` (Task 7), `Lightbox` (Task 8), `useAppData().putGame`, `Backup.photoCount`, `ListBackupsResponse.photoStoreBytes` (Task 6).
- Produces: `export default function CopyPhotos({ game, copy }: { game: Game; copy: Copy })`.

- [ ] **Step 1: The component** — `web/src/features/library/CopyPhotos.tsx`:

```tsx
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { errorMessage, gameClient, photoUrl } from '../../api/client';
import { Icon } from '../../components/Icon';
import Lightbox from '../../components/Lightbox';
import { useFormatters } from '../../components/ui';
import { preparePhoto, UnreadablePhotoError, uploadPhoto } from '../../lib/photos';
import { toDate, type Copy, type Game } from '../../lib/model';
import { useAppData } from '../../state/AppData';

const STRIP = 6;
const MAX_PHOTOS = 50;

interface Upload { key: string; name: string; progress: number; error?: string }

/** A copy's photos: a strip of thumbnails, "Add photos" with per-file progress, and the viewer. */
export default function CopyPhotos({ game, copy }: { game: Game; copy: Copy }) {
  const { t } = useTranslation();
  const { putGame } = useAppData();
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [open, setOpen] = useState<number | null>(null);
  const photos = copy.photos;

  const update = (key: string, patch: Partial<Upload>) =>
    setUploads((list) => list.map((u) => (u.key === key ? { ...u, ...patch } : u)));

  // One file at a time: each is reduced in memory first, and attaching one by one shows each photo
  // as soon as it is ready.
  const add = async (files: File[]) => {
    const room = MAX_PHOTOS - photos.length;
    const taken = files.slice(0, Math.max(0, room));
    const skipped = files.length - taken.length;
    const batch = taken.map((f, i) => ({ key: `${Date.now()}-${i}`, name: f.name, progress: 0 }));
    setUploads((list) => [
      ...list,
      ...batch,
      ...(skipped > 0 ? [{ key: `${Date.now()}-limit`, name: '', progress: 0, error: t('photos.tooMany', { max: MAX_PHOTOS, count: skipped }) }] : []),
    ]);
    for (const [i, file] of taken.entries()) {
      const { key } = batch[i]!;
      try {
        const prepared = await preparePhoto(file);
        const up = await uploadPhoto(prepared, (p) => update(key, { progress: p }));
        const res = await gameClient.addCopyPhotos({
          gameId: game.id,
          copyId: copy.id,
          photos: [{ id: up.id, takenAt: up.takenAt ? timestampFromDate(up.takenAt) : undefined }],
        });
        putGame(res.game!);
        setUploads((list) => list.filter((u) => u.key !== key));
      } catch (e) {
        update(key, { error: e instanceof UnreadablePhotoError ? t('photos.unreadable') : errorMessage(e) });
      }
    }
  };

  return (
    <div className="copy-photos">
      {photos.length > 0 && (
        <ul className="photo-strip">
          {photos.slice(0, STRIP).map((p, i) => (
            <li key={p.id}>
              <button onClick={() => setOpen(i)} aria-label={p.caption || t('photos.open', { n: i + 1 })}>
                <img src={photoUrl(p.id, true)} alt="" loading="lazy" />
                {game.coverPhotoId === p.id && <span className="photo-cover" title={t('photos.isCover')}><Icon name="star" size={12} /></span>}
              </button>
            </li>
          ))}
          {photos.length > STRIP && (
            <li><button className="photo-more" onClick={() => setOpen(STRIP)}>{t('photos.more', { count: photos.length - STRIP })}</button></li>
          )}
        </ul>
      )}
      {uploads.length > 0 && (
        <ul className="photo-uploads">
          {uploads.map((u) => (
            <li key={u.key} className={u.error ? 'failed' : ''}>
              {u.error ? (
                <>
                  <span className="small">{u.name ? t('photos.failed', { name: u.name, error: u.error }) : u.error}</span>
                  <button className="link" onClick={() => setUploads((l) => l.filter((x) => x.key !== u.key))}>{t('photos.dismiss')}</button>
                </>
              ) : (
                <>
                  <span className="small muted">{u.name}</span>
                  <progress max={1} value={u.progress} aria-label={t('photos.uploading', { name: u.name })} />
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      <label className="button small-button file">
        <Icon name="photo" size={16} />{t('photos.add')}
        <input type="file" accept="image/*" multiple hidden
          onChange={(e) => { const files = [...(e.target.files ?? [])]; e.target.value = ''; if (files.length) void add(files); }} />
      </label>
      {open !== null && photos[open] && (
        <Lightbox images={photos.map((p) => photoUrl(p.id))} index={open} onIndex={setOpen} onClose={() => setOpen(null)}
          label={t('photos.title')} footer={<PhotoFooter game={game} copy={copy} index={open} onIndex={setOpen} onEmpty={() => setOpen(null)} />} />
      )}
    </div>
  );
}

/** Under the photo in the viewer: caption (edited in place), date taken, order, cover, delete. */
function PhotoFooter({ game, copy, index, onIndex, onEmpty }: {
  game: Game;
  copy: Copy;
  index: number;
  onIndex: (i: number) => void;
  onEmpty: () => void;
}) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const { putGame } = useAppData();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const photos = copy.photos;
  const photo = photos[index]!;
  const [caption, setCaption] = useState(photo.caption);
  const [editingFor, setEditingFor] = useState(photo.id);
  if (editingFor !== photo.id) { setEditingFor(photo.id); setCaption(photo.caption); }
  const isCover = game.coverPhotoId === photo.id;
  const taken = toDate(photo.takenAt);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try { await fn(); } catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const ids = photos.map((p) => p.id);
  const move = (delta: number) => run(async () => {
    const order = [...ids];
    [order[index], order[index + delta]] = [order[index + delta]!, order[index]!];
    putGame((await gameClient.reorderCopyPhotos({ gameId: game.id, copyId: copy.id, photoIds: order })).game!);
    onIndex(index + delta);
  });
  const saveCaption = () => {
    if (caption.trim() === photo.caption) return;
    void run(async () => putGame((await gameClient.updateCopyPhoto({ gameId: game.id, copyId: copy.id, photoId: photo.id, caption })).game!));
  };

  return (
    <div className="photo-footer">
      <input className="photo-caption" value={caption} maxLength={200} placeholder={t('photos.captionPlaceholder')} aria-label={t('photos.caption')}
        onChange={(e) => setCaption(e.target.value)} onBlur={saveCaption}
        onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); if (e.key === 'Escape') { setCaption(photo.caption); e.currentTarget.blur(); } }} />
      <div className="photo-actions">
        {taken && <span className="small muted">{t('photos.takenOn', { date: taken.toLocaleDateString(undefined, { timeZone: 'UTC', dateStyle: 'medium' }) })}</span>}
        <span className="spacer" />
        <button className="lightbox-button" disabled={busy || index === 0} onClick={() => move(-1)} aria-label={t('photos.moveLeft')} title={t('photos.moveLeft')}><Icon name="arrowLeft" size={18} /></button>
        <button className="lightbox-button" disabled={busy || index === photos.length - 1} onClick={() => move(1)} aria-label={t('photos.moveRight')} title={t('photos.moveRight')}><Icon name="arrowRight" size={18} /></button>
        <button className={`lightbox-button ${isCover ? 'active' : ''}`} disabled={busy} aria-pressed={isCover}
          onClick={() => run(async () => putGame((await gameClient.setCoverPhoto({ gameId: game.id, photoId: isCover ? '' : photo.id })).game!))}>
          <Icon name="star" size={18} />{t(isCover ? 'photos.stopCover' : 'photos.useAsCover')}
        </button>
        <button className="lightbox-button" disabled={busy} aria-label={t('common.delete')} title={t('common.delete')}
          onClick={() => {
            if (!confirm(t('photos.confirmDelete'))) return;
            void run(async () => {
              putGame((await gameClient.removeCopyPhoto({ gameId: game.id, copyId: copy.id, photoId: photo.id })).game!);
              if (photos.length === 1) onEmpty();
              else if (index === photos.length - 1) onIndex(index - 1);
            });
          }}><Icon name="trash" size={18} /></button>
      </div>
      {error && <p className="small photo-error">{error}</p>}
    </div>
  );
}
```

Notes for the implementer:
- `fmt` is unused if the date is formatted inline; drop `useFormatters` or use it — keep `noUnusedLocals` happy. The date is shown in UTC because `takenAt` is the camera's clock reading stored as UTC (spec).
- The props `game` and `copy` change after each `putGame` (AppData re-renders the sheet with the new game), so `copy.photos` is always current; make sure `CopiesTab` passes the copy from `game.copies` (it does).
- `.lightbox-button.active` uses the amber accent (active state). `confirm` is what the rest of the app uses for deletions.

- [ ] **Step 2: Use it** — in `GameDetail.tsx`'s `CopiesTab`, inside each `<li className="copy-card …">`, after the notes line and before `syncedFrom`:

```tsx
              <CopyPhotos game={game} copy={c} />
```

with `import CopyPhotos from './CopyPhotos';`.

- [ ] **Step 3: Backups table** — in `SystemPage.tsx`, store `bk.photoStoreBytes` in state (`const [photoStore, setPhotoStore] = useState(0n)` → `setPhotoStore(bk.photoStoreBytes)`), add a column header `<th>{t('system.backupPhotos')}</th>` and cell `<td>{b.photoCount}</td>`, and under the table:

```tsx
        {photoStore > 0n && <p className="muted small">{t('system.photoStore', { size: fmt.bytes(photoStore) })}</p>}
```

- [ ] **Step 4: Styles** — append to `styles.css`:

```css
/* Copy photos (features/library/CopyPhotos.tsx) */
.copy-photos { display: grid; gap: 8px; margin-top: 8px; justify-items: start; }
.photo-strip { display: flex; flex-wrap: wrap; gap: 6px; list-style: none; margin: 0; padding: 0; }
.photo-strip button {
  position: relative; display: block; width: 64px; height: 64px; padding: 0; border: 0; border-radius: 8px; overflow: hidden;
  background: var(--surface-2, var(--mist)); transition: transform 160ms var(--ease-out);
}
.photo-strip button:active { transform: scale(0.97); }
.photo-strip img { width: 100%; height: 100%; object-fit: cover; }
.photo-more { font-weight: 700; font-size: 14px; color: var(--text); }
.photo-cover { position: absolute; top: 4px; right: 4px; display: grid; place-items: center; width: 18px; height: 18px; border-radius: 50%; background: #FFB020; color: #181C2C; }
.photo-uploads { display: grid; gap: 4px; list-style: none; margin: 0; padding: 0; width: 100%; }
.photo-uploads li { display: flex; align-items: center; gap: 8px; }
.photo-uploads progress { flex: 1; max-width: 220px; height: 6px; accent-color: #FFB020; }
.photo-uploads .failed { color: var(--danger, #d33); }
.photo-footer { display: grid; gap: 8px; max-width: 960px; margin: 0 auto; }
.photo-caption {
  width: 100%; font-size: 16px; padding: 8px 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / .2);
  background: rgb(255 255 255 / .06); color: #fff;
}
.photo-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; }
.photo-actions .lightbox-button { gap: 6px; font-size: 14px; }
.lightbox-button.active { color: #FFB020; }
.photo-error { color: #ff8a80; }
```

Use the real token names from `:root` in `styles.css` (search for the surface and danger tokens) instead of the fallbacks above.

- [ ] **Step 5: Translations** — `en.json`, a new top-level `photos` object:

```json
"photos": {
  "title": "Photos of the copy",
  "add": "Add photos",
  "open": "Photo {{n}}",
  "more": "+{{count}}",
  "uploading": "Uploading {{name}}",
  "failed": "{{name}}: {{error}}",
  "dismiss": "Dismiss",
  "unreadable": "This browser cannot read that image. Pick a JPEG or PNG (on an iPhone, set Camera → Formats → Most Compatible).",
  "tooMany_one": "A copy can have at most {{max}} photos: {{count}} photo was not added.",
  "tooMany_other": "A copy can have at most {{max}} photos: {{count}} photos were not added.",
  "caption": "Caption",
  "captionPlaceholder": "Add a caption",
  "takenOn": "Taken on {{date}}",
  "moveLeft": "Move left",
  "moveRight": "Move right",
  "useAsCover": "Use as cover",
  "stopCover": "Stop using as cover",
  "isCover": "The game's cover",
  "confirmDelete": "Delete this photo from the copy?"
}
```

and in `system`: `"backupPhotos": "Photos"`, `"photoStore": "Photos in backups: {{size}} in all; each photo is stored once, however many backups have it."`. `es.json`:

```json
"photos": {
  "title": "Fotos de la copia",
  "add": "Añadir fotos",
  "open": "Foto {{n}}",
  "more": "+{{count}}",
  "uploading": "Subiendo {{name}}",
  "failed": "{{name}}: {{error}}",
  "dismiss": "Descartar",
  "unreadable": "Este navegador no puede leer esa imagen. Elige un JPEG o PNG (en un iPhone, Ajustes → Cámara → Formatos → Más compatible).",
  "tooMany_one": "Una copia puede tener como mucho {{max}} fotos: {{count}} foto no se ha añadido.",
  "tooMany_other": "Una copia puede tener como mucho {{max}} fotos: {{count}} fotos no se han añadido.",
  "caption": "Pie de foto",
  "captionPlaceholder": "Añade un pie de foto",
  "takenOn": "Tomada el {{date}}",
  "moveLeft": "Mover a la izquierda",
  "moveRight": "Mover a la derecha",
  "useAsCover": "Usar como portada",
  "stopCover": "Dejar de usar como portada",
  "isCover": "La portada del juego",
  "confirmDelete": "¿Borrar esta foto de la copia?"
}
```

and in `system`: `"backupPhotos": "Fotos"`, `"photoStore": "Fotos en las copias de seguridad: {{size}} en total; cada foto se guarda una sola vez, la tengan las copias que la tengan."`. Check how `check-i18n.mjs` treats plural suffixes (`_one` / `_other`); other keys in the files (e.g. `common.daysLeft`) show the convention.

- [ ] **Step 6: Check**

Run: `task test`. Then `task test-server` and, in the browser pane (`?v=<n>`), on a game's Copies tab: add three photos (use JPEGs with EXIF from the toolchain, e.g. generate them with a small Go program in the scratchpad, plus one PNG); check progress, thumbnails, the "+N" button with more than six, the viewer footer (caption saved on Enter and blur, Esc in the caption does not close the viewer, move left/right, use as cover → the poster in the library changes, stop using it, delete with confirm), the date taken, a HEIC or text file renamed `.jpg` showing a clear error while the others continue. System → Backups: "Back up now", the photos column and the store size line. Mobile preset: the strip, the file picker, the footer wrapping. Reset the viewport; `task test-server:stop`.

- [ ] **Step 7: Commit**

```bash
git add web/src
git commit -m "Copy photos in the game sheet: add, view, caption, reorder, cover, delete"
```

---

### Task 10: Docs, review and PR

**Files:**
- Modify: `docs/technical.md`, `.claude/docs/ui.md`, `.claude/memory/data-model.md`, `README.md`

- [ ] **Step 1: `docs/technical.md`** — add a "Photos of copies" section: what can be attached (any copy, 50 per copy, captions ≤ 200, order, cover photo winning over the custom cover URL and kept when the game is edited unless the cover URL changes); where files live (`config/photos/<2 hex>/<sha256>.jpg` and `-thumb.jpg`); that the browser reduces photos to 2560 px and keeps their EXIF metadata, location included, with the orientation reset (say plainly that sharing a photo file from `config/photos` shares its location); the upload endpoint, its limits and the `X-Gamevault-Upload` header and why; serving and caching; the daily cleanup with its one-day grace. In the backups section: the `.photos` list per backup, the shared `backups/photos/` store with hard links (copies on file systems without them), rotation trimming it, the System page figures, and the restore steps:

```bash
# Stop Game Vault first.
cp config/backups/gamevault-YYYYMMDD-HHMMSS.db config/gamevault.db
rm -f config/gamevault.db-wal config/gamevault.db-shm
cp -R config/backups/photos/. config/photos/
# Start Game Vault again; the daily cleanup removes photos the restored catalog does not use.
```

- [ ] **Step 2: `.claude/docs/ui.md`** — under Structure, mention `components/Lightbox.tsx` (the shared viewer: props, keys, gestures, footer slot) and `features/library/CopyPhotos.tsx`; under Motion, note the viewer's open/close and that image changes are not animated.

- [ ] **Step 3: Memory** — `.claude/memory/data-model.md`: copies have `Photos` (content-addressed ids, never renamed), `Info.CoverPhoto`, the `photos` / `coverPhoto` document fields (game document still v2). Keep the index line in `.claude/MEMORY.md` accurate if the summary changes.

- [ ] **Step 4: README** — in "Your data", one line: `config/photos` holds the photos you take of your copies (backed up with the database).

- [ ] **Step 5: Full verification** — run the `verify` skill: `task lint`, `task test`, `task test-server` with the browser checks of Tasks 8 and 9 on desktop and mobile, `task test-server:stop`. Do not scan rotating-credential sources on the copy.

- [ ] **Step 6: Commit the docs**

```bash
git add docs .claude README.md
git commit -m "Document copy photos, the photo store, backups of photos and the viewer"
```

- [ ] **Step 7: Final review** — a fresh reviewer on the most capable model reviews the whole branch against the spec and the Review Focus list; fix what it finds in separate commits.

- [ ] **Step 8: PR** — push `feature/copy-photos` and open a PR to `main` titled "Photos of copies, deduplicated photo backups and a better image viewer", with a summary, the verification done, what was only checked against fakes (none: everything is local), and the restore steps. Then `mcp__ccd_pr__get_status` (bind the PR if needed) and report CI. No tags.
