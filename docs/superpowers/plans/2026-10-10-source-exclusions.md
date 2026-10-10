# Source Exclusions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user remove an imported copy for good ("Remove and do not import again"). The source remembers the item and skips it on every sync, and the source's dialog can bring it back.

**Architecture:** A per-source exclusion list in the source domain, stored in the source's JSON document. The sync service turns excluded imports into withdrawn copies before consolidating (the existing withdraw path), and gains `ExcludeCopy` / `IncludeCopy` use cases. A sync re-reads the source before consolidating and before saving, so an exclusion made during a scan is never overwritten. The RPCs `GameService.ExcludeCopy` and `SourceService.IncludeCopy` are added. The web uses them from the copy's trash button and from the source dialog.

**Tech Stack:** Go 1.26, ConnectRPC/buf, SQLite (JSON documents); React 19 + TypeScript, i18next.

**Spec:** `docs/superpowers/specs/2026-10-10-source-exclusions-design.md`

## Global Constraints

- Everything builds and tests in the toolchain container through Task (`task lint`, `task test`, `task generate`).
- Load the `write-go` skill before writing Go: a doc comment on every exported identifier, one struct field per line, a blank line between documented members, GIVEN/WHEN/THEN tests with time-bounded contexts.
- English in the repository; every UI string through `t()` in `en.json` and `es.json`; `task i18n` passes.
- No schema migration: sources are JSON documents (`internal/adapters/outbound/sqlite/docs.go`).
- Manual copies keep today's delete. Only copies with a `SourceID` and an `ExternalID` can be excluded.
- Never sync PlayStation, Epic, GOG, Xbox, Ubisoft, Battle.net or EA on a config copy; browser checks use Humble or Steam.

## Review Focus

- An exclusion made while a sync is fetching (the store can take seconds) must survive that sync's save of the source.
- A store that changed its id format (`PreviousExternalID`) must not bring back an excluded item under its new id.
- Excluding the last copy of a game deletes the game. The page must close its detail and drop it, not show a broken game.
- A source deleted after exclusions takes them with it. Re-creating the store starts clean.
- `IncludeCopy` of an id not listed answers "not found", not a 500.

---

### Task 1: Exclusions in the source domain and its document

**Files:**
- Modify: `internal/domain/source/source.go`
- Create: `internal/domain/source/exclusion_test.go`
- Modify: `internal/adapters/outbound/sqlite/docs.go`, `internal/adapters/outbound/sqlite/docs_test.go`

**Interfaces:**
- Produces:
  - `type Exclusion struct { ExternalID string; Title string; At time.Time }`
  - `(*Source).Exclusions() []Exclusion`, `Exclude(e Exclusion) error`, `Include(externalID string) bool`, `Excludes(externalID string) bool`
  - `source.Rehydrate(..., lastSync *SyncReport, exclusions []Exclusion, createdAt, updatedAt time.Time)`: a new parameter after `lastSync`
  - `SyncReport.Excluded int`: the imported items a scan skipped because the user removed them

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing domain test** (`internal/domain/source/exclusion_test.go`)

```go
package source

import (
	"testing"
	"time"
)

func TestExclusions(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	src := Rehydrate("s1", "psn", "PlayStation", true, 0, Settings{}, nil, nil, now, now)

	if err := src.Exclude(Exclusion{Title: "Netflix", At: now}); err == nil {
		t.Fatal("an exclusion needs the item's external id")
	}

	netflix := Exclusion{ExternalID: "psn:NETFLIX", Title: "Netflix", At: now}
	spotify := Exclusion{ExternalID: "psn:SPOTIFY", Title: "Spotify", At: now.Add(time.Hour)}

	for _, e := range []Exclusion{netflix, spotify, {ExternalID: "psn:NETFLIX", Title: "Renamed", At: now.Add(2 * time.Hour)}} {
		if err := src.Exclude(e); err != nil {
			t.Fatal(err)
		}
	}

	got := src.Exclusions()
	if len(got) != 2 || got[0] != spotify || got[1] != netflix {
		t.Fatalf("newest first, the first entry kept for a repeat: %+v", got)
	}

	if !src.Excludes("psn:NETFLIX") || src.Excludes("psn:YOUTUBE") {
		t.Fatal("Excludes must answer for listed ids only")
	}

	got[0].Title = "changed"
	if src.Exclusions()[0].Title != "Spotify" {
		t.Fatal("Exclusions must return a copy")
	}

	if !src.Include("psn:NETFLIX") || src.Include("psn:NETFLIX") || src.Excludes("psn:NETFLIX") {
		t.Fatal("Include takes an id off the list once")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-gomod:/go/pkg/mod -v gamevault-gobuild:/root/.cache/go-build gamevault-toolchain:446777a3814f go test ./internal/domain/source/`
