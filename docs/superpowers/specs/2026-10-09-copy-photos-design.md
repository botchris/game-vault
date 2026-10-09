# Photos of copies, deduplicated backups and a better image viewer

Status: approved design, waiting for the implementation plan. Branch `feature/copy-photos`.
Part 2 of the physical-copy work (part 1: grade, contents and price; part 3: second-hand valuation).

## Why

Collectors keep photos of what they own: the box, the disc, the figure of a collector's edition, a
card with a key. Game Vault downloads covers and screenshots, but has no photos of the user's own
copies, and its image viewer is minimal (no visible buttons, no zoom, no full screen). Photos are
also the first data the user creates that cannot be downloaded again, so backups must keep them,
without storing the same image once per backup.

## Scope

In:

- Photos on any copy (key, library or physical), with a caption, an order, the date taken and the
  date added; one of them can be the game's cover.
- Photo files stored content-addressed in `config/photos/`; uploads through an HTTP endpoint;
  attaching, captioning, reordering and removing through the API.
- Browser-side resizing that keeps the original JPEG metadata.
- Backups: the database snapshot plus a per-backup photo list and one shared, deduplicated photo
  store; rotation cleans the store.
- A shared image viewer (`components/Lightbox.tsx`) rebuilt for usability, used by the game
  sheet's screenshots and by copy photos.

Out: photo search or filters, image editing (crop, rotate), photos in the CSV export or import,
part 3 (valuation).

## Domain (`internal/domain/game`)

```go
// Photo is a picture of a copy the user uploaded.
type Photo struct {
	ID      PhotoID   // SHA-256 of the stored JPEG, lowercase hex: the file's name
	Caption string    // optional, at most 200 characters
	TakenAt time.Time // from the photo's metadata; zero when unknown
	AddedAt time.Time
}
```

- `PhotoID` is a validated type (64 lowercase hex characters).
- `Copy` gains `Photos []Photo` (ordered). Any kind of copy can have photos.
- New `Game` methods, all returning the updated copy and `ErrCopyNotFound` / `ErrPhotoNotFound`
  where relevant:
  - `AddPhotos(copyID, photos []Photo, now)`: appends; a photo already on that copy is not added
    twice; at most 50 photos per copy (validation error beyond).
  - `UpdatePhoto(copyID, photoID, caption, now)`.
  - `RemovePhoto(copyID, photoID, now)`: also clears the game's cover photo if it was that one.
  - `ReorderPhotos(copyID, ids []PhotoID, now)`: `ids` must be exactly the copy's photos.
- `Info.CoverPhoto PhotoID`: the game's cover is one of its copies' photos. It takes precedence over
  `CoverURL`; `UpdateInfo` refuses a photo that none of the game's copies has. Setting or clearing it
  reports `coverChanged`, so the cached cover is refreshed. Editing the game (`UpdateGame`, whose
  request has no cover photo) keeps the cover photo, unless the custom cover URL changed: then the
  user chose another cover and the cover photo is cleared.
- Moving or merging a copy keeps its photos. Removing a copy, or a photo, clears the cover photo
  when no remaining copy of the game has that photo. `TakenAt` is the camera's clock reading (EXIF
  has no zone), stored as UTC and shown as a date in UTC.
  Scans (`applyImport`) never touch photos.

## Storage

### Documents (`internal/adapters/outbound/sqlite`)

- `copyDoc` gains `photos` (array of `{id, caption, takenAt, addedAt}`), omitted when empty;
  `gameDoc` gains `coverPhoto`. Adding fields keeps the game document at version 2.

### Photo files (`internal/adapters/outbound/photostore`, port `media.PhotoStore`)

- Layout: `config/photos/<first two hex chars>/<id>.jpg` and `<id>-thumb.jpg`.
- `Put(photo, thumb []byte) (PhotoID, error)`: computes the SHA-256 of `photo`, writes both files
  atomically (temporary file + rename) unless they exist; idempotent.
- `Open(id, thumb bool)`: the file for serving.
- `Prune(referenced set, olderThan time.Duration)`: deletes the files no copy references and that
  are older than `olderThan` (one day), so a photo still being attached, or removed by mistake and
  added back, survives. It runs an hour after start-up and then once a day. Uploading a photo that
  is already stored marks its files as just written, so an old unreferenced file uploaded again is
  not pruned before it is attached.
- `Link(id, dir)`: adds the photo and its thumbnail to another store directory as hard links,
  copying when the file systems do not allow links; used by backups. Hard links are safe because a
  stored file is never changed in place: its name is its content's hash.

## Upload and serving

### In the browser (`web/src/lib/photos.ts`)

For each file the user picks:

1. Decode it with `createImageBitmap(file, { imageOrientation: 'from-image' })` (the rotation is
   applied). A format the browser cannot decode (HEIC in Chrome) fails with a message asking for a
   JPEG.
2. Draw it on a canvas at most 2560 px on its long side and encode JPEG quality 0.85; draw a thumbnail
   at most 400 px, quality 0.8.
