# Continuous barcode scanning with a list sent at once

Status: approved design, waiting for the implementation plan. Branch `feature/batch-scan`.

## Why

Registering a shelf of physical games means scanning box after box with the phone. Today the
camera freezes on every code until the user adds or skips it, so each box costs a tap and a
glance. Collectors expect the CLZ-style flow: the camera keeps reading, every box joins a list,
and the list is sent at once at the end. Nothing may interrupt the scanning session.

## Decisions taken with the user

- **The list lives in the browser** (`localStorage` on the phone): it survives a reload, a closed
  tab or a locked screen, and is seen only on that device. Nothing reaches the server until it is
  sent.
- **Repeats are always ignored within the session:** scanning a code already in the list gives a
  distinct sound and vibration and a short "already in the list". A real second copy is added with
  "+1" on its row. A code already in the collection shows "you have it" and is not sent unless "+1".
- **Send adds everything ready and keeps the rest:** rows to review (unknown code, no platform,
  several possible games) stay in the list to be fixed and sent later.
- **One request for the whole list (`AddScannedCopies`):** the server groups rows and saves them in
  one transaction, with a result per row.

## Scanning session

- **The camera never pauses.** A code counts once read twice with a valid check digit, and a box
  is ignored while it stays in view (both as today).
- **Feedback on every read, without interrupting:**
  - new: a short vibration, a high beep, a green flash of the frame and, under the video for a
    moment, the code and "added to the list";
  - repeat: a double vibration, a low tone, an amber flash and "already in the list";
  - unreadable or invalid: ignored silently.
  - A mute button next to the camera button; remembered on the device.
- **The list**, under the camera, newest first. Each row: thumbnail, title, platform and a state
  that updates by itself:
  - **Looking up…**
  - **Ready** (as a new game, or as a copy of one of yours)
  - **You have it** (not sent unless "+1")
  - **Review** (unknown code, no platform, or several possible games)
  - **Error** (with "Retry")
- **Lookups are queued, one at a time, in the background** (`IdentifyBarcode`), to respect the
  barcode databases' limits: 30 boxes in a minute never make 30 requests at once. Rows still
  looking up when the page reloads are looked up again.
- **USB / Bluetooth reader or typing:** each Enter puts the code in the list and leaves the field
  empty and focused. Same list, no camera.
- **The batch defaults** (platform, grade, contents, location) stay as today and are applied
  **when sending**, so changing them mid-session applies to the whole list.

## Reviewing and sending

- **A fixed bar at the bottom** (in reach of the thumb on a phone): "12 ready · 2 to review · 1 you
  have" and **Send 12**. While lookups run it reads "Send 12 (3 looking up…)" and still sends what
  is ready.
- **Tapping a row expands it in place** (one at a time) with what today's result card offers:
  editable title and platform, title search, cover choice, "copy of «X»" or "new game". Fixing a
  Review row makes it Ready. The camera keeps reading meanwhile; new codes join the top of the list
  without closing the open row.
- **A row resolves by itself** as today's card does: a database match with a platform and a
  suggested game is Ready with the first suggestion; exactly one catalog game with that title makes
  it a copy of that game; several make it Review.
- **Row actions:** "+1" (another copy of the same code) and remove (× button), with no
  confirmation and "Undo" for a few seconds.
- **Platform:** the batch platform, when set, wins over the code's (as today). A row with no
  platform from anywhere is Review.
- **Send:** one request with every Ready row (and "you have it" rows with "+1"). Saved rows leave
  the list for an "Added in this session" strip with covers that open the game (as today); failed
  rows keep their error; Review rows stay.
- **Clear:** a "Clear list" button at the end, with "Undo". The list never clears by itself.

## Server

### API (`proto/gamevault/v1/game.proto`, `GameService`)

```proto
rpc AddScannedCopies(AddScannedCopiesRequest) returns (AddScannedCopiesResponse);

message ScannedCopy {
  // The row's id on the device, echoed in its result.
  string client_id = 1;
  // The game to add the copy to; empty: a new game.
  string game_id = 2;
  // For a new game: its title and the chosen cover.
  string title = 3;
  string cover_url = 4;
  // The copy, barcode and batch defaults included.
  CopyDetails details = 5;
}
message AddScannedCopiesRequest { repeated ScannedCopy items = 1; } // at most 200
message ScannedCopyResult {
  string client_id = 1;
  string game_id = 2;
  string copy_id = 3;
  // Empty when saved; otherwise what happened and what to do.
  string error = 4;
}
message AddScannedCopiesResponse {
  repeated ScannedCopyResult results = 1;
  // Every game created or changed, for the page's state.
  repeated Game games = 2;
}
```

### Service (`internal/application/catalog`)

`AddScannedCopies(ctx, items []ScannedCopy) ([]ScannedResult, []*game.Game, error)`, in one
transaction:

- Items with a `game_id` are grouped by game: one save per game, whatever the number of copies.
- Items without one are grouped by `game.MatchKey(title)`: each group creates one game holding all
  its copies, with the cover of the first item that has one. Games already in the catalog are
  never matched by title: the row already chose between "copy of «X»" and "new game".
- **Errors are per item:** a missing target game ("the game no longer exists; choose another one")
  or invalid copy details fail that item; the rest are saved. An error of the store itself fails
  the whole request.
- More than 200 items, or an item without a title and without a `game_id`, refuses the whole
  request as invalid input (the page never sends either: it splits longer lists into several
  requests and only sends Ready rows).
- Physical copies with a barcode join the price schedule and new games' covers resolve exactly as
  with `CreateGame` / `AddCopy`.

## Web (`web/src/features/scan/`)

- `scanList.ts` (pure, no React): add a code (repeat detection), "+1", remove and undo, the row
  state from an `IdentifyBarcode` answer, the send items (batch defaults applied), saving to and
  loading from `localStorage` with a format version.
- `lookupQueue.ts`: one lookup at a time, rows left "looking up" after a reload are looked up
  again, manual retry.
- `feedback.ts`: tones (Web Audio, no files) and vibration, with the mute setting.
- `ScanPage.tsx`: composition of camera, code field, batch bar, list and send bar. Today's result
  card becomes the expanded detail of a row.
- `CameraScanner.tsx`: never paused; flashes the frame green (new) or amber (repeat).
- When `localStorage` is unavailable (private mode) the list works in memory and says once that it
  will not be kept if the page closes.
- **Errors:** a failed lookup (network, database down) leaves its row in Error with Retry and the
  queue goes on; a failed send (no network) leaves every row as it was, with a message; per-item
  errors show on their rows.
- Every text in `en.json` and `es.json`.

## Testing

- Go: the service (grouping by `MatchKey`, existing target, a deleted game, one item's error not
  blocking the others, one transaction, the 200 limit) and the RPC end to end.
- Node: `scanList` (repeats, "+1", states, send items with defaults, storage round trip and an
  older or broken format) and `lookupQueue` (one at a time, reload, retry), like `exif.test.mjs`.
- Browser, on the test server with a copy of the config: scan in a row by typing real codes from
  the catalog and new ones, review, send and check the games created, on desktop and the mobile
  preset. The real camera is tested by the user on the phone.

## Delivery

Branch `feature/batch-scan`, one PR to `main`, CI green before merging. `docs/technical.md`
(scanning and the new RPC). No version is tagged until the user asks.