Expected: FAIL to compile: `too many arguments in call to Rehydrate`, `undefined: Exclusion`.

- [ ] **Step 4: Implement** in `internal/domain/source/source.go`

Add to `SyncReport`, after `GamesCreated`:

```go
	// Excluded counts the imported items skipped because the user removed them from the source.
	Excluded int
```

Add after the `Config` type:

```go
// Exclusion is an item the user removed from a source's imports: the source skips it on every
// scan until the user brings it back.
type Exclusion struct {
	// ExternalID is the copy's id at the source, e.g. "psn:EP4350-CUSA00127_00-NETFLIXPOLLUX001".
	ExternalID string

	// Title is the title the item had, to list it.
	Title string

	// At is when it was removed.
	At time.Time
}
```

Add `exclusions []Exclusion // newest first` to the `Source` struct, after `lastSync`. Add the
`exclusions []Exclusion` parameter to `Rehydrate` after `lastSync` and set it. Update the call in
`docs.go`, and the one in `docs_test.go`, with `nil`. Then add these methods after `RecordSync`:

```go
// Exclusions returns the items the user removed from this source, newest first.
func (s *Source) Exclusions() []Exclusion { return slices.Clone(s.exclusions) }

// Excludes reports whether the item with this external id was removed by the user.
func (s *Source) Excludes(externalID string) bool {
	return slices.ContainsFunc(s.exclusions, func(e Exclusion) bool { return e.ExternalID == externalID })
}

// Exclude records that the user removed an item. Removing it again keeps the first entry.
func (s *Source) Exclude(e Exclusion) error {
	if e.ExternalID == "" {
		return invalid("an item to remove needs its id at the source")
	}

	if s.Excludes(e.ExternalID) {
		return nil
	}

	s.exclusions = append(s.exclusions, e)
	slices.SortStableFunc(s.exclusions, func(a, b Exclusion) int { return b.At.Compare(a.At) })

	return nil
}

// Include takes an item off the removed list, so the next scan imports it again. It reports
// whether the item was listed.
func (s *Source) Include(externalID string) bool {
	n := len(s.exclusions)
	s.exclusions = slices.DeleteFunc(s.exclusions, func(e Exclusion) bool { return e.ExternalID == externalID })

	return len(s.exclusions) < n
}
```

Import `slices` in `source.go`.

- [ ] **Step 5: Run the domain test.** Expected: `ok gamevault/internal/domain/source`.

- [ ] **Step 6: Failing document round trip.** In `docs_test.go`, in `TestDocuments_sourceAndProvider`, the first GIVEN: give the report `Excluded: 2` and rehydrate with
`[]source.Exclusion{{ExternalID: "humble:a", Title: "A", At: docTime}}`. In the THEN, add:

```go
				assert.Equal(t, 2, got.LastSync().Excluded)
				require.Len(t, got.Exclusions(), 1)
				assert.Equal(t, "humble:a", got.Exclusions()[0].ExternalID)
				assert.Equal(t, "A", got.Exclusions()[0].Title)
				assert.True(t, docTime.Equal(got.Exclusions()[0].At))
```

Run: `... go test ./internal/adapters/outbound/sqlite/ -run TestDocuments_sourceAndProvider`
Expected: FAIL to compile (`syncReportDoc` no longer converts from `source.SyncReport`).

- [ ] **Step 7: Store them** in `docs.go`:

```go
// exclusionDoc is the stored form of a source.Exclusion.
type exclusionDoc struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title,omitempty"`
	At         string `json:"at"`
}
```

- Add `Exclusions []exclusionDoc \`json:"exclusions,omitempty"\`` to `sourceDoc`, after `LastSync`.
- Add `Excluded int \`json:"excluded,omitempty"\`` to `syncReportDoc` after `GamesCreated`, in the same position as in `SyncReport`, so the conversion compiles.
- In `encodeSource`, add after the report:

```go
	for _, e := range s.Exclusions() {
		doc.Exclusions = append(doc.Exclusions, exclusionDoc{
			ExternalID: e.ExternalID,
			Title:      e.Title,
			At:         formatTime(e.At),
		})
	}
```

- In `decodeSource`, add before the return:

```go
	exclusions := make([]source.Exclusion, 0, len(doc.Exclusions))
	for _, e := range doc.Exclusions {
		exclusions = append(exclusions, source.Exclusion{
			ExternalID: e.ExternalID,
			Title:      e.Title,
			At:         parseTime(e.At),
		})
	}
```

and pass `exclusions` to `Rehydrate`.

- [ ] **Step 8: Run both tests, lint, commit.**

Run: `... go test ./internal/domain/source/ ./internal/adapters/outbound/sqlite/` and `task lint`.
Expected: `ok` for both packages, and `0 issues.` (run `task lint:fix` first if it reports layout).

```bash
git add internal/domain/source internal/adapters/outbound/sqlite/docs.go internal/adapters/outbound/sqlite/docs_test.go
git commit -m "Sources: remember the items the user removed, in the source's document"
```

---

### Task 2: Sync skips excluded items; ExcludeCopy and IncludeCopy

**Files:**
- Modify: `internal/application/sync/service.go`
- Create: `internal/application/sync/exclusions.go`, `internal/application/sync/exclusions_test.go`

**Interfaces:**
- Consumes: Task 1 (`Exclude`, `Include`, `Excludes`, `Exclusions`, `SyncReport.Excluded`).
- Produces:
  - `var ErrNotImported, ErrNotExcluded error`
  - `(*Service).ExcludeCopy(ctx, gameID, copyID game.ID) (*game.Game, error)`: nil game when it was deleted
  - `(*Service).IncludeCopy(ctx, id source.ID, externalID string) error`

- [ ] **Step 1: Write the failing test** (`internal/application/sync/exclusions_test.go`)

```go
package sync_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// listing is a library source with a fixed list of items; during runs inside Fetch, as if the
// user acted while the store was answering.
type listing struct {
	items  []game.ImportedCopy
	during func()
}

func (l *listing) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "ls",
		Name: "Listing",
	}
}

func (l *listing) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	if l.during != nil {
		l.during()
	}

	return l.items, nil, nil
}

func (l *listing) Test(context.Context, source.Settings) error { return nil }

func item(id, title string) game.ImportedCopy {
	return game.ImportedCopy{
		ExternalID: id,
		Title:      title,
		Details: game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		},
	}
}

func TestExclusions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*sync.Service, *listing, game.Repository, source.ID) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		games := sqlite.NewGameRepository(db)
		p := &listing{items: []game.ImportedCopy{item("ls:netflix", "Netflix"), item("ls:journey", "Journey")}}
		svc := sync.NewService(sqlite.NewSourceRepository(db), games, db, nil, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

		v, err := svc.Create(ctx, "ls", source.Config{Enabled: true})
		require.NoError(t, err)
		_, err = svc.Sync(ctx, v.ID())
		require.NoError(t, err)

		return svc, p, games, v.ID()
	}

	copyOf := func(t *testing.T, games game.Repository, title string) (game.ID, game.ID) {
		t.Helper()

		list, err := games.List(ctx)
		require.NoError(t, err)

		for _, g := range list {
			if g.Title() == title {
				return g.ID(), g.Copies()[0].ID
			}
		}

		t.Fatalf("no game %q", title)

		return "", ""
	}

	titles := func(t *testing.T, games game.Repository) []string {
		t.Helper()

		list, err := games.List(ctx)
		require.NoError(t, err)

		out := make([]string, 0, len(list))
		for _, g := range list {
			out = append(out, g.Title())
		}

		return out
	}

	t.Run("GIVEN a synced source WHEN the user removes an item", func(t *testing.T) {
		svc, _, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")

		g, err := svc.ExcludeCopy(ctx, gameID, copyID)
		require.NoError(t, err)

		t.Run("THEN its game, left empty, is deleted and the source lists the item", func(t *testing.T) {
			assert.Nil(t, g)
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))

			v, err := svc.Get(ctx, id)
			require.NoError(t, err)
			require.Len(t, v.Exclusions(), 1)
			assert.Equal(t, "ls:netflix", v.Exclusions()[0].ExternalID)
			assert.Equal(t, "Netflix", v.Exclusions()[0].Title)
		})

		t.Run("AND the next sync skips it and counts it", func(t *testing.T) {
			v, err := svc.Sync(ctx, id)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))
			assert.Equal(t, 1, v.LastSync().Excluded)
		})

		t.Run("AND bringing it back imports it on the next sync", func(t *testing.T) {
			require.NoError(t, svc.IncludeCopy(ctx, id, "ls:netflix"))
			_, err := svc.Sync(ctx, id)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"Journey", "Netflix"}, titles(t, games))
			assert.ErrorIs(t, svc.IncludeCopy(ctx, id, "ls:netflix"), sync.ErrNotExcluded)
		})
	})

	t.Run("GIVEN a removed item WHEN the store reports it under a new id format", func(t *testing.T) {
		svc, p, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")
		_, err := svc.ExcludeCopy(ctx, gameID, copyID)
		require.NoError(t, err)

		renamed := item("ls:v2:netflix", "Netflix")
		renamed.PreviousExternalID = "ls:netflix"
		p.items = []game.ImportedCopy{renamed, item("ls:journey", "Journey")}

		_, err = svc.Sync(ctx, id)
		require.NoError(t, err)

		t.Run("THEN it stays out", func(t *testing.T) {
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))
		})
	})

	t.Run("GIVEN the user removes an item while a sync is fetching", func(t *testing.T) {
		svc, p, games, id := setup(t)
		gameID, copyID := copyOf(t, games, "Netflix")
		p.during = func() {
			_, err := svc.ExcludeCopy(ctx, gameID, copyID)
			require.NoError(t, err)

			p.during = nil
		}

		_, err := svc.Sync(ctx, id)
		require.NoError(t, err)

		t.Run("THEN the sync keeps the exclusion and does not bring the item back", func(t *testing.T) {
			v, err := svc.Get(ctx, id)
			require.NoError(t, err)
			assert.Len(t, v.Exclusions(), 1)
			assert.ElementsMatch(t, []string{"Journey"}, titles(t, games))
		})
	})

	t.Run("GIVEN a copy added by hand", func(t *testing.T) {
		svc, _, games, _ := setup(t)
		gameID, _ := copyOf(t, games, "Journey")
		g, err := games.Get(ctx, gameID)
		require.NoError(t, err)
		manual, err := g.AddCopy(game.CopyDetails{Kind: game.KindPhysical}, time.Now())
		require.NoError(t, err)
		require.NoError(t, games.Save(ctx, g))

		_, err = svc.ExcludeCopy(ctx, gameID, manual.ID)

		t.Run("THEN it cannot be excluded", func(t *testing.T) {
			assert.ErrorIs(t, err, sync.ErrNotImported)
		})
	})
}
```