3. If the original is a JPEG with an EXIF (APP1) segment, insert that segment into the reduced photo
   after its SOI marker, with the IFD0 orientation tag (0x0112) set to 1, because the pixels are
   already upright. Other formats are uploaded without metadata. The metadata, GPS location
   included, is kept on purpose (the user's decision); the docs say so.

### On the server

- `POST /media/photos` (multipart form: `photo`, `thumb`): the same access control as every
  `/media/` route, plus a required `X-Gamevault-Upload` header. A page on another site cannot send
  that header without a CORS preflight the server never grants, so it cannot upload photos through a
  browser on a trusted network (where no session cookie is needed and SameSite does not help). Each part must be a JPEG (`image.DecodeConfig`), the photo at most 8000 px and
  15 MB, the thumbnail at most 512 px. The server computes the id itself, reads `DateTimeOriginal`
  from the EXIF segment if present (a small TIFF/IFD reader, no dependency), stores the files and
  answers `{"id": "…", "takenAt": "…"}`. Invalid input is a 400 with a message that says what is
  wrong.
- `GET /media/photos/{id}` and `GET /media/photos/{id}/thumb`: the JPEG, with
  `Cache-Control: private, max-age=31536000, immutable` (the content never changes).
- Connect, in `GameService`: `AddCopyPhotos(game_id, copy_id, photos[{id, caption, taken_at}])`,
  `UpdateCopyPhoto(game_id, copy_id, photo_id, caption)`, `RemoveCopyPhoto(game_id, copy_id,
  photo_id)`, `ReorderCopyPhotos(game_id, copy_id, photo_ids)`, `SetCoverPhoto(game_id, photo_id)`
  (empty clears it). Each returns the updated `Game`. `AddCopyPhotos` refuses an id whose file is
  not in the store.
- `Copy` gains `repeated Photo photos` (`id`, `caption`, `taken_at`, `added_at`); `Game` gains
  `cover_photo_id`. The cover endpoint (`/media/covers/{id}`) serves the cover photo when set.

## Backups (`internal/application/system`)

- **Create:** after `VACUUM INTO backups/gamevault-<ts>.db`, write `backups/gamevault-<ts>.photos`
  (the ids of every photo the catalog references, one per line) and link each into the shared store
  `backups/photos/` (`PhotoStore.Link`), skipping those already there.
- **Prune:** rotation deletes a backup's `.db` and `.photos` together; then every photo in
  `backups/photos/` that no remaining `.photos` list names is deleted. Backups without a list
  (older ones, `pre-migration-*.db`) count as having no photos.
- **List:** each backup shows how many photos it covers; the page shows the shared store's total
  size once.
- **Restore** (documented in `docs/technical.md`): stop the server; replace `config/gamevault.db`
  with the backup and delete `gamevault.db-wal` / `-shm`; copy `config/backups/photos/` over
  `config/photos/`; start the server. The daily prune then removes photos the restored catalog does
  not reference.

## Image viewer (`web/src/components/Lightbox.tsx`)

One viewer for every image gallery (game-sheet screenshots, copy photos), rebuilt for usability:

- **Always-visible controls:** a close button (top right), previous / next buttons on the sides
  (large hit areas; hidden with a single image), a counter ("3 / 12"), zoom out / zoom in / reset,
  and a full-screen toggle (the Fullscreen API; hidden where the browser lacks it, e.g. iPhone
  Safari).
- **Zoom:** double-click or double-tap toggles 1× ↔ 2.5×; the zoom buttons step by 1.5× up to 5×;
  Ctrl/⌘ + wheel and trackpad pinch zoom at the pointer; two-finger pinch on touch screens. While
  zoomed, dragging pans (kept inside the image's bounds) and swipe does not change image.
- **Navigation:** ← / → keys, swipe left / right on touch (when not zoomed), the side buttons;
  wraps around. The neighbouring images are preloaded.
- **Closing:** the close button, Esc (first leaves full screen if active), or a click on the dark
  backdrop (never on the image, so clicking to zoom or pan cannot close it).
- **Footer slot:** an optional area under the image for per-image content; copy photos put the
  caption (editable in place), the date taken, "Move left" / "Move right", "Use as cover" /
  "Stop using as cover" and "Delete" (with confirmation) there. Screenshots show nothing.
- **Accessibility:** `role="dialog"`, `aria-modal`, labelled buttons, focus moved into the viewer and
  restored on close, keyboard reachable controls.
- **Motion:** opening and closing fade and scale (0.97 → 1, ≤ 240 ms, `--ease-out`); image changes
  are not animated; `prefers-reduced-motion` collapses it (see `.claude/docs/ui.md`).
- Zoom is reset when the image changes.

## Copy UI

- Each copy card in the game sheet shows a strip of thumbnails (up to six, then "+N") and an
  "Add photos" button: a file input with `accept="image/*" multiple` (on phones it offers the camera
  and the gallery). Each upload shows its progress in the strip; a failed one shows why, the others
  continue.
- Clicking a thumbnail opens the viewer on that copy's photos, with the footer above.
- System → Backups shows the photos per backup and the store's size.
- Every text in `en.json` and `es.json`.

## Testing

- Domain: add (dedupe, limit 50), caption, remove (clears the cover photo), reorder (must be the same
  set), cover photo validation, moving a copy keeps its photos, scans keep them.
- Documents: round trip with photos and a cover photo.
- Photo store: put is idempotent and atomic, open, prune by reference and age, link (hard link, and
  copy when linking fails).
- Upload handler: a valid JPEG pair; a non-JPEG, an oversized image, a missing part (400s); the
  date taken read from a fixture with EXIF; a repeated upload gives the same id.
- EXIF reader: big- and little-endian TIFF, missing DateTimeOriginal, truncated segments (no panic).
- Backups: create links photos and writes the list; prune deletes only unreferenced store photos;
  a second backup adds nothing new; restore steps documented.
- Connect end-to-end: upload, attach, caption, reorder, cover, remove.
- Web: `photos.ts` EXIF splicing (orientation reset, segment preserved) checked in Node with a
  fixture; the viewer and the copy UI on the test server, desktop (buttons, keyboard, zoom, full
  screen) and mobile (swipe, pinch, camera picker).

## Delivery

Branch `feature/copy-photos`, one PR to `main`, CI green before merging. `docs/technical.md`
(photos, store, backups, restore, metadata kept), `.claude/docs/ui.md` (the viewer) and the
data-model memory updated. No version is tagged until the user asks.
