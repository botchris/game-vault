# Continuous Barcode Scanning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The scan page keeps the camera reading, collects every box in a list kept on the device, looks codes up one at a time in the background, and sends the ready rows at once through a new `AddScannedCopies` RPC that groups and saves them in one transaction.

**Architecture:** Server: a new catalog use case `AddScannedCopies` (groups by target game id, or by `game.MatchKey(title)` for new games; per-item errors; one transaction) exposed as `GameService.AddScannedCopies`. Web: pure modules (`lib/barcode.ts`, `features/scan/scanList.ts`, `features/scan/lookupQueue.ts`) tested in Node, a `feedback.ts` for tones and vibration, a non-pausing `CameraScanner`, and a rewritten `ScanPage` composed of the list, an expandable row detail and a fixed send bar.

**Tech Stack:** Go 1.26, ConnectRPC/buf, SQLite; React 19 + TypeScript, i18next; Node 24 test runner (TS files imported directly, type stripping only: no `enum` in files tests import).

**Spec:** `docs/superpowers/specs/2026-10-11-batch-scan-design.md`

## Global Constraints

- Everything compiles and tests in the toolchain container through Task (`task lint`, `task test`, `task generate`); no host tools.
- Everything in the repository is English; every UI string through `t()` with keys in both `en.json` and `es.json`; `task i18n` passes.
- Go: load the `write-go` skill before writing Go; doc comment on every exported identifier; one struct field per line; blank line between documented members; tests GIVEN/WHEN/THEN with time-bounded contexts.
- At most 200 items per `AddScannedCopies` request; the page splits longer lists.
- Lookups: one `IdentifyBarcode` at a time.
- The list lives only in the browser (`localStorage`, format version 1); nothing reaches the server until Send.
- Repeats within the session are always ignored (distinct feedback); a second copy only through "+1".
- Send adds every Ready row (and "you have it" rows with "+1"); Review rows stay.
- Batch platform, when set, wins over the code's; grade, contents and location are applied at Send.
- Files the Node tests import must not import generated protobuf code or `lib/model.ts` (they use TypeScript `enum`s, which Node's type stripping rejects).
- Tests never touch `config/` or port 8080; browser checks on the test server (port 8093) with a config copy.

## Review Focus

- A list saved by an older or broken page (bad JSON, another version, missing fields) must load as an empty list, never crash the page.
- UPC-A (12 digits) and its EAN-13 form (leading 0) are the same box: the second is a repeat.
- Two rows for the same new game (different barcodes, same title with different case or punctuation) must create one game with two copies.
- A send that fails for some items (deleted target game, a bad barcode) must keep exactly those rows, with their error, and remove the saved ones.
- Rows still "looking up" when the page reloads must be looked up again, once each.

---

### Task 1: Catalog use case `AddScannedCopies`

**Files:**
- Create: `internal/application/catalog/scanned.go`
- Test: `internal/application/catalog/scanned_test.go`

**Interfaces:**
- Consumes: `game.Repository`, `port.TxManager`, `game.New`, `(*game.Game).AddCopy`, `(*game.Game).UpdateInfo`, `game.MatchKey`, `game.ErrGameNotFound`, `*game.ValidationError`.
- Produces:
  - `const MaxScannedCopies = 200`
  - `var ErrInvalidScannedCopies error`, `var ErrScannedGameGone error`
  - `type ScannedCopy struct { Ref string; GameID game.ID; Title string; CoverURL string; Details game.CopyDetails }`
  - `type ScannedResult struct { Ref string; GameID game.ID; CopyID game.ID; Err error }`
  - `func (s *Service) AddScannedCopies(ctx context.Context, items []ScannedCopy) ([]ScannedResult, []*game.Game, error)`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing test**

```go
package catalog_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/game"
)

func physical(platform, barcode string) game.CopyDetails {
	b, err := game.ParseBarcode(barcode)
	if err != nil {
		panic(err)
	}

	return game.CopyDetails{
		Kind:     game.KindPhysical,
		Status:   game.StatusOwned,
		Platform: platform,
		Barcode:  b,
	}
}

func TestAddScannedCopies(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*catalog.Service, *game.Game) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, nil, nil)
		halo, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, nil)
		require.NoError(t, err)

		return svc, halo
	}

	t.Run("GIVEN a scanning session with copies of a game you have and of a new game twice", func(t *testing.T) {
		svc, halo := setup(t)
		items := []catalog.ScannedCopy{
			{Ref: "a", GameID: halo.ID(), Details: physical("Xbox 360", "882224536691")},
			{Ref: "b", Title: "Dead Space 3", CoverURL: "https://example.com/ds3.jpg", Details: physical("Xbox 360", "5030934110075")},
			{Ref: "c", Title: "dead space  3!", Details: physical("PS3", "5030941110075")},
			{Ref: "d", GameID: halo.ID(), Details: physical("Xbox 360", "882224536691")},
		}

		t.Run("WHEN they are saved", func(t *testing.T) {
			results, games, err := svc.AddScannedCopies(ctx, items)
			require.NoError(t, err)

			t.Run("THEN every item has its copy and no error", func(t *testing.T) {
				require.Len(t, results, 4)

				for _, r := range results {
					assert.NoError(t, r.Err, r.Ref)
					assert.NotEmpty(t, r.CopyID, r.Ref)
				}
			})

			t.Run("AND the new game is created once, with both copies and the first cover", func(t *testing.T) {
				require.Len(t, games, 2)

				ds3 := games[1]
				assert.Equal(t, "Dead Space 3", ds3.Title())
				assert.Len(t, ds3.Copies(), 2)
				assert.Equal(t, "https://example.com/ds3.jpg", ds3.CoverURL())
				assert.Equal(t, ds3.ID(), results[1].GameID)
				assert.Equal(t, ds3.ID(), results[2].GameID)
			})

			t.Run("AND the game you have gets both copies in one save", func(t *testing.T) {
				assert.Equal(t, halo.ID(), games[0].ID())
				assert.Len(t, games[0].Copies(), 2)

				all, err := svc.ListGames(ctx)
				require.NoError(t, err)
				assert.Len(t, all, 2)
			})
		})
	})

	t.Run("GIVEN a copy for a game deleted meanwhile next to a valid one", func(t *testing.T) {
		svc, halo := setup(t)
		require.NoError(t, svc.DeleteGame(ctx, halo.ID()))

		results, games, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{
			{Ref: "gone", GameID: halo.ID(), Details: physical("Xbox 360", "882224536691")},
			{Ref: "ok", Title: "Dead Space 3", Details: physical("Xbox 360", "5030934110075")},
		})
		require.NoError(t, err)

		t.Run("THEN only that copy fails, saying to choose another game", func(t *testing.T) {
			assert.ErrorIs(t, results[0].Err, catalog.ErrScannedGameGone)
			assert.NoError(t, results[1].Err)
			assert.Len(t, games, 1)
		})
	})

	t.Run("GIVEN a copy the domain refuses next to a valid one for the same new game", func(t *testing.T) {
		svc, _ := setup(t)
		bad := physical("Xbox 360", "5030934110075")
		bad.Grade = game.Grade("scratched") // not a grade the domain knows

		results, games, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{
			{Ref: "bad", Title: "Dead Space 3", Details: bad},
			{Ref: "ok", Title: "Dead Space 3", Details: physical("PS3", "5030941110075")},
		})
		require.NoError(t, err)

		t.Run("THEN the bad copy fails alone and the game is created with the other", func(t *testing.T) {
			var v *game.ValidationError
			assert.True(t, errors.As(results[0].Err, &v))
			assert.NoError(t, results[1].Err)
			require.Len(t, games, 1)
			assert.Len(t, games[0].Copies(), 1)
		})
	})

	t.Run("GIVEN requests the scan page never sends", func(t *testing.T) {
		svc, _ := setup(t)
		tooMany := make([]catalog.ScannedCopy, catalog.MaxScannedCopies+1)

		for i := range tooMany {
			tooMany[i] = catalog.ScannedCopy{Title: "X", Details: physical("PS3", "")}
		}

		t.Run("THEN more than the limit, or a new game without a title, is invalid input", func(t *testing.T) {
			_, _, err := svc.AddScannedCopies(ctx, tooMany)
			assert.ErrorIs(t, err, catalog.ErrInvalidScannedCopies)

			_, _, err = svc.AddScannedCopies(ctx, []catalog.ScannedCopy{{Title: " ", Details: physical("PS3", "")}})
			assert.ErrorIs(t, err, catalog.ErrInvalidScannedCopies)
		})
	})
}
```

The names used (`game.StatusOwned`, `game.KindPhysical`, `game.Grade`, `game.ParseBarcode("")` returning an empty barcode) were checked against `internal/domain/game` while planning; `AddCopy` refuses an unknown grade with a `*game.ValidationError`.

- [ ] **Step 3: Run it to verify it fails**

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-gomod:/go/pkg/mod -v gamevault-gobuild:/root/.cache/go-build gamevault-toolchain:446777a3814f go test ./internal/application/catalog/ -run TestAddScannedCopies`
Expected: FAIL to compile: `svc.AddScannedCopies undefined`, `undefined: catalog.ScannedCopy`.

- [ ] **Step 4: Write the implementation** (`internal/application/catalog/scanned.go`)

```go
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

// MaxScannedCopies is the most copies AddScannedCopies saves in one call.
const MaxScannedCopies = 200

var (
	// ErrInvalidScannedCopies means a request the scan page never sends: too many copies, or a
	// new game without a title.
	ErrInvalidScannedCopies = errors.New("invalid scanned copies")

	// ErrScannedGameGone is the error of a scanned copy whose game was deleted meanwhile.
	ErrScannedGameGone = errors.New("the game no longer exists; choose another one")
)

// ScannedCopy is one box of a scanning session to save.
type ScannedCopy struct {
	// Ref identifies the box on the scanning device; its result carries it back.
	Ref string

	// GameID is the game to add the copy to; empty: a new game titled Title.
	GameID game.ID

	// Title is the new game's title when GameID is empty.
	Title string

	// CoverURL is the new game's chosen cover, if any.
	CoverURL string

	// Details is the copy itself.
	Details game.CopyDetails
}

// ScannedResult is what happened to one ScannedCopy.
type ScannedResult struct {
	// Ref is the ScannedCopy's Ref.
	Ref string

	// GameID is the game the copy was added to; empty when Err is set.
	GameID game.ID

	// CopyID is the new copy; empty when Err is set.
	CopyID game.ID

	// Err says why the copy was not saved; nil when it was.
	Err error
}

// AddScannedCopies saves the boxes of a scanning session in one transaction. Copies for the same
// game are saved together; copies for new games are grouped by their title's match key, so two
// discs of a game you did not have yet make one game with two copies. A copy that cannot be saved
// (its game was deleted, the domain refuses it) fails alone; only a storage error fails the call.
// It returns a result per item, in order, and every game created or changed.
func (s *Service) AddScannedCopies(ctx context.Context, items []ScannedCopy) ([]ScannedResult, []*game.Game, error) {
	if len(items) > MaxScannedCopies {
		return nil, nil, fmt.Errorf("%w: at most %d copies at once", ErrInvalidScannedCopies, MaxScannedCopies)
	}

	for _, it := range items {
		if it.GameID == "" && strings.TrimSpace(it.Title) == "" {
			return nil, nil, fmt.Errorf("%w: a new game needs a title", ErrInvalidScannedCopies)
		}
	}

	var (
		results []ScannedResult
		changed []*game.Game
	)

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Start over on every attempt, so a retried transaction reports only what it saved.
		results = make([]ScannedResult, len(items))
		changed = nil
		now := s.now()

		for i, it := range items {
			results[i].Ref = it.Ref
		}

		for _, group := range groupScanned(items) {
			g, err := s.scannedTarget(ctx, items, group, now)
			if err != nil {
				var v *game.ValidationError
				if !errors.Is(err, game.ErrGameNotFound) && !errors.As(err, &v) {
					return err
				}

				if errors.Is(err, game.ErrGameNotFound) {
					err = ErrScannedGameGone
				}

				for _, i := range group {
					results[i].Err = err
				}

				continue
			}

			added := false

			for _, i := range group {
				c, err := g.AddCopy(items[i].Details, now)
				if err != nil {
					results[i].Err = err
					continue
				}

				results[i].GameID, results[i].CopyID = g.ID(), c.ID
				added = true
			}

			if !added {
				continue
			}

			if err := s.games.Save(ctx, g); err != nil {
				return err
			}

			changed = append(changed, g)
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return results, changed, nil
}

// scannedTarget returns the game a group of items goes to: the existing game, or a new one with
// the group's title and the first cover one of its items chose.
func (s *Service) scannedTarget(ctx context.Context, items []ScannedCopy, group []int, now time.Time) (*game.Game, error) {
	first := items[group[0]]
	if first.GameID != "" {
		return s.games.Get(ctx, first.GameID)
	}

	g, err := game.New(first.Title, now)
	if err != nil {
		return nil, err
	}

	for _, i := range group {
		if items[i].CoverURL == "" {
			continue
		}

		if _, err := g.UpdateInfo(game.Info{Title: g.Title(), CoverURL: items[i].CoverURL}, now); err != nil {
			return nil, err
		}

		break
	}

	return g, nil
}

// groupScanned groups the items' indexes by the game they go to, in order of first appearance: an
// existing game by its id, a new game by its title's match key.
func groupScanned(items []ScannedCopy) [][]int {
	var groups [][]int

	index := map[string]int{}

	for i, it := range items {
		key := "id:" + string(it.GameID)
		if it.GameID == "" {
			key = "new:" + game.MatchKey(it.Title)
		}

		n, ok := index[key]
		if !ok {
			n = len(groups)
			index[key] = n
			groups = append(groups, nil)
		}

		groups[n] = append(groups[n], i)
	}

	return groups
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: the command of Step 3.
Expected: `ok  gamevault/internal/application/catalog`.

- [ ] **Step 6: Lint and commit**

Run: `task lint` — Expected: `0 issues.`

```bash
git add internal/application/catalog/scanned.go internal/application/catalog/scanned_test.go
git commit -m "Catalog: save a scanning session at once, grouped by game, with a result per copy"
```

---

### Task 2: `GameService.AddScannedCopies` RPC

**Files:**
- Modify: `proto/gamevault/v1/game.proto` (messages after `MoveCopyResponse`, rpc in `GameService`)
- Modify: `internal/adapters/inbound/rpc/game_handler.go`
- Generated: `internal/gen/...`, `web/src/gen/...` (`task generate`)
- Test: `internal/adapters/inbound/rpc/scanned_test.go`

**Interfaces:**
- Consumes: Task 1's `catalog.ScannedCopy`, `catalog.ScannedResult`, `catalog.MaxScannedCopies`, `catalog.ErrInvalidScannedCopies`; `detailsFromPB`, `gameToPB`, `toConnectError`.
- Produces (proto, TS names after generation): `gameClient.addScannedCopies({ items: ScannedCopy[] })` → `{ results: ScannedCopyResult[] (clientId, gameId, copyId, error), games: Game[] }`.

- [ ] **Step 1: Add the API** to `proto/gamevault/v1/game.proto`

```proto
// ScannedCopy is one box of a scanning session (see AddScannedCopies).
message ScannedCopy {
  // The row's id on the scanning device, echoed in its result.
  string client_id = 1;
  // The game to add the copy to; empty: a new game.
  string game_id = 2;
  // For a new game: its title and the chosen cover.
  string title = 3;
  string cover_url = 4;
  // The copy, barcode and batch defaults included.
  CopyDetails details = 5;
}
message AddScannedCopiesRequest {
  // At most 200.
  repeated ScannedCopy items = 1;
}
message ScannedCopyResult {
  string client_id = 1;
  string game_id = 2;
  string copy_id = 3;
  // Empty when saved; otherwise what happened and what to do.
  string error = 4;
}
message AddScannedCopiesResponse {
  repeated ScannedCopyResult results = 1;
  // Every game created or changed.
  repeated Game games = 2;
}
```

and in `service GameService`, after `MoveCopy`:

```proto
  // Saves the boxes of a scanning session in one transaction: copies of the same new game (by
  // title) become one game; a copy that cannot be saved fails alone, with its error.
  rpc AddScannedCopies(AddScannedCopiesRequest) returns (AddScannedCopiesResponse);
```

Run: `task generate` — Expected: generated Go and TS updated; `go build ./...` now fails: `*GameHandler does not implement GameServiceHandler (missing method AddScannedCopies)`.

- [ ] **Step 2: Write the failing end-to-end test** (`internal/adapters/inbound/rpc/scanned_test.go`)

```go
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

func TestAddScannedCopies_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	disc := func(platform, barcode string) *pb.CopyDetails {
		return &pb.CopyDetails{
			Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
			Status:   pb.CopyStatus_COPY_STATUS_OWNED,
			Platform: platform,
			Barcode:  barcode,
		}
	}

	t.Run("GIVEN two discs of a new game and one with a broken barcode", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		res, err := c.games.AddScannedCopies(ctx, connect.NewRequest(&pb.AddScannedCopiesRequest{Items: []*pb.ScannedCopy{
			{ClientId: "r1:0", Title: "Dead Space 3", Details: disc("Xbox 360", "5030934110075")},
			{ClientId: "r2:0", Title: "Dead Space 3", Details: disc("PS3", "5030941110075")},
			{ClientId: "r3:0", Title: "Halo 3", Details: disc("Xbox 360", "123")},
		}}))
		require.NoError(t, err)

		t.Run("THEN the game is created once with both discs", func(t *testing.T) {
			require.Len(t, res.Msg.Games, 1)
			assert.Len(t, res.Msg.Games[0].Copies, 2)
		})

		t.Run("AND every item has a result, the broken one with its error", func(t *testing.T) {
			byID := map[string]*pb.ScannedCopyResult{}
			for _, r := range res.Msg.Results {
				byID[r.ClientId] = r
			}

			require.Len(t, byID, 3)
			assert.Empty(t, byID["r1:0"].Error)
			assert.Equal(t, res.Msg.Games[0].Id, byID["r2:0"].GameId)
			assert.NotEmpty(t, byID["r2:0"].CopyId)
			assert.NotEmpty(t, byID["r3:0"].Error)
		})
	})

	t.Run("GIVEN more items than the limit", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		items := make([]*pb.ScannedCopy, 201)

		for i := range items {
			items[i] = &pb.ScannedCopy{Title: "X", Details: disc("PS3", "")}
		}

		_, err := c.games.AddScannedCopies(ctx, connect.NewRequest(&pb.AddScannedCopiesRequest{Items: items}))

		t.Run("THEN the request is refused as invalid", func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	})
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-gomod:/go/pkg/mod -v gamevault-gobuild:/root/.cache/go-build gamevault-toolchain:446777a3814f go test ./internal/adapters/inbound/rpc/ -run TestAddScannedCopies`
Expected: FAIL to compile (missing method `AddScannedCopies`).

- [ ] **Step 4: Write the handler** (in `game_handler.go`, after `MoveCopy`)

```go
// AddScannedCopies saves the boxes of a scanning session at once, with a result per box. A box
// whose details cannot be read (a broken barcode…) fails alone, like the ones the catalog refuses.
func (h *GameHandler) AddScannedCopies(ctx context.Context, req *connect.Request[pb.AddScannedCopiesRequest]) (*connect.Response[pb.AddScannedCopiesResponse], error) {
	if len(req.Msg.Items) > catalog.MaxScannedCopies {
		return nil, connect.NewError(connect.CodeInvalidArgument, catalog.ErrInvalidScannedCopies)
	}

	out := &pb.AddScannedCopiesResponse{}
	items := make([]catalog.ScannedCopy, 0, len(req.Msg.Items))

	for _, it := range req.Msg.Items {
		d, err := detailsFromPB(it.Details)
		if err != nil {
			out.Results = append(out.Results, &pb.ScannedCopyResult{ClientId: it.ClientId, Error: itemError(err)})
			continue
		}

		items = append(items, catalog.ScannedCopy{
			Ref:      it.ClientId,
			GameID:   game.ID(it.GameId),
			Title:    it.Title,
			CoverURL: it.CoverUrl,
			Details:  d,
		})
	}

	results, games, err := h.catalog.AddScannedCopies(ctx, items)
	if errors.Is(err, catalog.ErrInvalidScannedCopies) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err != nil {
		return nil, toConnectError(err)
	}

	for _, r := range results {
		res := &pb.ScannedCopyResult{
			ClientId: r.Ref,
			GameId:   string(r.GameID),
			CopyId:   string(r.CopyID),
		}
		if r.Err != nil {
			res.Error = itemError(r.Err)
		}

		out.Results = append(out.Results, res)
	}

	for _, g := range games {
		out.Games = append(out.Games, gameToPB(g))
	}

	return connect.NewResponse(out), nil
}

// itemError is the text of one item's error, without a Connect code in front.
func itemError(err error) string {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Message()
	}

	return err.Error()
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: the command of Step 3. Expected: `ok  gamevault/internal/adapters/inbound/rpc`.

- [ ] **Step 6: Lint, test, commit**

Run: `task lint` (Expected `0 issues.`), `task test` (Expected green; the TS type check still passes because nothing uses the new client method yet).

```bash
git add proto internal/gen web/src/gen internal/adapters/inbound/rpc/game_handler.go internal/adapters/inbound/rpc/scanned_test.go
git commit -m "API: AddScannedCopies saves a scanning session in one request"
```

---

### Task 3: Barcode helpers and the scan list (pure)

**Files:**
- Create: `web/src/lib/barcode.ts`
- Modify: `web/src/lib/model.ts` (re-export `validBarcode` from `./barcode`, delete its body there)
- Create: `web/src/features/scan/scanList.ts`
- Test: `web/tests/scanList.test.mjs`

**Interfaces:**
- Produces (`lib/barcode.ts`): `validBarcode(code: string): boolean`, `normalizeBarcode(code: string): string` (digits only; 12 digits → `'0' + digits`).
- Produces (`scanList.ts`):
  - types `Phase = 'looking' | 'done' | 'error'`, `Status = 'looking' | 'ready' | 'owned' | 'review' | 'error'`, `Answer`, `Choice`, `Row`, `SendItem`, `Removed`
  - `addCode(rows, raw, id) → { rows, outcome: 'added' | 'repeat' | 'invalid', row? }`
  - `settle(rows, id, answer?, error?)`, `retry(rows, id)`, `plusOne(rows, id)`, `choose(rows, id, choice)`
  - `remove(rows, id) → { rows, removed? }`, `restore(rows, removed)`
  - `resolved(row, batchPlatform) → Choice | null`, `status(row, batchPlatform) → Status`
  - `summary(rows, batchPlatform) → { ready, review, owned, looking, error, copies }`
  - `sendItems(rows, batchPlatform) → SendItem[]`
  - `applyResults(rows, results) → { rows, saved: { row, gameId }[] }`
  - `saveRows(rows) → string`, `loadRows(text: string | null) → Row[]`

- [ ] **Step 1: Write the failing test** (`web/tests/scanList.test.mjs`)

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { normalizeBarcode, validBarcode } from '../src/lib/barcode.ts';
import {
  addCode, applyResults, choose, loadRows, plusOne, remove, resolved, restore, retry, saveRows, sendItems, settle, status, summary,
} from '../src/features/scan/scanList.ts';

const DS3 = '5030934110075';
const answer = (over = {}) => ({
  barcode: DS3, owned: [], warnings: [], existing: [],
  match: { raw: 'Dead Space 3 X360', title: 'Dead Space 3', platform: 'Xbox 360', edition: '', providerId: 'cex' },
  suggestions: [{ title: 'Dead Space 3', platform: 'Xbox 360', coverUrl: 'https://c/ds3.jpg', thumbUrl: 'https://c/ds3-t.jpg', label: '' }],
  ...over,
});
const scanned = (code = DS3, id = 'r1') => addCode([], code, id).rows;

test('barcodes: check digit, and UPC-A as EAN-13', () => {
  assert.equal(validBarcode('5030934110075'), true);
  assert.equal(validBarcode('5030934110076'), false);
  assert.equal(normalizeBarcode('0-45496-59024-6'), '0045496590246');
  assert.equal(normalizeBarcode('5030934110075'), '5030934110075');
});

test('a new code joins the top of the list looking up; a repeat or an invalid code changes nothing', () => {
  let r = addCode([], DS3, 'r1');
  assert.equal(r.outcome, 'added');
  assert.deepEqual(r.rows.map((x) => [x.id, x.code, x.count, x.phase]), [['r1', DS3, 1, 'looking']]);
  r = addCode(r.rows, '045496590246', 'r2');
  assert.equal(r.rows[0].id, 'r2', 'newest first');
  const again = addCode(r.rows, '0045496590246', 'r3');
  assert.equal(again.outcome, 'repeat', 'UPC-A and its EAN-13 form are the same box');
  assert.equal(again.rows, r.rows);
  assert.equal(addCode(r.rows, '123', 'r4').outcome, 'invalid');
});

test('a row resolves by itself from the answer, the batch platform winning', () => {
  const rows = settle(scanned(), 'r1', answer());
  assert.equal(status(rows[0], ''), 'ready');
  assert.deepEqual(resolved(rows[0], ''), { title: 'Dead Space 3', platform: 'Xbox 360', edition: '', coverUrl: 'https://c/ds3.jpg', thumbUrl: 'https://c/ds3-t.jpg', gameId: '' });
  assert.equal(resolved(rows[0], 'PS3').platform, 'PS3');
});

test('one game of yours with that title makes it a copy of it; several make it a row to review', () => {
  const one = settle(scanned(), 'r1', answer({ existing: [{ id: 'g1', title: 'Dead Space 3' }] }));
  assert.equal(resolved(one[0], '').gameId, 'g1');
  assert.equal(status(one[0], ''), 'ready');
  const two = settle(scanned(), 'r1', answer({ existing: [{ id: 'g1', title: 'Dead Space 3' }, { id: 'g2', title: 'Dead Space 3' }] }));
  assert.equal(status(two[0], ''), 'review');
});

test('unknown codes, products without platform and no suggestion are to review until chosen', () => {
  const unknown = settle(scanned(), 'r1', answer({ match: null, suggestions: [] }));
  assert.equal(status(unknown[0], ''), 'review');
  const noPlatform = settle(scanned(), 'r1', answer({ match: { raw: 'x', title: 'x', platform: '', edition: '', providerId: 'cex' }, suggestions: [] }));
  assert.equal(status(noPlatform[0], ''), 'review');
  const chosen = choose(unknown, 'r1', { title: 'Halo 3', platform: 'Xbox 360', edition: '', coverUrl: '', thumbUrl: '', gameId: '' });
  assert.equal(status(chosen[0], ''), 'ready');
  const noPlatformChosen = choose(unknown, 'r1', { title: 'Halo 3', platform: '', edition: '', coverUrl: '', thumbUrl: '', gameId: '' });
  assert.equal(status(noPlatformChosen[0], ''), 'review');
  assert.equal(status(noPlatformChosen[0], 'PS3'), 'ready', 'the batch platform fills it');
});

test('a code you already have is not sent unless +1', () => {
  let rows = settle(scanned(), 'r1', answer({ owned: [{ gameId: 'g1', title: 'Dead Space 3', platform: 'Xbox 360' }] }));
  assert.equal(status(rows[0], ''), 'owned');
  assert.equal(rows[0].count, 0);
  assert.deepEqual(sendItems(rows, ''), []);
  rows = plusOne(rows, 'r1');
  assert.deepEqual(sendItems(rows, '').map((i) => [i.clientId, i.gameId]), [['r1:0', 'g1']]);
});

test('+1 pressed while looking up survives an owned answer', () => {
  const rows = settle(plusOne(scanned(), 'r1'), 'r1', answer({ owned: [{ gameId: 'g1', title: 'Dead Space 3', platform: 'Xbox 360' }] }));
  assert.equal(rows[0].count, 1);
});

test('failed lookups can be retried; send items carry one item per copy', () => {
  let rows = settle(scanned(), 'r1', undefined, 'network down');
  assert.equal(status(rows[0], ''), 'error');
  assert.equal(rows[0].error, 'network down');
  rows = retry(rows, 'r1');
  assert.equal(status(rows[0], ''), 'looking');
  rows = plusOne(settle(rows, 'r1', answer()), 'r1');
  assert.deepEqual(sendItems(rows, 'PS3'), [
    { clientId: 'r1:0', rowId: 'r1', gameId: '', title: 'Dead Space 3', coverUrl: 'https://c/ds3.jpg', platform: 'PS3', edition: '', barcode: DS3 },
    { clientId: 'r1:1', rowId: 'r1', gameId: '', title: 'Dead Space 3', coverUrl: 'https://c/ds3.jpg', platform: 'PS3', edition: '', barcode: DS3 },
  ]);
});

test('remove and restore put a row back where it was', () => {
  const rows = addCode(scanned(), '045496590246', 'r2').rows;
  const { rows: left, removed } = remove(rows, 'r1');
  assert.deepEqual(left.map((r) => r.id), ['r2']);
  assert.deepEqual(restore(left, removed).map((r) => r.id), ['r2', 'r1']);
});

test('results remove saved rows and keep failed ones with their error', () => {
  let rows = settle(scanned(), 'r1', answer());
  rows = settle(addCode(rows, '045496590246', 'r2').rows, 'r2', answer({ barcode: '0045496590246' }));
  rows = plusOne(rows, 'r2');
  const out = applyResults(rows, [
    { clientId: 'r1:0', gameId: 'g9', error: '' },
    { clientId: 'r2:0', gameId: 'g9', error: '' },
    { clientId: 'r2:1', gameId: '', error: 'the game no longer exists; choose another one' },
  ]);
  assert.deepEqual(out.rows.map((r) => [r.id, r.count, r.error]), [['r2', 1, 'the game no longer exists; choose another one']]);
  assert.deepEqual(out.saved.map((s) => [s.row.id, s.gameId]), [['r2', 'g9'], ['r1', 'g9']]);
});

test('summary counts rows by state and the copies Send would add', () => {
  let rows = settle(scanned(), 'r1', answer());
  rows = addCode(rows, '045496590246', 'r2').rows;
  assert.deepEqual(summary(rows, ''), { ready: 1, review: 0, owned: 0, looking: 1, error: 0, copies: 1 });
});

test('the list round-trips through storage; anything else loads empty', () => {
  const rows = settle(scanned(), 'r1', answer());
  assert.deepEqual(loadRows(saveRows(rows)), rows);
  assert.deepEqual(loadRows(null), []);
  assert.deepEqual(loadRows('{oops'), []);
  assert.deepEqual(loadRows(JSON.stringify({ v: 2, rows })), []);
  assert.deepEqual(loadRows(JSON.stringify({ v: 1, rows: [{ id: 'x' }, ...rows] })), rows, 'broken rows are dropped');
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `docker run --rm -v "$PWD:/src" -w /src/web gamevault-toolchain:446777a3814f sh -c 'node --test --test-timeout=10000 tests/scanList.test.mjs'`
Expected: FAIL: `Cannot find module '/src/web/src/lib/barcode.ts'`.

- [ ] **Step 3: Write `web/src/lib/barcode.ts`**

```ts
// Barcode helpers with no other dependency, so Node tests can import them.

/** Whether code is an EAN-13, UPC-A or EAN-8 with a valid check digit (spaces and dashes ignored). */
export function validBarcode(code: string): boolean {
  const d = code.replace(/[\s-]/g, '');
  if (!/^(\d{8}|\d{12}|\d{13})$/.test(d)) return false;
  let sum = 0;
  for (let i = d.length - 2, w = 3; i >= 0; i--, w = w === 3 ? 1 : 3) sum += Number(d[i]) * w;
  return (10 - (sum % 10)) % 10 === Number(d[d.length - 1]);
}

/** The code's digits, a UPC-A written as its EAN-13 (a leading 0), as the server stores it. */
export function normalizeBarcode(code: string): string {
  const d = code.replace(/\D/g, '');
  return d.length === 12 ? `0${d}` : d;
}
```

In `web/src/lib/model.ts`, replace the `validBarcode` function with `export { validBarcode } from './barcode';`.

- [ ] **Step 4: Write `web/src/features/scan/scanList.ts`**

```ts
// The scanning session's list: pure functions over plain rows, which the page keeps in
// localStorage. Nothing here imports generated code, so Node tests run it as is.
import { normalizeBarcode, validBarcode } from '../../lib/barcode';

const FORMAT = 1;

export type Phase = 'looking' | 'done' | 'error';
export type Status = 'looking' | 'ready' | 'owned' | 'review' | 'error';

/** The parts of an IdentifyBarcode answer a row keeps (plain data, so it can be stored). */
export interface Answer {
  barcode: string;
  owned: { gameId: string; title: string; platform: string }[];
  match: { raw: string; title: string; platform: string; edition: string; providerId: string } | null;
  suggestions: { title: string; platform: string; coverUrl: string; thumbUrl: string; label: string }[];
  existing: { id: string; title: string }[];
  warnings: string[];
}

/** What a row adds: the game (gameId, or a new one with title and cover) and the copy's platform and edition. */
export interface Choice {
  title: string;
  platform: string;
  edition: string;
  coverUrl: string;
  thumbUrl: string;
  gameId: string;
}

/** One scanned box. count is the copies to add: 1 for a new box, 0 for one you already have (until "+1"). */
export interface Row {
  id: string;
  code: string;
  count: number;
  phase: Phase;
  answer?: Answer;
  /** Set when the user chose in the row's detail; otherwise the row resolves by itself. */
  choice?: Choice;
  error?: string;
}

/** One copy to send (AddScannedCopies item, before the batch defaults). */
export interface SendItem {
  clientId: string;
  rowId: string;
  gameId: string;
  title: string;
  coverUrl: string;
  platform: string;
  edition: string;
  barcode: string;
}

/** A removed row and where it was, to undo. */
export interface Removed {
  row: Row;
  index: number;
}

const update = (rows: Row[], id: string, fn: (r: Row) => Row) => rows.map((r) => (r.id === id ? fn(r) : r));

/** Adds a scanned code at the top. A code already in the list (in any form) is a repeat. */
export function addCode(rows: Row[], raw: string, id: string): { rows: Row[]; outcome: 'added' | 'repeat' | 'invalid'; row?: Row } {
  if (!validBarcode(raw)) return { rows, outcome: 'invalid' };
  const code = normalizeBarcode(raw);
  const same = rows.find((r) => r.code === code);
  if (same) return { rows, outcome: 'repeat', row: same };
  const row: Row = { id, code, count: 1, phase: 'looking' };
  return { rows: [row, ...rows], outcome: 'added', row };
}

/** Records a lookup's answer, or its error. A box you already have is not added unless "+1". */
export function settle(rows: Row[], id: string, answer?: Answer, error?: string): Row[] {
  return update(rows, id, (r) => {
    if (error !== undefined || !answer) return { ...r, phase: 'error', error: error ?? 'no answer' };
    const count = answer.owned.length ? Math.max(0, r.count - 1) : r.count;
    return { ...r, phase: 'done', answer, count, error: undefined };
  });
}

/** Looks a failed row up again. */
export function retry(rows: Row[], id: string): Row[] {
  return update(rows, id, (r) => ({ ...r, phase: 'looking', error: undefined }));
}

/** One more copy of the same box. */
export function plusOne(rows: Row[], id: string): Row[] {
  return update(rows, id, (r) => ({ ...r, count: r.count + 1 }));
}

/** The user's choice for a row (from its detail). */
export function choose(rows: Row[], id: string, choice: Choice): Row[] {
  return update(rows, id, (r) => ({ ...r, choice }));
}

export function remove(rows: Row[], id: string): { rows: Row[]; removed?: Removed } {
  const index = rows.findIndex((r) => r.id === id);
  if (index < 0) return { rows };
  return { rows: rows.filter((r) => r.id !== id), removed: { row: rows[index]!, index } };
}

export function restore(rows: Row[], removed?: Removed): Row[] {
  if (!removed || rows.some((r) => r.id === removed.row.id)) return rows;
  const out = [...rows];
  out.splice(Math.min(removed.index, out.length), 0, removed.row);
  return out;
}

/** What the row adds: the user's choice, or what the answer suggests; the batch platform wins. */
export function resolved(row: Row, batchPlatform: string): Choice | null {
  const a = row.answer;
  if (!a) return null;
  const owned = a.owned[0];
  if (owned) {
    return { title: owned.title, platform: batchPlatform || owned.platform, edition: '', coverUrl: '', thumbUrl: '', gameId: owned.gameId };
  }
  const s = a.suggestions[0];
  const base: Choice = row.choice ?? {
    // A product name without platform is usually not a game: no title then.
    title: s?.title ?? (a.match?.platform ? a.match.title : ''),
    platform: a.match?.platform || s?.platform || '',
    edition: a.match?.edition ?? '',
    coverUrl: s?.coverUrl ?? '',
    thumbUrl: s?.thumbUrl ?? '',
    gameId: a.existing.length === 1 ? a.existing[0]!.id : '',
  };
  return { ...base, platform: batchPlatform || base.platform };
}

export function status(row: Row, batchPlatform: string): Status {
  if (row.phase === 'looking') return 'looking';
  if (row.phase === 'error' || !row.answer) return 'error';
  if (row.answer.owned.length) return 'owned';
  const c = resolved(row, batchPlatform)!;
  if (!c.title.trim() || !c.platform.trim()) return 'review';
  // Resolved by itself only when there is a suggested game and at most one of yours to add it to.
  if (!row.choice && (row.answer.suggestions.length === 0 || row.answer.existing.length > 1)) return 'review';
  return 'ready';
}

export function summary(rows: Row[], batchPlatform: string) {
  const out = { ready: 0, review: 0, owned: 0, looking: 0, error: 0, copies: 0 };
  for (const r of rows) out[status(r, batchPlatform)]++;
  out.copies = sendItems(rows, batchPlatform).length;
  return out;
}

/** One item per copy of every Ready row, and of "you have it" rows with "+1". */
export function sendItems(rows: Row[], batchPlatform: string): SendItem[] {
  const items: SendItem[] = [];
  for (const r of rows) {
    const st = status(r, batchPlatform);
    if (st !== 'ready' && st !== 'owned') continue;
    const c = resolved(r, batchPlatform)!;
    for (let n = 0; n < r.count; n++) {
      items.push({
        clientId: `${r.id}:${n}`, rowId: r.id, gameId: c.gameId, title: c.title.trim(), coverUrl: c.coverUrl,
        platform: c.platform.trim(), edition: c.edition.trim(), barcode: r.code,
      });
    }
  }
  return items;
}

/**
 * Applies AddScannedCopies results: saved copies leave their row (the row leaves when none is
 * left), failed ones stay with the error. saved lists each row with a saved copy and its game.
 */
export function applyResults(rows: Row[], results: { clientId: string; gameId: string; error: string }[]) {
  const saved: { row: Row; gameId: string }[] = [];
  const next: Row[] = [];
  for (const r of rows) {
    const mine = results.filter((x) => x.clientId.startsWith(`${r.id}:`));
    if (!mine.length) {
      next.push(r);
      continue;
    }
    const ok = mine.filter((x) => !x.error);
    const failed = mine.filter((x) => x.error);
    if (ok.length) saved.push({ row: r, gameId: ok[0]!.gameId });
    if (failed.length) next.push({ ...r, count: failed.length, error: failed[0]!.error });
  }
  return { rows: next, saved };
}

export function saveRows(rows: Row[]): string {
  return JSON.stringify({ v: FORMAT, rows });
}

const isRow = (r: unknown): r is Row => {
  const x = r as Row;
  return !!x && typeof x.id === 'string' && typeof x.code === 'string' && typeof x.count === 'number'
    && ['looking', 'done', 'error'].includes(x.phase) && (x.phase !== 'done' || !!x.answer);
};

/** The stored list; empty when there is none, it is broken or it has another format. */
export function loadRows(text: string | null): Row[] {
  if (!text) return [];
  try {
    const data = JSON.parse(text) as { v?: number; rows?: unknown[] };
    if (data.v !== FORMAT || !Array.isArray(data.rows)) return [];
    return data.rows.filter(isRow);
  } catch {
    return [];
  }
}
```

`saved` follows the list order (newest first), which is the order the page prepends to "Added in this session".

- [ ] **Step 5: Run the test to verify it passes**

Run: the command of Step 2. Expected: all tests pass.

- [ ] **Step 6: Type check and commit**

Run: `task test` — Expected green (TS type check included).

```bash
git add web/src/lib/barcode.ts web/src/lib/model.ts web/src/features/scan/scanList.ts web/tests/scanList.test.mjs
git commit -m "Scan list: rows, repeats, states and send items as pure functions"
```

---

### Task 4: Lookup queue and feedback

**Files:**
- Create: `web/src/features/scan/lookupQueue.ts`
- Create: `web/src/features/scan/feedback.ts`
- Test: `web/tests/lookupQueue.test.mjs`

**Interfaces:**
- Produces: `createLookupQueue<A>(lookup: (code: string) => Promise<A>, done: (id: string, answer?: A, error?: unknown) => void) → { push(id, code): void; drop(id): void }`; `signal(kind: 'new' | 'repeat', muted: boolean): void`; `unlockAudio(): void`.

- [ ] **Step 1: Write the failing test** (`web/tests/lookupQueue.test.mjs`)

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createLookupQueue } from '../src/features/scan/lookupQueue.ts';

const flush = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setImmediate(r)); };

function fakeLookups() {
  const calls = [];
  let running = 0;
  let most = 0;
  const lookup = (code) => new Promise((resolve, reject) => {
    running++;
    most = Math.max(most, running);
    calls.push({ code, resolve: (v) => { running--; resolve(v); }, reject: (e) => { running--; reject(e); } });
  });
  return { calls, lookup, most: () => most };
}

test('codes are looked up one at a time, in order, and each answer reaches its row', async () => {
  const f = fakeLookups();
  const done = [];
  const q = createLookupQueue(f.lookup, (id, answer, error) => done.push([id, answer, error]));
  q.push('r1', '1');
  q.push('r2', '2');
  q.push('r3', '3');
  await flush();
  assert.deepEqual(f.calls.map((c) => c.code), ['1']);
  f.calls[0].resolve('a1');
  await flush();
  f.calls[1].reject(new Error('down'));
  await flush();
  f.calls[2].resolve('a3');
  await flush();
  assert.equal(f.most(), 1);
  assert.deepEqual(done.map(([id, a, e]) => [id, a, e?.message]), [['r1', 'a1', undefined], ['r2', undefined, 'down'], ['r3', 'a3', undefined]]);
});

test('a row pushed twice is looked up once; a dropped row is not looked up', async () => {
  const f = fakeLookups();
  const q = createLookupQueue(f.lookup, () => {});
  q.push('r1', '1');
  q.push('r2', '2');
  q.push('r2', '2');
  q.push('r3', '3');
  q.drop('r3');
  await flush();
  f.calls[0].resolve('a');
  await flush();
  f.calls[1].resolve('b');
  await flush();
  assert.deepEqual(f.calls.map((c) => c.code), ['1', '2']);
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `docker run --rm -v "$PWD:/src" -w /src/web gamevault-toolchain:446777a3814f sh -c 'node --test --test-timeout=10000 tests/lookupQueue.test.mjs'`
Expected: FAIL: cannot find module `lookupQueue.ts`.

- [ ] **Step 3: Write `web/src/features/scan/lookupQueue.ts`**

```ts
// Looks scanned codes up one at a time, in order: the barcode databases behind IdentifyBarcode
// have rate limits, and a shelf scanned in a minute must not become thirty requests at once.

export interface LookupQueue {
  /** Queues a row's code; a row already waiting is not queued twice. */
  push(id: string, code: string): void;
  /** Forgets a waiting row (it was removed from the list). */
  drop(id: string): void;
}

export function createLookupQueue<A>(
  lookup: (code: string) => Promise<A>,
  done: (id: string, answer?: A, error?: unknown) => void,
): LookupQueue {
  const waiting: { id: string; code: string }[] = [];
  let current = '';
  let running = false;

  const pump = async () => {
    if (running) return;
    running = true;
    while (waiting.length) {
      const next = waiting.shift()!;
      current = next.id;
      try {
        done(next.id, await lookup(next.code));
      } catch (e) {
        done(next.id, undefined, e);
      }
    }
    current = '';
    running = false;
  };

  return {
    push(id, code) {
      if (id === current || waiting.some((w) => w.id === id)) return;
      waiting.push({ id, code });
      void pump();
    },
    drop(id) {
      const i = waiting.findIndex((w) => w.id === id);
      if (i >= 0) waiting.splice(i, 1);
    },
  };
}
```

- [ ] **Step 4: Write `web/src/features/scan/feedback.ts`**

```ts
// What a read feels and sounds like, so the user keeps scanning without looking at the screen:
// a short high beep for a new box, two low tones for one already in the list. Tones are made with
// Web Audio (no files); vibration where the device has it.

let audio: AudioContext | null = null;

/** Browsers start audio only after a tap: call it from one (opening the camera, unmuting). */
export function unlockAudio() {
  try {
    audio ??= new AudioContext();
    void audio.resume();
  } catch {
    /* no Web Audio: vibration and the flash still tell */
  }
}

export function signal(kind: 'new' | 'repeat', muted: boolean) {
  navigator.vibrate?.(kind === 'new' ? 60 : [40, 60, 40]);
  if (muted || !audio) return;
  const tones = kind === 'new' ? [1320] : [330, 330];
  tones.forEach((frequency, i) => {
    const t0 = audio!.currentTime + i * 0.13;
    const osc = audio!.createOscillator();
    const gain = audio!.createGain();
    osc.type = 'sine';
    osc.frequency.value = frequency;
    gain.gain.setValueAtTime(0.0001, t0);
    gain.gain.exponentialRampToValueAtTime(0.25, t0 + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.1);
    osc.connect(gain).connect(audio!.destination);
    osc.start(t0);
    osc.stop(t0 + 0.11);
  });
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: the command of Step 2. Expected: both tests pass.

- [ ] **Step 6: Commit**

Run: `task test` — Expected green.

```bash
git add web/src/features/scan/lookupQueue.ts web/src/features/scan/feedback.ts web/tests/lookupQueue.test.mjs
git commit -m "Scan: one-at-a-time lookup queue, and tones and vibration for each read"
```

---

### Task 5: The camera keeps reading

**Files:**
- Modify: `web/src/features/scan/CameraScanner.tsx`
- Modify: `web/src/styles.css` (camera flash and toast)

**Interfaces:**
- Consumes: nothing new.
- Produces: `CameraScanner({ onCode: (code: string) => 'added' | 'repeat' | 'invalid' })` — no `paused` or `searching` props.

- [ ] **Step 1: Change the component.** Replace the props, drop the pause logic and add the flash:

```tsx
export default function CameraScanner({ onCode }: { onCode: (code: string) => 'added' | 'repeat' | 'invalid' }) {
  const { t } = useTranslation();
  const video = useRef<HTMLVideoElement>(null);
  const onCodeRef = useRef(onCode);
  onCodeRef.current = onCode;
  const [error, setError] = useState('');
  const [mirrored, setMirrored] = useState(false);
  // The last read, shown for a moment over the picture: green when added, amber when a repeat.
  const [flash, setFlash] = useState<{ kind: 'added' | 'repeat'; code: string; n: number } | null>(null);

  useEffect(() => {
    if (!flash) return;
    const timer = window.setTimeout(() => setFlash(null), 1200);
    return () => window.clearTimeout(timer);
  }, [flash]);
```

In `handle`, delete `if (pausedRef.current) return;` and the `navigator.vibrate?.(80);` line (feedback.ts vibrates now), and replace `onCodeRef.current(code);` with:

```tsx
        const outcome = onCodeRef.current(code);
        if (outcome !== 'invalid') setFlash((f) => ({ kind: outcome, code, n: (f?.n ?? 0) + 1 }));
```

In the native detector loop, change `if (!pausedRef.current && v.readyState >= 2)` to `if (v.readyState >= 2)`. Delete `pausedRef`, the `paused` effect that pauses the video, and update the doc comment: the camera never pauses; a box is ignored while it stays in view; each read flashes the frame and shows its code for a moment.

Render:

```tsx
  if (error) return <div className="alert warn">{error}</div>;
  return (
    <div className={`camera ${flash ? `flash-${flash.kind}` : ''} ${mirrored ? 'mirrored' : ''}`}>
      <video ref={video} muted playsInline />
      <div className="camera-guide" aria-hidden="true" />
      {flash ? (
        <p key={flash.n} className="camera-toast" role="status">
          <code>{flash.code}</code> · {t(flash.kind === 'added' ? 'scan.list.toastAdded' : 'scan.list.toastRepeat')}
        </p>
      ) : (
        <p className="camera-hint">{t(mirrored ? 'scan.camera.hintLaptop' : 'scan.camera.hint')}</p>
      )}
    </div>
  );
```

- [ ] **Step 2: CSS** — replace the `.camera.paused`, `.camera.found`, `.camera-found*` rules with:

```css
/* Each read flashes the guide: green for a new box, amber for one already in the list. */
.camera.flash-added .camera-guide { border-color: var(--ok); box-shadow: 0 0 0 9999px rgb(0 0 0 / .3), 0 0 0 4px rgb(23 128 61 / .45); }
.camera.flash-repeat .camera-guide { border-color: var(--accent); box-shadow: 0 0 0 9999px rgb(0 0 0 / .3), 0 0 0 4px rgb(255 176 32 / .45); }
.camera-toast {
  position: absolute; left: 50%; bottom: 12px; transform: translateX(-50%); margin: 0; padding: 7px 12px; border-radius: 999px;
  background: rgb(24 28 44 / .86); color: #fff; font-size: 13px; white-space: nowrap; animation: toast-in 160ms ease-out;
}
.camera-toast code { color: var(--accent); letter-spacing: .03em; }
@keyframes toast-in { from { opacity: 0; transform: translate(-50%, 4px); } }
@media (prefers-reduced-motion: reduce) { .camera-toast { animation: none; } }
```

Keep `.spinner` (the list uses it).

- [ ] **Step 3: Type check.** It fails until Task 6 updates `ScanPage` (it still passes `paused`/`searching`): do Task 6 before running `task test`, and commit both together at the end of Task 6.

---

### Task 6: The scan page: list, row detail, send bar

**Files:**
- Modify: `web/src/features/scan/ScanPage.tsx` (rewrite of `ScanPage`; `BatchBar` kept as is; `ScanResult` replaced by `RowDetail`)
- Create: `web/src/features/scan/ScanRow.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`

**Interfaces:**
- Consumes: Task 2 `gameClient.addScannedCopies`; Task 3 `scanList` functions; Task 4 `createLookupQueue`, `signal`, `unlockAudio`; Task 5 `CameraScanner({ onCode })`; existing `lookupClient.identifyBarcode`, `lookupClient.suggestGames`, `useAppData().{games, putGame}`, `emptyDetails`, `CopyKind`, `CopyStatus`, `Cover`, `PlatformBadge`, `Icon`, `Alert`, `GameDetail`.

- [ ] **Step 1: Texts.** In `scan` of both locales, remove the keys no longer used (`camera.paused`, `owned`, `openGame`, `next`, `foundIn`, `unknownShort`, `unknownTitle`, `unknown`, `fix`, `skip`, `addGame`, `addCopyTo`, `enterHint`, `needPlatform`, `searching`, `searchingHint`) — grep each in `web/src` first and keep any still referenced — and add `scan.list`:

en:
```json
"list": {
  "title": "Scanned ({{count}})",
  "empty": "Scan a box to start: each one joins this list, and nothing is added until you press Send.",
  "toastAdded": "added to the list",
  "toastRepeat": "already in the list",
  "repeat": "{{code}} is already in the list: use +1 on its row for another copy.",
  "looking": "Looking up…",
  "ready": "Ready",
  "newGame": "New game",
  "copyOf": "Copy of «{{title}}»",
  "owned": "You have it",
  "review": "Review",
  "error": "Error",
  "retry": "Retry",
  "plusOne": "+1",
  "plusOneTitle": "One more copy of this box",
  "count": "×{{count}}",
  "remove": "Remove",
  "removed": "Removed {{code}}.",
  "undo": "Undo",
  "clear": "Clear list",
  "cleared": "List cleared.",
  "summary": "{{ready}} ready · {{review}} to review · {{owned}} you have",
  "send_one": "Send {{count}}",
  "send_other": "Send {{count}}",
  "sendLooking": "{{count}} looking up…",
  "sending": "Sending…",
  "sendFailed": "Nothing was sent: {{error}}. The list is unchanged.",
  "added_one": "Added in this session ({{count}})",
  "added_other": "Added in this session ({{count}})",
  "noStorage": "This browser does not let Game Vault keep the list: it will be lost if the page closes.",
  "mute": "Mute",
  "unmute": "Sound",
  "detailTitle": "Game",
  "search": "Search",
  "addTo": "Add as",
  "cover": "Cover",
  "done": "Done",
  "foundIn": "found in {{provider}} as «{{raw}}»",
  "unknown": "Not in any barcode database: write the title and search. The box keeps its barcode, so next time it is recognized at once."
}
```

es:
```json
"list": {
  "title": "Escaneados ({{count}})",
  "empty": "Escanea una caja para empezar: cada una se suma a esta lista y no se añade nada hasta que pulses Enviar.",
  "toastAdded": "añadido a la lista",
  "toastRepeat": "ya está en la lista",
  "repeat": "{{code}} ya está en la lista: usa +1 en su fila para otra copia.",
  "looking": "Buscando…",
  "ready": "Listo",
  "newGame": "Juego nuevo",
  "copyOf": "Copia de «{{title}}»",
  "owned": "Ya lo tienes",
  "review": "Revisar",
  "error": "Error",
  "retry": "Reintentar",
  "plusOne": "+1",
  "plusOneTitle": "Otra copia de esta caja",
  "count": "×{{count}}",
  "remove": "Quitar",
  "removed": "Quitado {{code}}.",
  "undo": "Deshacer",
  "clear": "Vaciar lista",
  "cleared": "Lista vaciada.",
  "summary": "{{ready}} listos · {{review}} a revisar · {{owned}} ya los tienes",
  "send_one": "Enviar {{count}}",
  "send_other": "Enviar {{count}}",
  "sendLooking": "{{count}} buscando…",
  "sending": "Enviando…",
  "sendFailed": "No se ha enviado nada: {{error}}. La lista sigue igual.",
  "added_one": "Añadidos en esta sesión ({{count}})",
  "added_other": "Añadidos en esta sesión ({{count}})",
  "noStorage": "Este navegador no deja a Game Vault guardar la lista: se perderá si cierras la página.",
  "mute": "Silenciar",
  "unmute": "Sonido",
  "detailTitle": "Juego",
  "search": "Buscar",
  "addTo": "Añadir como",
  "cover": "Portada",
  "done": "Listo",
  "foundIn": "encontrado en {{provider}} como «{{raw}}»",
  "unknown": "No está en ninguna base de datos de códigos: escribe el título y busca. La caja guarda su código, así la próxima vez se reconoce al instante."
}
```

Also change `scan.intro` (both) to say the new flow: en "Add physical games by their barcode: point the camera at box after box, use a USB or Bluetooth reader, or type the code. Each box joins the list below; Send adds them all at once." es "Añade juegos físicos por su código de barras: apunta la cámara a una caja tras otra, usa un lector USB o Bluetooth, o escribe el código. Cada caja se suma a la lista de abajo; Enviar las añade todas de golpe." and `scan.placeholderCamera` stays.

Run: `task i18n` — Expected `translations OK`.

- [ ] **Step 2: Write `web/src/features/scan/ScanRow.tsx`** — one row and its detail:

```tsx
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, lookupClient, proxiedImage } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { PlatformBadge } from '../../components/PlatformBadge';
import { Alert } from '../../components/ui';
import { useAppData } from '../../state/AppData';
import { resolved, status, type Answer, type Choice, type Row } from './scanList';

/** Names of the barcode databases, for "found in …". */
const PROVIDER_NAMES: Record<string, string> = { cex: 'CeX', ebay: 'eBay', upcitemdb: 'UPCitemdb', eansearch: 'EAN-Search' };

export function ScanRow({ row, platform, open, onToggle, onPlus, onRemove, onRetry, onChoose }: {
  row: Row;
  /** The batch platform ('' when the code's is used). */
  platform: string;
  open: boolean;
  onToggle: () => void;
  onPlus: () => void;
  onRemove: () => void;
  onRetry: () => void;
  onChoose: (c: Choice) => void;
}) {
  const { t } = useTranslation();
  const { games } = useAppData();
  const st = status(row, platform);
  const c = resolved(row, platform);
  const target = c?.gameId ? games.find((g) => g.id === c.gameId) : undefined;
  const title = c?.title || row.answer?.match?.raw || row.code;
  const thumb = c?.thumbUrl || c?.coverUrl;
  const state = st === 'ready'
    ? (c?.gameId ? t('scan.list.copyOf', { title: target?.title ?? c.title }) : t('scan.list.newGame'))
    : t(`scan.list.${st}`);
  const canOpen = row.phase === 'done' && st !== 'owned';

  return (
    <li className={`scan-row is-${st} ${open ? 'open' : ''}`}>
      <div className="scan-row-line">
        <button type="button" className="scan-row-main" onClick={onToggle} disabled={!canOpen} aria-expanded={canOpen ? open : undefined}>
          <span className="scan-row-thumb">
            {target ? <Cover game={target} /> : thumb ? <img src={proxiedImage(thumb)} alt="" /> : <span className="thumb-placeholder" />}
          </span>
          <span className="scan-row-text">
            <span className="scan-row-title">{st === 'looking' ? <code>{row.code}</code> : title}</span>
            <span className="scan-row-meta">
              {c?.platform && <PlatformBadge platform={c.platform} />}
              <span className={`scan-row-state state-${st}`}>{st === 'looking' && <span className="spinner" aria-hidden="true" />}{state}</span>
              {row.count > 1 && <span className="scan-row-count">{t('scan.list.count', { count: row.count })}</span>}
            </span>
            {row.error && <span className="scan-row-error">{row.error}</span>}
          </span>
        </button>
        <div className="scan-row-actions">
          {st === 'error' && row.phase === 'error' && <button type="button" className="small-button" onClick={onRetry}>{t('scan.list.retry')}</button>}
          <button type="button" className="small-button" onClick={onPlus} title={t('scan.list.plusOneTitle')}>{t('scan.list.plusOne')}</button>
          <button type="button" className="icon-button" onClick={onRemove} aria-label={t('scan.list.remove')} title={t('scan.list.remove')}>
            <Icon name="close" size={16} />
          </button>
        </div>
      </div>
      {open && canOpen && row.answer && c && <RowDetail key={row.id} answer={row.answer} choice={c} batchPlatform={platform} onChoose={onChoose} onDone={onToggle} />}
    </li>
  );
}

/** The expanded row: what today's result card offered, editing the row's choice as you go. */
function RowDetail({ answer, choice, batchPlatform, onChoose, onDone }: {
  answer: Answer;
  choice: Choice;
  batchPlatform: string;
  onChoose: (c: Choice) => void;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const [suggestions, setSuggestions] = useState(answer.suggestions);
  const [existing, setExisting] = useState(answer.existing);
  const [warnings, setWarnings] = useState(answer.warnings);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const m = answer.match;
  const set = (patch: Partial<Choice>) => onChoose({ ...choice, ...patch });

  const search = async () => {
    if (!choice.title.trim()) return;
    setBusy(true);
    setError('');
    try {
      const res = await lookupClient.suggestGames({ title: choice.title, platform: choice.platform });
      const s = res.suggestions.map((x) => ({ title: x.title, platform: x.platform, coverUrl: x.coverUrl, thumbUrl: x.thumbUrl, label: x.label }));
      setSuggestions(s);
      setExisting(res.existing.map((g) => ({ id: g.id, title: g.title })));
      setWarnings(res.warnings);
      const first = s[0];
      set({
        title: first?.title ?? choice.title,
        platform: choice.platform || first?.platform || '',
        coverUrl: first?.coverUrl ?? '',
        thumbUrl: first?.thumbUrl ?? '',
        gameId: res.existing.length === 1 ? res.existing[0]!.id : '',
      });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="scan-row-detail">
      <p className="muted small scan-row-found">
        <code>{answer.barcode}</code>{' · '}
        {m ? t('scan.list.foundIn', { provider: PROVIDER_NAMES[m.providerId] ?? m.providerId, raw: m.raw }) : t('scan.list.unknown')}
      </p>
      <form className="scan-fix" onSubmit={(e) => { e.preventDefault(); search(); }}>
        <label className="scan-fix-title">
          {t('game.title')}
          <input value={choice.title} onChange={(e) => set({ title: e.target.value })} required />
        </label>
        <label>
          {t('copy.platform')}
          <input list="scan-platforms" value={choice.platform} disabled={!!batchPlatform} onChange={(e) => set({ platform: e.target.value })} />
        </label>
        <label>
          {t('copy.edition')}
          <input value={choice.edition} onChange={(e) => set({ edition: e.target.value })} />
        </label>
        <button type="submit" disabled={busy || !choice.title.trim()}>{busy ? t('common.working') : t('scan.list.search')}</button>
      </form>

      {existing.length > 0 && (
        <div className="segmented scan-target" role="radiogroup" aria-label={t('scan.list.addTo')}>
          {existing.map((g) => (
            <button key={g.id} type="button" role="radio" aria-checked={choice.gameId === g.id} className={choice.gameId === g.id ? 'active' : ''}
              onClick={() => set({ gameId: g.id })}>{t('scan.list.copyOf', { title: g.title })}</button>
          ))}
          <button type="button" role="radio" aria-checked={choice.gameId === ''} className={choice.gameId === '' ? 'active' : ''}
            onClick={() => set({ gameId: '' })}>{t('scan.list.newGame')}</button>
        </div>
      )}

      {!choice.gameId && suggestions.length > 0 && (
        <div className="scan-covers">
          <p className="muted small">{t('scan.list.cover')}</p>
          <div className="scan-cover-strip">
            {suggestions.map((s) => (
              <button key={s.coverUrl} type="button" className={`scan-thumb ${choice.coverUrl === s.coverUrl ? 'selected' : ''}`}
                onClick={() => set({ title: s.title, coverUrl: s.coverUrl, thumbUrl: s.thumbUrl, platform: choice.platform || s.platform })}
                title={s.label || s.title} aria-pressed={choice.coverUrl === s.coverUrl}>
                <img src={proxiedImage(s.thumbUrl || s.coverUrl)} alt={s.label || s.title} loading="lazy" />
              </button>
            ))}
          </div>
        </div>
      )}

      {warnings.map((w) => <Alert key={w} tone="warn">{w}</Alert>)}
      {error && <Alert tone="error">{error}</Alert>}
      <div className="scan-actions"><button type="button" className="primary" onClick={onDone}>{t('scan.list.done')}</button></div>
    </div>
  );
}
```

`Icon` has `close`, and `.icon-button` / `.small-button` exist in `styles.css` (checked while planning).

- [ ] **Step 3: Rewrite `ScanPage` in `web/src/features/scan/ScanPage.tsx`.** Keep the imports it still needs, `DEFAULTS_KEY`, `CAMERA_KEY`, `Defaults`, `NO_DEFAULTS`, `loadDefaults`, `initialCamera`, `BatchBar`; delete `reveal`, `ScanResult` and `PROVIDER_NAMES` (moved to `ScanRow.tsx`). Change `remember` to return whether it was saved, and add:

```tsx
const LIST_KEY = 'gamevault.scanList';
const MUTED_KEY = 'gamevault.scanMuted';
const SEND_CHUNK = 200;

function remember(key: string, value: string): boolean {
  try {
    localStorage.setItem(key, value);
    return true;
  } catch {
    return false; // private mode: not remembered
  }
}

function recall(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** A row id: unique on this device (crypto.randomUUID needs HTTPS, which a LAN address may lack). */
const newId = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

/** The plain parts of an IdentifyBarcode answer a row keeps. */
function toAnswer(res: IdentifyBarcodeResponse): Answer {
  return {
    barcode: res.barcode,
    owned: res.owned.map((o) => ({ gameId: o.game?.id ?? '', title: o.game?.title ?? '', platform: o.platform })),
    match: res.match ? { raw: res.match.raw, title: res.match.title, platform: res.match.platform, edition: res.match.edition, providerId: res.match.providerId } : null,
    suggestions: res.suggestions.map((s) => ({ title: s.title, platform: s.platform, coverUrl: s.coverUrl, thumbUrl: s.thumbUrl, label: s.label })),
    existing: res.existing.map((g) => ({ id: g.id, title: g.title })),
    warnings: res.warnings,
  };
}

interface Added {
  gameId: string;
  key: string;
}
```

Component:

```tsx
/**
 * Registering a shelf of physical games: the camera (or a reader, or typing) keeps reading, every
 * box joins a list kept on this device and looked up one at a time in the background, and Send
 * adds the ready ones at once. Nothing asks for a confirmation while scanning.
 */
export default function ScanPage() {
  const { t } = useTranslation();
  const { games, putGame } = useAppData();
  const [openGame, setOpenGame] = useState<string | null>(null);
  const [openRow, setOpenRow] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [camera, setCamera] = useState(initialCamera);
  const [muted, setMuted] = useState(() => recall(MUTED_KEY) === '1');
  const [defaults, setDefaults] = useState<Defaults>(loadDefaults);
  const [rows, setRows] = useState<Row[]>(() => loadRows(recall(LIST_KEY)));
  const rowsRef = useRef(rows);
  const [notice, setNotice] = useState<{ tone: 'warn' | 'error'; text: string } | null>(null);
  const [sending, setSending] = useState(false);
  const [added, setAdded] = useState<Added[]>([]);
  const [undo, setUndo] = useState<{ text: string; apply: () => void } | null>(null);
  const warnedStorage = useRef(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => remember(DEFAULTS_KEY, JSON.stringify(defaults)) && undefined, [defaults]);

  // Every change goes through here: the ref lets handlers read the list synchronously (a read
  // and a lookup answer can arrive in the same tick), and the list is saved on the device.
  const update = useCallback((fn: (r: Row[]) => Row[]) => {
    const next = fn(rowsRef.current);
    if (next === rowsRef.current) return;
    rowsRef.current = next;
    setRows(next);
    if (!remember(LIST_KEY, saveRows(next)) && !warnedStorage.current) {
      warnedStorage.current = true;
      setNotice({ tone: 'warn', text: t('scan.list.noStorage') });
    }
  }, [t]);

  const queue = useMemo(() => createLookupQueue(
    (c: string) => lookupClient.identifyBarcode({ barcode: c }).then(toAnswer),
    (id, answer, error) => update((r) => settle(r, id, answer, error === undefined ? undefined : errorMessage(error))),
  ), [update]);

  // Rows still looking up when the page was closed are looked up again.
  useEffect(() => {
    for (const r of rowsRef.current) if (r.phase === 'looking') queue.push(r.id, r.code);
  }, [queue]);

  // Undo offers last a few seconds.
  useEffect(() => {
    if (!undo) return;
    const timer = window.setTimeout(() => setUndo(null), 6000);
    return () => window.clearTimeout(timer);
  }, [undo]);

  /** A read from the camera, a reader or the keyboard. */
  const take = (raw: string): 'added' | 'repeat' | 'invalid' => {
    const res = addCode(rowsRef.current, raw, newId());
    if (res.outcome === 'invalid') return 'invalid';
    if (res.outcome === 'added') {
      update(() => res.rows);
      queue.push(res.row!.id, res.row!.code);
    }
    signal(res.outcome === 'added' ? 'new' : 'repeat', muted);
    return res.outcome;
  };

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    const raw = code.trim();
    if (!raw) return;
    const outcome = take(raw);
    if (outcome === 'invalid') {
      setNotice({ tone: 'error', text: t('scan.invalid', { code: raw }) });
      return;
    }
    setNotice(outcome === 'repeat' ? { tone: 'warn', text: t('scan.list.repeat', { code: normalizeBarcode(raw) }) } : null);
    setCode('');
    input.current?.focus();
  };

  const toggleCamera = () => {
    unlockAudio();
    setCamera(!camera);
    remember(CAMERA_KEY, camera ? '0' : '1');
  };

  const toggleMute = () => {
    unlockAudio();
    setMuted(!muted);
    remember(MUTED_KEY, muted ? '0' : '1');
  };

  const removeRow = (id: string) => {
    const { rows: next, removed } = remove(rowsRef.current, id);
    if (!removed) return;
    queue.drop(id);
    update(() => next);
    setUndo({ text: t('scan.list.removed', { code: removed.row.code }), apply: () => {
      update((r) => restore(r, removed));
      if (removed.row.phase === 'looking') queue.push(removed.row.id, removed.row.code);
    } });
  };

  const clearList = () => {
    const before = rowsRef.current;
    before.forEach((r) => queue.drop(r.id));
    update(() => []);
    setOpenRow(null);
    setUndo({ text: t('scan.list.cleared'), apply: () => {
      update(() => before);
      before.filter((r) => r.phase === 'looking').forEach((r) => queue.push(r.id, r.code));
    } });
  };

  const send = async () => {
    const items = sendItems(rowsRef.current, defaults.platform);
    if (!items.length) return;
    setSending(true);
    setNotice(null);
    try {
      for (let i = 0; i < items.length; i += SEND_CHUNK) {
        const res = await gameClient.addScannedCopies({
          items: items.slice(i, i + SEND_CHUNK).map((it) => ({
            clientId: it.clientId, gameId: it.gameId, title: it.title, coverUrl: it.coverUrl,
            details: {
              ...emptyDetails(CopyKind.PHYSICAL), status: CopyStatus.OWNED, platform: it.platform, edition: it.edition,
              grade: defaults.grade, contents: defaults.contents, location: defaults.location, barcode: it.barcode,
            },
          })),
        });
        res.games.forEach(putGame);
        const { rows: next, saved } = applyResults(rowsRef.current, res.results);
        update(() => next);
        setAdded((list) => [...saved.map((s) => ({ gameId: s.gameId, key: `${s.row.id}-${s.gameId}` })), ...list].slice(0, 60));
      }
      setOpenRow(null);
    } catch (e) {
      setNotice({ tone: 'error', text: t('scan.list.sendFailed', { error: errorMessage(e) }) });
    } finally {
      setSending(false);
    }
  };

  const sum = summary(rows, defaults.platform);

  return (
    <div className="page scan">
      <header className="page-head">
        <h1 className="page-title">{t('scan.title')}</h1>
      </header>
      <p className="muted scan-intro">{t('scan.intro')}</p>

      <section className="card scanner">
        {camera && (
          <Suspense fallback={<div className="camera camera-loading" />}>
            <CameraScanner onCode={take} />
          </Suspense>
        )}
        <form className="scanner-form" onSubmit={submitCode}>
          <input ref={input} className="barcode-input" inputMode="numeric" autoFocus={!camera} autoComplete="off"
            placeholder={t(camera ? 'scan.placeholderCamera' : 'scan.placeholder')} aria-label={t('scan.placeholder')}
            value={code} onChange={(e) => setCode(e.target.value)} />
          <button type="submit" className="primary" disabled={!code.trim()}>{t('scan.lookup')}</button>
          <button type="button" className={`camera-toggle ${camera ? 'active' : ''}`} onClick={toggleCamera}
            aria-pressed={camera} title={t(camera ? 'scan.camera.stop' : 'scan.camera.start')}>
            <Icon name="camera" size={18} />
            <span className="camera-toggle-label">{t(camera ? 'scan.camera.stop' : 'scan.camera.start')}</span>
          </button>
          <button type="button" className="camera-toggle" onClick={toggleMute} aria-pressed={!muted}
            title={t(muted ? 'scan.list.unmute' : 'scan.list.mute')}>
            <span className="camera-toggle-label">{t(muted ? 'scan.list.unmute' : 'scan.list.mute')}</span>
          </button>
        </form>
        <BatchBar defaults={defaults} onChange={setDefaults} />
      </section>

      {notice && <Alert tone={notice.tone}>{notice.text}</Alert>}

      <section className="scan-list-section">
        <h2 className="scan-list-title">{t('scan.list.title', { count: rows.length })}</h2>
        {rows.length === 0 ? <p className="muted">{t('scan.list.empty')}</p> : (
          <>
            <ul className="scan-list">
              {rows.map((r) => (
                <ScanRow key={r.id} row={r} platform={defaults.platform} open={openRow === r.id}
                  onToggle={() => setOpenRow(openRow === r.id ? null : r.id)}
                  onPlus={() => update((x) => plusOne(x, r.id))}
                  onRemove={() => removeRow(r.id)}
                  onRetry={() => { update((x) => retry(x, r.id)); queue.push(r.id, r.code); }}
                  onChoose={(c) => update((x) => choose(x, r.id, c))} />
              ))}
            </ul>
            <button type="button" className="link scan-clear" onClick={clearList}>{t('scan.list.clear')}</button>
          </>
        )}
      </section>

      {added.length > 0 && (
        <section className="scan-added">
          <h2 className="scan-added-title">{t('scan.list.added', { count: added.length })}</h2>
          <ul>
            {added.map((a) => {
              const g = games.find((x) => x.id === a.gameId);
              return (
                <li key={a.key}>
                  <button onClick={() => setOpenGame(a.gameId)} title={g?.title}>
                    {g ? <Cover game={g} /> : <span className="thumb-placeholder" />}
                    <span className="scan-added-name">{g?.title}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      {(rows.length > 0 || undo) && (
        <div className="scan-sendbar" role="region" aria-label={t('scan.list.title', { count: rows.length })}>
          {undo ? (
            <p className="scan-sendbar-undo">{undo.text} <button type="button" className="link" onClick={() => { undo.apply(); setUndo(null); }}>{t('scan.list.undo')}</button></p>
          ) : (
            <p className="scan-sendbar-summary">
              {t('scan.list.summary', { ready: sum.ready, review: sum.review, owned: sum.owned })}
              {sum.looking > 0 && <> · <span className="spinner" aria-hidden="true" /> {t('scan.list.sendLooking', { count: sum.looking })}</>}
            </p>
          )}
          <button type="button" className="primary" disabled={sending || sum.copies === 0} onClick={send}>
            {sending ? t('scan.list.sending') : t('scan.list.send', { count: sum.copies })}
          </button>
        </div>
      )}

      {openGame && <GameDetail key={openGame} gameId={openGame} onClose={() => setOpenGame(null)} onOpenGame={setOpenGame} />}
    </div>
  );
}
```

Imports at the top of `ScanPage.tsx`:

```tsx
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, gameClient, lookupClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { Icon } from '../../components/Icon';
import { Alert } from '../../components/ui';
import type { IdentifyBarcodeResponse } from '../../gen/gamevault/v1/lookup_pb';
import { CopyGrade } from '../../gen/gamevault/v1/game_pb';
import { CONTENTS, CopyKind, CopyStatus, GRADES, PHYSICAL_PLATFORMS, contentKey, emptyDetails, gradeKey } from '../../lib/model';
import { normalizeBarcode } from '../../lib/barcode';
import { useAppData } from '../../state/AppData';
import GameDetail from '../library/GameDetail';
import { signal, unlockAudio } from './feedback';
import { createLookupQueue } from './lookupQueue';
import { ScanRow } from './ScanRow';
import {
  addCode, applyResults, choose, loadRows, plusOne, remove, restore, retry, saveRows, sendItems, settle, summary, type Answer, type Row,
} from './scanList';
```

Note the defaults effect: `useEffect(() => { remember(DEFAULTS_KEY, JSON.stringify(defaults)); }, [defaults]);` (a block body: an effect must not return a boolean).

- [ ] **Step 4: CSS** — append to the scan section of `web/src/styles.css`:

```css
/* The scanned list: one compact row per box; a row opens in place to fix it. */
.scan-list-section { margin-top: 22px; padding-bottom: 84px; }
.scan-list-title { font-size: 15px; font-weight: 650; margin: 0 0 10px; }
.scan-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.scan-row { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); box-shadow: var(--shadow-sm); }
.scan-row.is-review { border-color: color-mix(in srgb, var(--accent-strong) 55%, var(--border)); }
.scan-row.is-error { border-color: color-mix(in srgb, var(--danger) 45%, var(--border)); }
.scan-row-line { display: flex; align-items: center; gap: 8px; padding: 8px 10px; }
.scan-row-main {
  flex: 1 1 auto; min-width: 0; display: flex; align-items: center; gap: 12px; padding: 0; border: none; background: none;
  min-height: 0; text-align: left; white-space: normal; font-weight: 500;
}
.scan-row-main:hover:not(:disabled) { background: none; }
.scan-row-main:disabled { opacity: 1; cursor: default; }
.scan-row-thumb { flex: none; width: 40px; }
.scan-row-thumb img, .scan-row-thumb .cover, .scan-row-thumb .thumb-placeholder { width: 40px; aspect-ratio: 2 / 3; border-radius: 4px; object-fit: cover; display: block; }
.scan-row-text { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.scan-row-title { font-weight: 650; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scan-row-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 13px; color: var(--muted); }
.scan-row-state { display: inline-flex; align-items: center; gap: 6px; }
.state-ready { color: var(--ok); }
.state-review { color: var(--warn); font-weight: 600; }
.state-error { color: var(--danger); }
.scan-row-count { font-weight: 650; color: var(--text); }
.scan-row-error { font-size: 13px; color: var(--danger); }
.scan-row-actions { display: flex; align-items: center; gap: 6px; flex: none; }
.scan-row-detail { display: flex; flex-direction: column; gap: 12px; padding: 4px 12px 14px; border-top: 1px solid var(--border); }
.scan-row-found { margin: 8px 0 0; overflow-wrap: anywhere; }
.scan-clear { margin-top: 12px; font-size: 14px; }

/* Send bar: always in reach of the thumb while there is a list. */
.scan-sendbar {
  position: fixed; left: 0; right: 0; bottom: 0; z-index: 20; display: flex; align-items: center; gap: 12px;
  padding: 10px max(16px, env(safe-area-inset-left)) calc(10px + env(safe-area-inset-bottom));
  background: color-mix(in srgb, var(--surface) 94%, transparent); backdrop-filter: blur(8px);
  border-top: 1px solid var(--border); box-shadow: 0 -6px 20px rgb(24 28 44 / .08);
}
.scan-sendbar p { margin: 0; flex: 1 1 auto; min-width: 0; font-size: 14px; color: var(--muted); }
.scan-sendbar .primary { flex: none; }
/* Desktop: beside the sidebar. Phones: above the bottom tabs (the shell's breakpoint is 860px). */
@media (min-width: 861px) { .scan-sendbar { left: var(--sidebar); } }
@media (max-width: 860px) { .scan-sendbar { bottom: calc(58px + env(safe-area-inset-bottom)); padding-bottom: 10px; } }
```

On the mobile preset (Task 7) check that the bar sits just above the bottom tabs; adjust the `58px` to the tabs' real height if it overlaps or leaves a gap.

- [ ] **Step 5: Type check and tests**

Run: `task test` — Expected green (Go, `tsc --noEmit`, Node tests, translations). Fix type errors against the generated names (`res.results`, `clientId`, `gameId`, `error`).

- [ ] **Step 6: Commit**

```bash
git add web/src/features/scan web/src/styles.css web/src/i18n/locales
git commit -m "Scan page: continuous scanning into a list, row review in place, send at once"
```

---

### Task 7: Docs and browser verification

**Files:**
- Modify: `docs/technical.md` (the barcode scanning section)
- Modify: `.claude/memory/open-threads.md` only if it lists batch scanning (remove it if so)

- [ ] **Step 1: Docs.** Find the scanning section (`grep -n -i "scan\b\|barcode" docs/technical.md | head -30`) and describe the new flow: the camera never pauses; every box joins a list kept in the browser (`localStorage`, `gamevault.scanList`, format 1); lookups one at a time; repeats ignored with "+1" for another copy; row states; batch defaults applied at Send; `GameService.AddScannedCopies` (one transaction, grouping by target game or `MatchKey` of the title for new games, per-item errors, at most 200 items).

- [ ] **Step 2: Lint and tests**

Run: `task lint` (Expected `0 issues.`) and `task test` (Expected green).

- [ ] **Step 3: Browser check on the test server**

Run: `task test-server`. Open `http://127.0.0.1:8093/?v=<new number>#/scan` in the browser pane (desktop):
- type a barcode the copy's catalog already has (find one with the browser: open a physical game's copy) → row "You have it", Send shows 0;
- type `5030934110075` twice → second time the repeat notice, one row;
- type a code no database knows (a valid EAN such as `4006381333931`) → Review; open it, write a title, search, pick a cover → Ready;
- press +1 on a Ready row → ×2;
- remove a row → Undo brings it back;
- reload the page → the list is still there;
- Send → rows leave the list, "Added in this session" shows the games, and the library has them (one game for two discs of the same new title).
Then the mobile preset: the send bar sits at the bottom without covering the last row; reset the viewport to desktop. Run `task test-server:stop`.

The real camera, tones and vibration are checked by the user on the phone.

- [ ] **Step 4: Commit**

```bash
git add docs/technical.md .claude/memory
git commit -m "Document continuous barcode scanning and AddScannedCopies"
```