The service has no `Get` yet (checked while planning): Step 3 adds one. `invalidateCovers`
already accepts a nil cover cache, so the test passes `nil`.

- [ ] **Step 2: Run it to verify it fails**

Run: `... go test ./internal/application/sync/ -run TestExclusions`
Expected: FAIL to compile: `svc.ExcludeCopy undefined`, `undefined: sync.ErrNotExcluded`.

- [ ] **Step 3: Write `internal/application/sync/exclusions.go`**

```go
package sync

import (
	"context"
	"errors"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

var (
	// ErrNotImported means the copy was added by hand: there is nothing to stop importing.
	ErrNotImported = errors.New("this copy was added by hand: delete it instead")

	// ErrNotExcluded means the item is not on the source's removed list.
	ErrNotExcluded = errors.New("this item is not on the source's removed list")
)

// ExcludeCopy removes a copy a source imported and records it on the source, so later scans skip
// it. A game left without copies is deleted; the result is then nil.
func (s *Service) ExcludeCopy(ctx context.Context, gameID, copyID game.ID) (*game.Game, error) {
	var (
		out   *game.Game
		stale bool
	)

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		g, err := s.games.Get(ctx, gameID)
		if err != nil {
			return err
		}

		i := slices.IndexFunc(g.Copies(), func(c game.Copy) bool { return c.ID == copyID })
		if i < 0 {
			return game.ErrCopyNotFound
		}

		c := g.Copies()[i]
		if c.SourceID == "" || c.ExternalID == "" {
			return ErrNotImported
		}

		src, err := s.sources.Get(ctx, source.ID(c.SourceID))
		if err != nil {
			return err
		}

		now := s.now()
		if err := src.Exclude(source.Exclusion{
			ExternalID: c.ExternalID,
			Title:      g.Title(),
			At:         now,
		}); err != nil {
			return err
		}

		if err := s.sources.Save(ctx, src); err != nil {
			return err
		}

		cover := g.CoverPhoto()
		if _, err := g.RemoveCopy(copyID, now); err != nil {
			return err
		}

		if len(g.Copies()) == 0 {
			stale = true
			return s.games.Delete(ctx, gameID)
		}

		stale, out = g.CoverPhoto() != cover, g

		return s.games.Save(ctx, g)
	})
	if err != nil {
		return nil, err
	}

	if stale {
		s.invalidateCovers(ctx, []game.ID{gameID})
	}

	return out, nil
}

// IncludeCopy takes an item off a source's removed list: the next scan imports it again.
func (s *Service) IncludeCopy(ctx context.Context, id source.ID, externalID string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		src, err := s.sources.Get(ctx, id)
		if err != nil {
			return err
		}

		if !src.Include(externalID) {
			return ErrNotExcluded
		}

		return s.sources.Save(ctx, src)
	})
}

// skipExcluded turns the imported items the user removed from src into withdrawn copies: the
// consolidation then removes an old copy of them and never adds a new one. It returns how many
// items it skipped.
func skipExcluded(src *source.Source, copies []game.ImportedCopy) ([]game.ImportedCopy, int) {
	out := make([]game.ImportedCopy, len(copies))
	skipped := 0

	for i, c := range copies {
		out[i] = c

		if c.Withdrawn || !(src.Excludes(c.ExternalID) || c.PreviousExternalID != "" && src.Excludes(c.PreviousExternalID)) {
			continue
		}

		out[i].Withdrawn = true
		skipped++
	}

	return out, skipped
}
```

Add `"slices"` to the imports, and this method next to `List`:

```go
// Get returns one source with the number of copies it manages.
func (s *Service) Get(ctx context.Context, id source.ID) (SourceView, error) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return SourceView{}, err
	}

	return s.view(ctx, src)
}
```

- [ ] **Step 4: Use it in `Sync`** (`internal/application/sync/service.go`). Inside the transaction, before `game.NewConsolidator(games).Apply(...)`, read the source as it is now. The user may have removed items since the scan started:

```go
			fresh, err := s.sources.Get(ctx, src.ID())
			if err != nil {
				return err
			}

			var excluded int

			copies, excluded = skipExcluded(fresh, copies)
			report.Excluded = excluded
```

Replace the final `src.RecordSync(report)` and `s.sources.Save(ctx, src)` with a save on the source as it is now:

```go
	report.FinishedAt = s.now()

	// Saved on the source as it is now: the user may have removed items or changed its settings
	// while the store was answering. Only the scan's own results (its report and any session the
	// store rotated) are written over it.
	saveErr := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		latest, err := s.sources.Get(ctx, src.ID())
		if err != nil {
			return err
		}

		if d, err := s.Descriptor(latest.Type()); err == nil {
			latest.UpdateState(d, settings, s.now())
		}

		latest.RecordSync(report)
		src = latest

		return s.sources.Save(ctx, latest)
	})
	if saveErr != nil {
		return SourceView{}, errors.Join(syncErr, saveErr)
	}
```

Remove the earlier `src.UpdateState(d, settings, s.now()) // saved below with the report` call: the state is now applied in the final save. Keep `settings` (the map `Fetch` may have changed).

- [ ] **Step 5: Run the test and the package**

Run: `... go test ./internal/application/sync/`
Expected: `ok gamevault/internal/application/sync`. `TestCoverPhotoLeavesWithTheSourcesCopy` and the keep-alive tests still pass.

- [ ] **Step 6: Lint and commit**

Run: `task lint` (`task lint:fix` first if it reports layout). Expected `0 issues.`

```bash
git add internal/application/sync
git commit -m "Sync: skip the items the user removed; ExcludeCopy and IncludeCopy; never overwrite the source after a scan"
```

---

### Task 3: API

**Files:**
- Modify: `proto/gamevault/v1/game.proto`, `proto/gamevault/v1/source.proto`
- Modify: `internal/adapters/inbound/rpc/game_handler.go`, `source_handler.go`, `mapper.go`
- Generated: `internal/gen`, `web/src/gen` (`task generate`)
- Test: `internal/adapters/inbound/rpc/exclusions_test.go`

**Interfaces:**
- Consumes: Task 2 `ExcludeCopy`, `IncludeCopy`, `ErrNotImported`, `ErrNotExcluded`; Task 1 `Exclusions()`, `SyncReport.Excluded`.
- Produces (TS after generation):
  - `gameClient.excludeCopy({ gameId, copyId }) → { game?: Game }`
  - `sourceClient.includeCopy({ sourceId, externalId }) → {}`
  - `Source.exclusions: { externalId, title, at }[]`
  - `SyncReport.excluded: number`

- [ ] **Step 1: Proto.** In `source.proto`:

```proto
// Exclusion is an item the user removed from a source: scans skip it until it is brought back.
message Exclusion {
  string external_id = 1;
  string title = 2;
  google.protobuf.Timestamp at = 3;
}
```

- `Source` gains `repeated Exclusion exclusions = 11;` (newest first).
- `SyncReport` gains `// Imported items skipped because the user removed them.` and `int32 excluded = 11;`.
- Add the messages `IncludeCopyRequest { string source_id = 1; string external_id = 2; }` and `IncludeCopyResponse {}`.
- Add `// Takes an item off the source's removed list: the next scan imports it again.` and `rpc IncludeCopy(IncludeCopyRequest) returns (IncludeCopyResponse);` to `SourceService`.

In `game.proto`, add `message ExcludeCopyRequest { string game_id = 1; string copy_id = 2; }` and:

```proto
message ExcludeCopyResponse {
  // Absent when the game was left without copies and deleted.
  Game game = 1;
}
```

Then add `// Removes a copy a source imported and keeps the source from importing it again. A copy added by hand is refused (delete it instead).` and `rpc ExcludeCopy(ExcludeCopyRequest) returns (ExcludeCopyResponse);` after `DeleteCopy`.

Field 11 is free in both `Source` and `SyncReport` (checked while planning).

Run: `task generate`. Expected: generated code updated; the build fails on the missing handler methods.

- [ ] **Step 2: Write the failing end-to-end test** (`internal/adapters/inbound/rpc/exclusions_test.go`)

```go
package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
)

func TestExclusions_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a synced source with one item", func(t *testing.T) {
		c := newServer(t, &fakeProvider{copies: []game.ImportedCopy{{
			ExternalID: "fake:netflix",
			Title:      "Netflix",
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "PS4",
			},
		}}})

		src, err := c.sources.CreateSource(ctx, connect.NewRequest(&pb.CreateSourceRequest{Source: &pb.SourceInput{
			Type:     "fake",
			Enabled:  true,
			Settings: map[string]string{"token": "t"},
		}}))
		require.NoError(t, err)

		_, err = c.sources.SyncSource(ctx, connect.NewRequest(&pb.SyncSourceRequest{Id: src.Msg.Source.Id}))
		require.NoError(t, err)

		list, err := c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
		require.NoError(t, err)
		require.Len(t, list.Msg.Games, 1)
		g := list.Msg.Games[0]

		t.Run("WHEN its copy is excluded", func(t *testing.T) {
			res, err := c.games.ExcludeCopy(ctx, connect.NewRequest(&pb.ExcludeCopyRequest{
				GameId: g.Id,
				CopyId: g.Copies[0].Id,
			}))
			require.NoError(t, err)

			t.Run("THEN the emptied game is gone and the source lists the item", func(t *testing.T) {
				assert.Nil(t, res.Msg.Game)

				sources, err := c.sources.ListSources(ctx, connect.NewRequest(&pb.ListSourcesRequest{}))
				require.NoError(t, err)
				require.Len(t, sources.Msg.Sources[0].Exclusions, 1)
				assert.Equal(t, "fake:netflix", sources.Msg.Sources[0].Exclusions[0].ExternalId)
				assert.Equal(t, "Netflix", sources.Msg.Sources[0].Exclusions[0].Title)
			})

			t.Run("AND a sync reports it skipped", func(t *testing.T) {
				synced, err := c.sources.SyncSource(ctx, connect.NewRequest(&pb.SyncSourceRequest{Id: src.Msg.Source.Id}))
				require.NoError(t, err)
				assert.Equal(t, int32(1), synced.Msg.Source.LastSync.Excluded)
			})

			t.Run("AND including it twice answers not found the second time", func(t *testing.T) {
				req := &pb.IncludeCopyRequest{
					SourceId:   src.Msg.Source.Id,
					ExternalId: "fake:netflix",
				}
				_, err := c.sources.IncludeCopy(ctx, connect.NewRequest(req))
				require.NoError(t, err)

				_, err = c.sources.IncludeCopy(ctx, connect.NewRequest(req))
				assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			})
		})
	})

	t.Run("GIVEN a game added by hand", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:  "Halo 3",
			Copies: []*pb.CopyDetails{{Kind: pb.CopyKind_COPY_KIND_PHYSICAL}},
		}))
		require.NoError(t, err)

		_, err = c.games.ExcludeCopy(ctx, connect.NewRequest(&pb.ExcludeCopyRequest{
			GameId: created.Msg.Game.Id,
			CopyId: created.Msg.Game.Copies[0].Id,
		}))

		t.Run("THEN excluding its copy is refused as a precondition", func(t *testing.T) {
			assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		})
	})
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `... go test ./internal/adapters/inbound/rpc/ -run TestExclusions`
Expected: FAIL to compile (missing `ExcludeCopy` / `IncludeCopy` methods).

- [ ] **Step 4: Handlers and mapping**

`game_handler.go`, after `DeleteCopy`:

```go
// ExcludeCopy removes a copy a source imported and keeps the source from importing it again.
func (h *GameHandler) ExcludeCopy(ctx context.Context, req *connect.Request[pb.ExcludeCopyRequest]) (*connect.Response[pb.ExcludeCopyResponse], error) {
	g, err := h.sources.ExcludeCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.ExcludeCopyResponse{Game: gameToPB(g)}), nil
}
```

`source_handler.go`, after `DeleteSource`:

```go
// IncludeCopy takes an item off a source's removed list.
func (h *SourceHandler) IncludeCopy(ctx context.Context, req *connect.Request[pb.IncludeCopyRequest]) (*connect.Response[pb.IncludeCopyResponse], error) {
	if err := h.sync.IncludeCopy(ctx, source.ID(req.Msg.SourceId), req.Msg.ExternalId); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.IncludeCopyResponse{}), nil
}
```

`mapper.go`:

- `reportToPB` gains `Excluded: int32(r.Excluded),`.
- `sourceToPB` gains `Exclusions: exclusionsToPB(v.Exclusions()),`.
- Add:

```go
func exclusionsToPB(list []source.Exclusion) []*pb.Exclusion {
	out := make([]*pb.Exclusion, 0, len(list))
	for _, e := range list {
		out = append(out, &pb.Exclusion{
			ExternalId: e.ExternalID,
			Title:      e.Title,
			At:         ts(e.At),
		})
	}

	return out
}
```

- In `toConnectError`, add `errors.Is(err, sync.ErrNotExcluded)` to the `NotFound` case and `errors.Is(err, sync.ErrNotImported)` to the `FailedPrecondition` case.

- [ ] **Step 5: Run the test, lint, full suite, commit**

Run: `... go test ./internal/adapters/inbound/rpc/`, then `task lint`, then `task test`.
Expected: `ok`, then `0 issues.`, then green (the TS type check passes; nothing uses the new fields yet).

```bash
git add proto internal/gen web/src/gen internal/adapters/inbound/rpc
git commit -m "API: ExcludeCopy, IncludeCopy, a source's removed items and the skipped count"
```

---

### Task 4: Web, docs and browser check

**Files:**
- Modify: `web/src/features/library/GameDetail.tsx`
- Modify: `web/src/features/sources/SourceDialog.tsx`, `web/src/features/sources/SourcesPage.tsx`
- Modify: `web/src/i18n/locales/en.json`, `es.json`, `web/src/styles.css`
- Modify: `docs/technical.md`, `.claude/memory/open-threads.md`, `.claude/MEMORY.md`

**Interfaces:**
- Consumes: Task 3 TS client and fields; `useAppData().{putGame, dropGame, reloadSources, sourceName}`.

- [ ] **Step 1: Texts** (both locales, keeping key parity):
  - en `copy.confirmExclude`: "Remove «{{title}}» from {{source}} and do not import it again? The next sync will skip it; you can bring it back from the source's settings."
  - en `copy.confirmExcludeLoses`: " Its photos and price estimates will be deleted."
  - en `sources.excludedTitle`: "Removed from this source ({{count}})"
  - en `sources.importAgain`: "Import again"
  - en `sources.excludedHint`: "Scans skip these items. Import one again to bring it back on the next sync."
  - en `sources.excludedCount_one` / `_other`: "{{count}} removed by you"
  - es: "¿Quitar «{{title}}» de {{source}} y no volver a importarlo? La próxima sincronización lo saltará; puedes recuperarlo desde los ajustes de la fuente.", " Se borrarán sus fotos y sus precios estimados.", "Quitados de esta fuente ({{count}})", "Volver a importar", "Las sincronizaciones saltan estos elementos. Vuelve a importar uno para recuperarlo en la próxima sincronización.", "{{count}} quitado por ti" / "{{count}} quitados por ti".

  Use `excludedTitle` without plural forms (the count is in parentheses). Run `task i18n`: expected `translations OK`.

- [ ] **Step 2: The copy's trash button** (`GameDetail.tsx`). `CopiesTab` gets an `onGameGone: () => void` prop. `GameDetailBody` passes `() => { dropGame(game.id); onClose(); }`. Replace `deleteCopy`:

```tsx
  const deleteCopy = (c: Copy) => {
    if (!c.sourceId || !c.externalId) {
      if (!confirm(t('copy.confirmDelete'))) return;
      run(async () => putGame((await gameClient.deleteCopy({ gameId: game.id, copyId: c.id })).game!));
      return;
    }
    // An imported copy would come back on the next sync: remove it for good instead.
    const loses = c.photos.length > 0 || c.estimates.length > 0;
    const question = t('copy.confirmExclude', { title: game.title, source: sourceName(c.sourceId) })
      + (loses ? t('copy.confirmExcludeLoses') : '');
    if (!confirm(question)) return;
    run(async () => {
      const res = await gameClient.excludeCopy({ gameId: game.id, copyId: c.id });
      await reloadSources();
      if (res.game) putGame(res.game);
      else onGameGone();
    });
  };
```

Take `reloadSources` from `useAppData()` in `CopiesTab`. The generated `Copy` already exposes `externalId`.

- [ ] **Step 3: The source dialog** (`SourceDialog.tsx`). After the last-scan section, render the list when the source has exclusions:

```tsx
        {source && source.exclusions.filter((e) => !restored.includes(e.externalId)).length > 0 && (
          <section className="source-excluded">
            <h3>{t('sources.excludedTitle', { count: source.exclusions.filter((e) => !restored.includes(e.externalId)).length })}</h3>
            <p className="muted small">{t('sources.excludedHint')}</p>
            <ul>
              {source.exclusions.filter((e) => !restored.includes(e.externalId)).map((e) => (
                <li key={e.externalId}>
                  <span className="source-excluded-title">{e.title || e.externalId}</span>
                  <span className="muted small">{fmt.date(toDate(e.at))}</span>
                  <button type="button" className="small-button" disabled={locked} onClick={() => includeAgain(e.externalId)}>{t('sources.importAgain')}</button>
                </li>
              ))}
            </ul>
          </section>
        )}
```

with

```tsx
  // Items brought back in this dialog leave the list at once; the source reloads behind.
  const [restored, setRestored] = useState<string[]>([]);
  const { reloadSources } = useAppData();
  const includeAgain = async (externalId: string) => {
    try {
      await sourceClient.includeCopy({ sourceId: source!.id, externalId });
      setRestored((r) => [...r, externalId]);
      void reloadSources();
    } catch (e) {
      setResult({ tone: 'error', text: errorMessage(e) });
    }
  };
```

`fmt.date` takes a `Date`, so `fmt.date(toDate(e.at))` is right. Hoist the filtered list into a `const visible = …` to avoid repeating the filter.

CSS: `.source-excluded ul { list-style: none; margin: 8px 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }`, `.source-excluded li { display: flex; align-items: center; gap: 10px; }`, `.source-excluded-title { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }`.

- [ ] **Step 4: The sources list line** (`SourcesPage.tsx`): add `r.excluded > 0 && t('sources.excludedCount', { count: r.excluded })` to the `status` parts, after `changes`.

- [ ] **Step 5: Type check, tests, lint**

Run: `task test` and `task lint`. Expected: green, and `0 issues.`

- [ ] **Step 6: Docs**
  - `docs/technical.md`, in Sources: a paragraph "Removing imported items". It covers the trash button on an imported copy, the source's removed list (stored in its document), how scans skip it (withdrawn, `PreviousExternalID` included, counted in the report), "Import again", and that manual copies are deleted as before.
  - `.claude/memory/open-threads.md`: remove the PlayStation Store covers line. Add the reason to the line about Nintendo / new sources, or to `.claude/memory/playstation.md`: "PS Store covers dropped (2026-10-10): 35/36 real PS games already have covers, and playstation.com's terms (section 10) forbid automated access to the site".
  - `.claude/MEMORY.md`: update the open-threads line accordingly.

- [ ] **Step 7: Browser check.** `task test-server`, then http://127.0.0.1:8093/?v=<new number> in the browser pane, on desktop:
  - open a Humble game's copy and press the trash button: the new question names Humble;
  - confirm: the game (or copy) is gone;
  - Sources → Humble: "Removed from this source (1)" lists it;
  - Scan now: the list line says "1 removed by you" and the item does not come back;
  - Import again, then Scan now: it is back.

  Then a manual copy's trash button still asks the old question. Then the mobile preset: the dialog's list does not widen the page. Reset the viewport, then `task test-server:stop`.

- [ ] **Step 8: Commit**

```bash
git add web docs .claude
git commit -m "Remove imported items for good from a copy, bring them back from the source"
```
