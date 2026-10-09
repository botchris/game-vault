# JSON documents in SQLite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store games (with their copies), sources and providers as JSON documents in SQLite, so adding a field never needs a schema migration, without changing the domain, the services, the API or the UI.

**Architecture:** Each aggregate becomes one row `(id, doc)`; the sqlite adapter maps the domain to its own document structs (`docs.go`) and back. Migration `0009` rebuilds the documents from the current columns in pure SQL, altering the tables in place, after a `VACUUM INTO` backup of the database. Repositories keep their interfaces; only their SQL and mapping change.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (pure Go, JSON1 functions), testify, Task + the toolchain container.

**Spec:** `docs/superpowers/specs/2026-10-09-json-documents-design.md`

## Global Constraints

- Everything runs in the toolchain container through Task; the host only has Docker and Task. Never run Go, golangci-lint or Node on the host.
- Load the `write-go` skill before writing Go. House rules checked by `task lint`: one struct field per line in struct literals and types (`tools/fieldlines`), a blank line between documented members and a doc comment on every interface method (`tools/docspacing`), golangci-lint (revive, godot, wsl_v5…).
- Tests: GIVEN / WHEN / THEN subtests with testify; never pass `context.Background()` to code under test, use `context.WithTimeout(t.Context(), 10*time.Second)`.
- Never edit an existing migration; `0009_documents.sql` is new.
- Never open or modify `config/`; real-data checks use the copies `task test-server` makes.
- Document field names are lowerCamelCase and never change once released; every document carries `"v": 1`; empty values are omitted on write.
- Everything in the repository is English.
- Branch `feature/json-documents`; one PR to `main`; no tags.

## Review Focus

- A disabled source or provider (`enabled` is the integer `0` in the old columns) must come back disabled: the migration must write JSON booleans, not `0`/`1` (test in Task 3).
- A source that was never scanned (`last_sync` is NULL) and a game with no copies must migrate and read back (no report; an empty copy list) (test in Task 3).
- Copy order and manual copies (NULL `source_id`) must survive the migration: same order as before, empty source id (test in Task 3).
- A document written by a newer Game Vault (`"v": 2`) must be refused with an error that says to update, not read with fields silently dropped (test in Task 1).
- Restoring a pre-migration backup and starting again must not be blocked because `pre-migration-0009.db` already exists: the second backup gets a timestamped name (test in Task 2).

---

## File structure

| File | Responsibility |
| --- | --- |
| `Taskfile.yml` (modify) | New `task go -- …` to run one go command (a single package's tests) in the toolchain |
| `internal/adapters/outbound/sqlite/docs.go` (create) | Document structs and the domain ↔ document mapping, the format version |
| `internal/adapters/outbound/sqlite/docs_test.go` (create) | Mapping round trips, omitted empty values, unknown fields, newer versions |
| `internal/adapters/outbound/sqlite/db.go` (modify) | `Open(ctx, path, backupDir)`, migrations up to a version (tests), pre-migration backup |
| `internal/adapters/outbound/sqlite/migrate_test.go` (create) | Backup before migrating; migration 0009 from a 0008 database |
| `internal/adapters/outbound/sqlite/migrations/0009_documents.sql` (create) | Columns → documents |
| `internal/adapters/outbound/sqlite/game_repository.go`, `source_repository.go`, `provider_repository.go` (modify) | Read and write documents |
| `internal/adapters/outbound/sqlite/repository_test.go` (modify) | Repository round trips through the database |
| `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`, `internal/application/sync/keepalive_test.go` (modify) | New `Open` signature |
| `docs/technical.md`, `.claude/memory/data-model.md` (modify) | Persistence docs |

---

### Task 1: Document structs and mapping

**Files:**
- Modify: `Taskfile.yml` (add the `go` task after `test`)
- Create: `internal/adapters/outbound/sqlite/docs.go`
- Create: `internal/adapters/outbound/sqlite/docs_test.go`
- Modify: `internal/adapters/outbound/sqlite/source_repository.go` (remove `syncReportJSON`, now `syncReportDoc` in `docs.go`; use it in `Save` and `scanSource`)

**Interfaces:**
- Consumes: `game.Rehydrate(id, info, copies, createdAt, updatedAt)`, `source.Rehydrate(id, typ, name, enabled, interval, settings, lastSync, createdAt, updatedAt)`, `provider.Rehydrate(id, kind, enabled, priority, settings, updatedAt)`, `formatTime`, `parseTime` (db.go).
- Produces: `encodeGame(*game.Game) (string, error)`, `decodeGame(game.ID, string) (*game.Game, error)`, `encodeSource(*source.Source) (string, error)`, `decodeSource(source.ID, string) (*source.Source, error)`, `encodeProvider(*provider.Provider) (string, error)`, `decodeProvider(provider.ID, string) (*provider.Provider, error)`, `errNewerDocument`, `syncReportDoc`.

- [ ] **Step 1: Add `task go`**

In `Taskfile.yml`, after the `test` task:

```yaml
  go:
    desc: 'Run one go command in the toolchain, e.g. task go -- test ./internal/adapters/outbound/sqlite/ -run TestDocuments -v'
    deps: [toolchain]
    cmds:
      - '{{.IN_TOOLCHAIN}} go {{.CLI_ARGS}}'
```

- [ ] **Step 2: Write the failing tests** in `internal/adapters/outbound/sqlite/docs_test.go`

```go
package sqlite

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

var docTime = time.Date(2026, 10, 9, 18, 30, 0, 123456789, time.UTC)

func sampleGame(t *testing.T) *game.Game {
	t.Helper()

	return game.Rehydrate("g1", game.Info{
		Title:    "Hades",
		Links:    game.Links{"steam": "1145360"},
		Notes:    "GOTY",
		CoverURL: "https://example.test/hades.jpg",
	}, []game.Copy{
		{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindKey,
				Platform: "Steam",
				Status:   game.StatusRevealed,
				Key:      "AAAA-BBBB",
				RedeemBy: "2027-01-01",
				Origin:   "Humble – Indie Bundle",
			},
			SourceID:   "src1",
			ExternalID: "humble:A:hades:0",
			CreatedAt:  docTime,
			UpdatedAt:  docTime,
		},
		{
			ID: "c2",
			CopyDetails: game.CopyDetails{
				Kind:       game.KindPhysical,
				Platform:   "PS4",
				Status:     game.StatusOwned,
				AcquiredOn: "2021-07-11",
				Edition:    "Collector's",
				Condition:  "Complete",
				Location:   "Shelf",
				Barcode:    "5026555255042",
				Notes:      "signed",
			},
			CreatedAt: docTime,
			UpdatedAt: docTime,
		},
	}, docTime, docTime)
}

func TestDocuments_game(t *testing.T) {
	t.Run("GIVEN a game with a key and a physical copy", func(t *testing.T) {
		g := sampleGame(t)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the aggregate is the same", func(t *testing.T) {
				assert.Equal(t, g.Info(), got.Info())
				assert.Equal(t, g.Copies(), got.Copies())
				assert.True(t, g.CreatedAt().Equal(got.CreatedAt()))
				assert.True(t, g.UpdatedAt().Equal(got.UpdatedAt()))
			})

			t.Run("AND the document has a version and no empty fields", func(t *testing.T) {
				var doc map[string]any
				require.NoError(t, json.Unmarshal([]byte(raw), &doc))
				assert.Equal(t, float64(1), doc["v"])

				physical := doc["copies"].([]any)[1].(map[string]any)
				assert.NotContains(t, physical, "key")
				assert.NotContains(t, physical, "sourceId")
			})
		})
	})

	t.Run("GIVEN a document from an older version without some fields, and with unknown ones", func(t *testing.T) {
		raw := `{"v":1,"title":"Celeste","createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z",
			"futureField":42,"copies":[{"id":"c1","kind":"library","platform":"Steam","status":"owned","extra":"x"}]}`

		t.Run("WHEN it is decoded", func(t *testing.T) {
			got, err := decodeGame("g2", raw)
			require.NoError(t, err)

			t.Run("THEN missing fields are empty and unknown ones are ignored", func(t *testing.T) {
				assert.Equal(t, "Celeste", got.Title())
				assert.Empty(t, got.Links())
				require.Len(t, got.Copies(), 1)
				assert.Equal(t, game.KindLibrary, got.Copies()[0].Kind)
				assert.Empty(t, got.Copies()[0].Location)
			})
		})
	})

	t.Run("GIVEN a document written by a newer Game Vault", func(t *testing.T) {
		t.Run("THEN it is refused, saying to update", func(t *testing.T) {
			_, err := decodeGame("g3", `{"v":2,"title":"x"}`)
			assert.ErrorIs(t, err, errNewerDocument)
		})
	})
}

func TestDocuments_sourceAndProvider(t *testing.T) {
	t.Run("GIVEN a disabled source with settings and a sync report", func(t *testing.T) {
		report := &source.SyncReport{
			StartedAt:   docTime,
			FinishedAt:  docTime.Add(time.Minute),
			Fetched:     3,
			CopiesAdded: 2,
			Warnings:    []string{"one skipped"},
		}
		s := source.Rehydrate("src1", "humble", "Humble Bundle", false, 24*time.Hour,
			source.Settings{"session": "secret"}, report, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeSource(s)
			require.NoError(t, err)

			got, err := decodeSource(s.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN every field comes back", func(t *testing.T) {
				assert.Equal(t, s.Type(), got.Type())
				assert.Equal(t, s.Name(), got.Name())
				assert.False(t, got.Enabled())
				assert.Equal(t, s.SyncInterval(), got.SyncInterval())
				assert.Equal(t, s.Settings(), got.Settings())
				require.NotNil(t, got.LastSync())
				assert.Equal(t, report.Warnings, got.LastSync().Warnings)
				assert.True(t, report.FinishedAt.Equal(got.LastSync().FinishedAt))
			})
		})
	})

	t.Run("GIVEN a source never scanned, stored with an explicit null report", func(t *testing.T) {
		got, err := decodeSource("src2", `{"v":1,"type":"steam","name":"Steam","enabled":true,"lastSync":null}`)

		t.Run("THEN it has no report and empty settings, not nil ones", func(t *testing.T) {
			require.NoError(t, err)
			assert.Nil(t, got.LastSync())
			assert.NotNil(t, got.Settings())
		})
	})

	t.Run("GIVEN a disabled provider with settings", func(t *testing.T) {
		p := provider.Rehydrate("thegamesdb", provider.KindCover, false, 3, schema.Settings{"api_key": "k"}, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeProvider(p)
			require.NoError(t, err)

			got, err := decodeProvider(p.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN every field comes back", func(t *testing.T) {
				assert.Equal(t, provider.KindCover, got.Kind())
				assert.False(t, got.Enabled())
				assert.Equal(t, 3, got.Priority())
				assert.Equal(t, p.Settings(), got.Settings())
				assert.True(t, docTime.Equal(got.UpdatedAt()))
			})
		})
	})
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestDocuments -v`
Expected: build failure, `undefined: encodeGame` (and the other functions).

- [ ] **Step 4: Write `docs.go`**

```go
package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// docVersion is the format version written into every document. Adding a field keeps it; changing
// the meaning of one bumps it, and the decoders convert older versions when they read them (see
// docs/superpowers/specs/2026-10-09-json-documents-design.md).
const docVersion = 1

// errNewerDocument means a document was written by a newer Game Vault, whose fields this version
// would silently drop if it read and saved it again.
var errNewerDocument = errors.New("written by a newer version of Game Vault: update Game Vault to open this database")

func checkVersion(v int) error {
	if v > docVersion {
		return fmt.Errorf("document version %d: %w", v, errNewerDocument)
	}

	return nil
}

// gameDoc is the stored form of a game.Game, copies included. Field names never change once released.
type gameDoc struct {
	V         int        `json:"v"`
	Title     string     `json:"title"`
	Links     game.Links `json:"links,omitempty"`
	Notes     string     `json:"notes,omitempty"`
	CoverURL  string     `json:"coverUrl,omitempty"`
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`
	Copies    []copyDoc  `json:"copies,omitempty"`
}

// copyDoc is the stored form of a game.Copy.
type copyDoc struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Platform   string `json:"platform,omitempty"`
	Status     string `json:"status"`
	Key        string `json:"key,omitempty"`
	RedeemBy   string `json:"redeemBy,omitempty"`
	Origin     string `json:"origin,omitempty"`
	AcquiredOn string `json:"acquiredOn,omitempty"`
	Edition    string `json:"edition,omitempty"`
	Condition  string `json:"condition,omitempty"`
	Location   string `json:"location,omitempty"`
	Barcode    string `json:"barcode,omitempty"`
	Notes      string `json:"notes,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// sourceDoc is the stored form of a source.Source.
type sourceDoc struct {
	V                   int             `json:"v"`
	Type                string          `json:"type"`
	Name                string          `json:"name"`
	Enabled             bool            `json:"enabled"`
	SyncIntervalSeconds int64           `json:"syncIntervalSeconds,omitempty"`
	Settings            source.Settings `json:"settings,omitempty"`
	LastSync            *syncReportDoc  `json:"lastSync,omitempty"`
	CreatedAt           string          `json:"createdAt"`
	UpdatedAt           string          `json:"updatedAt"`
}

// syncReportDoc is the stored form of source.SyncReport (the same fields, so the types convert).
type syncReportDoc struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt"`
	Err             string    `json:"error,omitempty"`
	Fetched         int       `json:"fetched"`
	CopiesAdded     int       `json:"copiesAdded"`
	CopiesUpdated   int       `json:"copiesUpdated"`
	CopiesUnchanged int       `json:"copiesUnchanged"`
	GamesCreated    int       `json:"gamesCreated"`
	Warnings        []string  `json:"warnings,omitempty"`
}

// providerDoc is the stored form of a provider.Provider.
type providerDoc struct {
	V         int             `json:"v"`
	Kind      string          `json:"kind"`
	Enabled   bool            `json:"enabled"`
	Priority  int             `json:"priority"`
	Settings  schema.Settings `json:"settings,omitempty"`
	UpdatedAt string          `json:"updatedAt"`
}

func encode(doc any) (string, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func encodeGame(g *game.Game) (string, error) {
	info := g.Info()
	doc := gameDoc{
		V:         docVersion,
		Title:     info.Title,
		Links:     info.Links,
		Notes:     info.Notes,
		CoverURL:  info.CoverURL,
		CreatedAt: formatTime(g.CreatedAt()),
		UpdatedAt: formatTime(g.UpdatedAt()),
	}

	for _, c := range g.Copies() {
		doc.Copies = append(doc.Copies, copyDoc{
			ID:         string(c.ID),
			Kind:       string(c.Kind),
			Platform:   c.Platform,
			Status:     string(c.Status),
			Key:        c.Key,
			RedeemBy:   string(c.RedeemBy),
			Origin:     c.Origin,
			AcquiredOn: string(c.AcquiredOn),
			Edition:    c.Edition,
			Condition:  c.Condition,
			Location:   c.Location,
			Barcode:    string(c.Barcode),
			Notes:      c.Notes,
			SourceID:   c.SourceID,
			ExternalID: c.ExternalID,
			CreatedAt:  formatTime(c.CreatedAt),
			UpdatedAt:  formatTime(c.UpdatedAt),
		})
	}

	return encode(doc)
}

func decodeGame(id game.ID, raw string) (*game.Game, error) {
	var doc gameDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V); err != nil {
		return nil, err
	}

	copies := make([]game.Copy, 0, len(doc.Copies))
	for _, c := range doc.Copies {
		copies = append(copies, game.Copy{
			ID: game.ID(c.ID),
			CopyDetails: game.CopyDetails{
				Kind:       game.Kind(c.Kind),
				Platform:   c.Platform,
				Status:     game.Status(c.Status),
				Key:        c.Key,
				RedeemBy:   game.Date(c.RedeemBy),
				Origin:     c.Origin,
				AcquiredOn: game.Date(c.AcquiredOn),
				Edition:    c.Edition,
				Condition:  c.Condition,
				Location:   c.Location,
				Barcode:    game.Barcode(c.Barcode),
				Notes:      c.Notes,
			},
			SourceID:   c.SourceID,
			ExternalID: c.ExternalID,
			CreatedAt:  parseTime(c.CreatedAt),
			UpdatedAt:  parseTime(c.UpdatedAt),
		})
	}

	info := game.Info{
		Title:    doc.Title,
		Links:    doc.Links,
		Notes:    doc.Notes,
		CoverURL: doc.CoverURL,
	}

	return game.Rehydrate(id, info, copies, parseTime(doc.CreatedAt), parseTime(doc.UpdatedAt)), nil
}

func encodeSource(s *source.Source) (string, error) {
	doc := sourceDoc{
		V:                   docVersion,
		Type:                string(s.Type()),
		Name:                s.Name(),
		Enabled:             s.Enabled(),
		SyncIntervalSeconds: int64(s.SyncInterval() / time.Second),
		Settings:            s.Settings(),
		CreatedAt:           formatTime(s.CreatedAt()),
		UpdatedAt:           formatTime(s.UpdatedAt()),
	}

	if rep := s.LastSync(); rep != nil {
		r := syncReportDoc(*rep)
		doc.LastSync = &r
	}

	return encode(doc)
}

func decodeSource(id source.ID, raw string) (*source.Source, error) {
	var doc sourceDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V); err != nil {
		return nil, err
	}

	if doc.Settings == nil {
		doc.Settings = source.Settings{}
	}

	var lastSync *source.SyncReport

	if doc.LastSync != nil {
		r := source.SyncReport(*doc.LastSync)
		lastSync = &r
	}

	return source.Rehydrate(id, source.Type(doc.Type), doc.Name, doc.Enabled,
		time.Duration(doc.SyncIntervalSeconds)*time.Second, doc.Settings, lastSync,
		parseTime(doc.CreatedAt), parseTime(doc.UpdatedAt)), nil
}

func encodeProvider(p *provider.Provider) (string, error) {
	return encode(providerDoc{
		V:         docVersion,
		Kind:      string(p.Kind()),
		Enabled:   p.Enabled(),
		Priority:  p.Priority(),
		Settings:  p.Settings(),
		UpdatedAt: formatTime(p.UpdatedAt()),
	})
}

func decodeProvider(id provider.ID, raw string) (*provider.Provider, error) {
	var doc providerDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V); err != nil {
		return nil, err
	}

	if doc.Settings == nil {
		doc.Settings = schema.Settings{}
	}

	return provider.Rehydrate(id, provider.Kind(doc.Kind), doc.Enabled, doc.Priority, doc.Settings, parseTime(doc.UpdatedAt)), nil
}
```

In `source_repository.go`, delete the `syncReportJSON` type and replace its two uses with `syncReportDoc` (the fields are identical; Task 3 rewrites this file anyway).

If `source.Settings` and `schema.Settings` are the same type alias, keep both spellings as written; if the compiler reports a mismatch, use `schema.Settings` in `sourceDoc` and convert with `source.Settings(doc.Settings)`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestDocuments -v`
Expected: PASS for `TestDocuments_game` and `TestDocuments_sourceAndProvider`.

- [ ] **Step 6: Lint and commit**

Run: `task lint:fix && task lint` — expected `0 issues.`

```bash
git add Taskfile.yml internal/adapters/outbound/sqlite/docs.go internal/adapters/outbound/sqlite/docs_test.go internal/adapters/outbound/sqlite/source_repository.go
git commit -m "Add the JSON document format of games, sources and providers"
```

---

### Task 2: Pre-migration backup and migrating up to a version

**Files:**
- Modify: `internal/adapters/outbound/sqlite/db.go` (`Open`, `migrate`)
- Create: `internal/adapters/outbound/sqlite/migrate_test.go`
- Modify: `internal/adapters/outbound/sqlite/repository_test.go` (`openTest` passes `""`), `cmd/gamevault/main.go`, `internal/adapters/inbound/rpc/server_test.go`, `internal/application/sync/keepalive_test.go` (new `Open` signature)

**Interfaces:**
- Consumes: `(*DB).BackupTo(ctx, path) error` (refuses an existing path).
- Produces: `Open(ctx context.Context, path, backupDir string) (*DB, error)` (`backupDir == ""` means no pre-migration backup); `openUpTo(ctx context.Context, path, backupDir, last string) (*DB, error)` applies migrations whose version sorts `<= last` (used by tests; `last == ""` means all); `migrationNumber(version string) string` ("0009_documents" → "0009").

- [ ] **Step 1: Write the failing tests** in `internal/adapters/outbound/sqlite/migrate_test.go`

```go
package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_backupBeforeMigrating(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a new database", func(t *testing.T) {
		dir := t.TempDir()
		backups := filepath.Join(dir, "backups")

		t.Run("WHEN it is opened", func(t *testing.T) {
			db, err := Open(ctx, filepath.Join(dir, "new.db"), backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN nothing is backed up: there is nothing to lose", func(t *testing.T) {
				files, _ := filepath.Glob(filepath.Join(backups, "*.db"))
				assert.Empty(t, files)
			})
		})
	})

	t.Run("GIVEN a database migrated up to 0007", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0007")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN it is opened with the pending migrations", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN a copy named after the first pending migration was written first", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0008.db"))
			})
		})

		t.Run("WHEN that copy is restored and opened again", func(t *testing.T) {
			restored := filepath.Join(dir, "restored.db")
			data, err := os.ReadFile(filepath.Join(backups, "pre-migration-0008.db"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(restored, data, 0o600))

			db, err := Open(ctx, restored, backups)

			t.Run("THEN it migrates, keeping the first copy and writing a timestamped one", func(t *testing.T) {
				require.NoError(t, err)
				db.Close()

				files, _ := filepath.Glob(filepath.Join(backups, "pre-migration-0008*.db"))
				assert.Len(t, files, 2)
			})
		})
	})
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestOpen_backupBeforeMigrating -v`
Expected: build failure (`too many arguments in call to Open`, `undefined: openUpTo`).

- [ ] **Step 3: Implement in `db.go`**

Replace `Open` and `migrate`:

```go
// Open opens (creating if needed) the database at path and applies pending migrations. When an
// existing database has pending migrations and backupDir is set, a copy is written there first
// (pre-migration-<first pending>.db), so an upgrade that goes wrong can be undone.
func Open(ctx context.Context, path, backupDir string) (*DB, error) {
	return openUpTo(ctx, path, backupDir, "")
}

// openUpTo is Open applying only the migrations up to last (all when last is empty), so tests can
// build a database as an older version left it.
func openUpTo(ctx context.Context, path, backupDir, last string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serializes writes; plenty for a single-user app and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	d := &DB{sql: db}
	if err := d.migrate(ctx, backupDir, last); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return d, nil
}

// migrationNumber is the number part of a migration version: "0009_documents" → "0009".
func migrationNumber(version string) string {
	n, _, _ := strings.Cut(version, "_")
	return n
}

func (d *DB) migrate(ctx context.Context, backupDir, last string) error {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}

	sort.Strings(files)

	var applied int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		return err
	}

	backedUp := false

	for _, f := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(f, "migrations/"), ".sql")
		if last != "" && migrationNumber(version) > last {
			break
		}

		var n int
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
			return err
		}

		if n > 0 {
			continue
		}

		// An existing database is copied once, before its first pending migration.
		if applied > 0 && backupDir != "" && !backedUp {
			if err := d.backupBeforeMigrating(ctx, backupDir, migrationNumber(version)); err != nil {
				return err
			}

			backedUp = true
		}

		body, err := migrations.ReadFile(f)
		if err != nil {
			return err
		}

		err = d.WithinTx(ctx, func(ctx context.Context) error {
			if _, err := d.conn(ctx).ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", version, err)
			}

			_, err := d.conn(ctx).ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, formatTime(time.Now()))

			return err
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// backupBeforeMigrating writes pre-migration-<number>.db into dir, or a timestamped name when that
// file exists already (a pre-migration copy that was restored and is being migrated again).
func (d *DB) backupBeforeMigrating(ctx context.Context, dir, number string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s for the pre-migration backup: %w", dir, err)
	}

	path := filepath.Join(dir, "pre-migration-"+number+".db")
	if _, err := os.Stat(path); err == nil {
		path = filepath.Join(dir, fmt.Sprintf("pre-migration-%s-%s.db", number, time.Now().UTC().Format("20060102-150405")))
	}

	if err := d.BackupTo(ctx, path); err != nil {
		return fmt.Errorf("backing up the database to %s before migrating it: %w", path, err)
	}

	return nil
}
```

Add `"path/filepath"` to the imports of `db.go`.

Update the callers:

- `cmd/gamevault/main.go`: `db, err := sqlite.Open(ctx, cfg.DatabasePath(), cfg.BackupDir())`
- `internal/adapters/inbound/rpc/server_test.go`: `sqlite.Open(ctx, filepath.Join(dir, "gamevault.db"), "")`
- `internal/application/sync/keepalive_test.go` (two calls): `sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")`
- `internal/adapters/outbound/sqlite/repository_test.go` `openTest`: `Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), "")`

- [ ] **Step 4: Run the tests to see them pass**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestOpen_backupBeforeMigrating -v`
Expected: PASS (the 0007 database has 0008 pending, so the copy is `pre-migration-0008.db`; once Task 3 adds 0009 the first pending one is still 0008).

- [ ] **Step 5: Run the whole suite**

Run: `task test`
Expected: everything green.

- [ ] **Step 6: Lint and commit**

Run: `task lint:fix && task lint` — expected `0 issues.`

```bash
git add internal/adapters/outbound/sqlite/db.go internal/adapters/outbound/sqlite/migrate_test.go internal/adapters/outbound/sqlite/repository_test.go cmd/gamevault/main.go internal/adapters/inbound/rpc/server_test.go internal/application/sync/keepalive_test.go
git commit -m "Back up the database before migrating it, and migrate up to a version in tests"
```

---

### Task 3: Migration 0009 and document repositories

**Files:**
- Create: `internal/adapters/outbound/sqlite/migrations/0009_documents.sql`
- Modify: `internal/adapters/outbound/sqlite/game_repository.go`, `source_repository.go`, `provider_repository.go`
- Modify: `internal/adapters/outbound/sqlite/migrate_test.go` (add `TestMigration0009`)
- Modify: `internal/adapters/outbound/sqlite/repository_test.go` (add a provider round trip; the existing tests keep passing)

**Interfaces:**
- Consumes: `encodeGame`/`decodeGame`, `encodeSource`/`decodeSource`, `encodeProvider`/`decodeProvider` (Task 1); `openUpTo` (Task 2).
- Produces: tables `games(id, doc)`, `sources(id, doc)`, `providers(id, doc)`; same repository methods as today.

- [ ] **Step 1: Write the failing migration test** (append to `migrate_test.go`)

```go
func TestMigration0009(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database as version 0008 left it, with rows of every shape", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "old.db")

		old, err := openUpTo(ctx, path, "", "0008")
		require.NoError(t, err)

		_, err = old.sql.ExecContext(ctx, `
			INSERT INTO sources (id, type, name, enabled, sync_interval_seconds, settings, last_sync, created_at, updated_at) VALUES
			  ('s1', 'humble', 'Humble Bundle', 0, 86400, '{"session":"secret"}',
			   '{"startedAt":"2026-10-08T10:00:00Z","finishedAt":"2026-10-08T10:01:00Z","fetched":2,"copiesAdded":1,"copiesUpdated":0,"copiesUnchanged":1,"gamesCreated":1,"warnings":["w"]}',
			   '2026-10-01T10:00:00Z', '2026-10-08T10:01:00Z'),
			  ('s2', 'steam', 'Steam', 1, 0, '{}', NULL, '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
			INSERT INTO games (id, title, notes, cover_url, links, created_at, updated_at) VALUES
			  ('g1', 'Hades', 'GOTY', '', '{"steam":"1145360"}', '2026-10-01T10:00:00Z', '2026-10-02T10:00:00Z'),
			  ('g2', 'Empty', '', '', '{}', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
			INSERT INTO copies (id, game_id, kind, platform, status, cd_key, redeem_by, origin, acquired_on, edition,
			                    condition, location, barcode, notes, source_id, external_id, created_at, updated_at) VALUES
			  ('c2', 'g1', 'physical', 'PS4', 'owned', '', '', 'GAME', '2021-07-11', 'Collector''s', 'Complete', 'Shelf',
			   '5026555255042', 'signed', NULL, '', '2026-10-02T10:00:00Z', '2026-10-02T10:00:00Z'),
			  ('c1', 'g1', 'key', 'Steam', 'revealed', 'AAAA', '2027-01-01', 'Humble', '', '', '', '', '', '', 's1',
			   'humble:A:hades:0', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
			INSERT INTO providers (id, kind, enabled, priority, settings, updated_at) VALUES
			  ('thegamesdb', 'cover', 0, 0, '{"api_key":"k"}', '2026-10-01T10:00:00Z'),
			  ('steam', 'cover', 1, 1, '{}', '2026-10-01T10:00:00Z');
			INSERT INTO game_details (game_id, language, data, fetched_at) VALUES ('g1', 'en', '{}', '2026-10-01T10:00:00Z');`)
		require.NoError(t, err)
		old.Close()

		t.Run("WHEN it is opened by this version", func(t *testing.T) {
			db, err := Open(ctx, path, "")
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })

			games, err := NewGameRepository(db).List(ctx)
			require.NoError(t, err)

			t.Run("THEN games keep their fields and their copies, in their order", func(t *testing.T) {
				require.Len(t, games, 2)
				assert.Equal(t, "Empty", games[0].Title())
				assert.Empty(t, games[0].Copies())

				hades := games[1]
				assert.Equal(t, game.Links{"steam": "1145360"}, hades.Links())
				assert.Equal(t, "GOTY", hades.Notes())
				require.Len(t, hades.Copies(), 2)
				assert.Equal(t, game.ID("c1"), hades.Copies()[0].ID, "copies come in creation order")
				assert.Equal(t, "s1", hades.Copies()[0].SourceID)
				assert.Equal(t, "AAAA", hades.Copies()[0].Key)
				assert.Empty(t, hades.Copies()[1].SourceID, "a manual copy has no source")
				assert.Equal(t, game.Barcode("5026555255042"), hades.Copies()[1].Barcode)
				assert.Equal(t, "Shelf", hades.Copies()[1].Location)
			})

			t.Run("AND sources keep their state, disabled ones disabled", func(t *testing.T) {
				srcs, err := NewSourceRepository(db).List(ctx)
				require.NoError(t, err)
				require.Len(t, srcs, 2)
				assert.Equal(t, "Humble Bundle", srcs[0].Name())
				assert.False(t, srcs[0].Enabled())
				assert.Equal(t, 24*time.Hour, srcs[0].SyncInterval())
				assert.Equal(t, "secret", srcs[0].Settings()["session"])
				require.NotNil(t, srcs[0].LastSync())
				assert.Equal(t, []string{"w"}, srcs[0].LastSync().Warnings)
				assert.True(t, srcs[1].Enabled())
				assert.Nil(t, srcs[1].LastSync(), "a source never scanned has no report")
			})

			t.Run("AND providers keep their order and state", func(t *testing.T) {
				ps, err := NewProviderRepository(db).List(ctx, provider.KindCover)
				require.NoError(t, err)
				require.Len(t, ps, 2)
				assert.Equal(t, provider.ID("thegamesdb"), ps[0].ID())
				assert.False(t, ps[0].Enabled())
				assert.Equal(t, "k", ps[0].Settings()["api_key"])
				assert.True(t, ps[1].Enabled())
			})

			t.Run("AND the cached details survive", func(t *testing.T) {
				var n int
				require.NoError(t, db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_details WHERE game_id = 'g1'`).Scan(&n))
				assert.Equal(t, 1, n)
			})
		})
	})
}
```

Add `"gamevault/internal/domain/game"` and `"gamevault/internal/domain/provider"` to the imports of `migrate_test.go`.

- [ ] **Step 2: Run it to see it fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestMigration0009 -v`
Expected: PASS already, on the old repositories (the 0008 schema is still the current one): it pins the behavior that Steps 3–4 must keep. Add these lines to the games' THEN block so it fails first, proving the documents exist:

```go
				var raw string
				require.NoError(t, db.sql.QueryRowContext(ctx, `SELECT doc FROM games WHERE id = 'g1'`).Scan(&raw))
				assert.Contains(t, raw, `"v":1`)
```

Re-run: FAIL with `no such column: doc`.

- [ ] **Step 3: Write `migrations/0009_documents.sql`**

```sql
-- Games (with their copies), sources and providers become JSON documents, so adding a field never
-- needs a schema migration (docs/superpowers/specs/2026-10-09-json-documents-design.md). Tables are
-- altered in place: game_details references games(id) with ON DELETE CASCADE, so dropping games
-- would empty the details cache. Integer flags become JSON booleans with json('true'/'false'),
-- because json_object would write 0/1, which a Go bool cannot decode.

ALTER TABLE games ADD COLUMN doc TEXT NOT NULL DEFAULT '{}';

UPDATE games SET doc = json_object(
  'v', 1,
  'title', title,
  'links', json(links),
  'notes', notes,
  'coverUrl', cover_url,
  'createdAt', created_at,
  'updatedAt', updated_at,
  'copies', json((
    SELECT json_group_array(json_object(
      'id', c.id,
      'kind', c.kind,
      'platform', c.platform,
      'status', c.status,
      'key', c.cd_key,
      'redeemBy', c.redeem_by,
      'origin', c.origin,
      'acquiredOn', c.acquired_on,
      'edition', c.edition,
      'condition', c.condition,
      'location', c.location,
      'barcode', c.barcode,
      'notes', c.notes,
      'sourceId', COALESCE(c.source_id, ''),
      'externalId', c.external_id,
      'createdAt', c.created_at,
      'updatedAt', c.updated_at
    ) ORDER BY c.created_at, c.id)
    FROM copies c
    WHERE c.game_id = games.id
  ))
);

DROP INDEX games_title;
ALTER TABLE games DROP COLUMN title;
ALTER TABLE games DROP COLUMN notes;
ALTER TABLE games DROP COLUMN cover_url;
ALTER TABLE games DROP COLUMN links;
ALTER TABLE games DROP COLUMN created_at;
ALTER TABLE games DROP COLUMN updated_at;
DROP TABLE copies;

ALTER TABLE sources ADD COLUMN doc TEXT NOT NULL DEFAULT '{}';

UPDATE sources SET doc = json_object(
  'v', 1,
  'type', type,
  'name', name,
  'enabled', json(CASE WHEN enabled THEN 'true' ELSE 'false' END),
  'syncIntervalSeconds', sync_interval_seconds,
  'settings', json(settings),
  'lastSync', json(last_sync),
  'createdAt', created_at,
  'updatedAt', updated_at
);

ALTER TABLE sources DROP COLUMN type;
ALTER TABLE sources DROP COLUMN name;
ALTER TABLE sources DROP COLUMN enabled;
ALTER TABLE sources DROP COLUMN sync_interval_seconds;
ALTER TABLE sources DROP COLUMN settings;
ALTER TABLE sources DROP COLUMN last_sync;
ALTER TABLE sources DROP COLUMN created_at;
ALTER TABLE sources DROP COLUMN updated_at;

ALTER TABLE providers ADD COLUMN doc TEXT NOT NULL DEFAULT '{}';

UPDATE providers SET doc = json_object(
  'v', 1,
  'kind', kind,
  'enabled', json(CASE WHEN enabled THEN 'true' ELSE 'false' END),
  'priority', priority,
  'settings', json(settings),
  'updatedAt', updated_at
);

DROP INDEX providers_kind;
ALTER TABLE providers DROP COLUMN kind;
ALTER TABLE providers DROP COLUMN enabled;
ALTER TABLE providers DROP COLUMN priority;
ALTER TABLE providers DROP COLUMN settings;
ALTER TABLE providers DROP COLUMN updated_at;

CREATE INDEX games_title ON games(json_extract(doc, '$.title') COLLATE NOCASE);
CREATE INDEX sources_name ON sources(json_extract(doc, '$.name') COLLATE NOCASE);
CREATE INDEX providers_kind ON providers(json_extract(doc, '$.kind'), json_extract(doc, '$.priority'));
```

A game without copies gets `"copies": []` (the aggregate over no rows); it decodes as no copies. A NULL `last_sync` becomes `"lastSync": null`, which decodes as no report. `ORDER BY` inside `json_group_array` needs SQLite 3.44+; `modernc.org/sqlite` v1.60.1 bundles 3.53.4 (checked).

- [ ] **Step 4: Rewrite the repositories**

`game_repository.go` — replace the whole file body below the imports with:

```go
// GameRepository implements game.Repository. Each game is one JSON document, copies included.
type GameRepository struct{ db *DB }

// NewGameRepository returns the repository backed by db.
func NewGameRepository(db *DB) *GameRepository { return &GameRepository{db: db} }

// List returns every game with its copies, by title.
func (r *GameRepository) List(ctx context.Context) ([]*game.Game, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, doc FROM games ORDER BY json_extract(doc, '$.title') COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*game.Game

	for rows.Next() {
		var (
			id  game.ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		g, err := decodeGame(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading game %s: %w", id, err)
		}

		games = append(games, g)
	}

	return games, rows.Err()
}

// Get returns a game with its copies.
func (r *GameRepository) Get(ctx context.Context, id game.ID) (*game.Game, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT doc FROM games WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, game.ErrGameNotFound
	}

	if err != nil {
		return nil, err
	}

	g, err := decodeGame(id, raw)
	if err != nil {
		return nil, fmt.Errorf("reading game %s: %w", id, err)
	}

	return g, nil
}

// Save writes the whole game, copies included. A copy moved to another game is part of that
// game's document from now on: saving both games in one transaction moves it.
func (r *GameRepository) Save(ctx context.Context, g *game.Game) error {
	raw, err := encodeGame(g)
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO games (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, g.ID(), raw)

	return err
}

// Delete removes a game and its copies (its cached details go with it).
func (r *GameRepository) Delete(ctx context.Context, id game.ID) error {
	res, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM games WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return game.ErrGameNotFound
	}

	return nil
}
```

Imports: `context`, `database/sql`, `errors`, `fmt`, `gamevault/internal/domain/game`. Remove `copyCols`, `copies`, `scanGame`, `encodeLinks`.

`source_repository.go`:

```go
// SourceRepository implements source.Repository. Each source is one JSON document.
type SourceRepository struct{ db *DB }

// NewSourceRepository returns the repository backed by db.
func NewSourceRepository(db *DB) *SourceRepository { return &SourceRepository{db: db} }

// List returns every source, by name.
func (r *SourceRepository) List(ctx context.Context) ([]*source.Source, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, doc FROM sources ORDER BY json_extract(doc, '$.name') COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*source.Source

	for rows.Next() {
		var (
			id  source.ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		s, err := decodeSource(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading source %s: %w", id, err)
		}

		out = append(out, s)
	}

	return out, rows.Err()
}

// Get returns the source with the given id.
func (r *SourceRepository) Get(ctx context.Context, id source.ID) (*source.Source, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT doc FROM sources WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, source.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	s, err := decodeSource(id, raw)
	if err != nil {
		return nil, fmt.Errorf("reading source %s: %w", id, err)
	}

	return s, nil
}

// Save inserts or updates a source.
func (r *SourceRepository) Save(ctx context.Context, s *source.Source) error {
	raw, err := encodeSource(s)
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO sources (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, s.ID(), raw)

	return err
}

// Delete removes a source.
func (r *SourceRepository) Delete(ctx context.Context, id source.ID) error {
	res, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return source.ErrNotFound
	}

	return nil
}
```

Imports: `context`, `database/sql`, `errors`, `fmt`, `gamevault/internal/domain/source`. Remove `sourceCols` and `scanSource`.

`provider_repository.go`:

```go
// ProviderRepository implements provider.Repository. Each provider's configuration is one JSON
// document.
type ProviderRepository struct{ db *DB }

// NewProviderRepository returns the repository backed by db.
func NewProviderRepository(db *DB) *ProviderRepository { return &ProviderRepository{db: db} }

// List returns the stored providers of a kind, in chain order.
func (r *ProviderRepository) List(ctx context.Context, kind provider.Kind) ([]*provider.Provider, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, doc FROM providers WHERE json_extract(doc, '$.kind') = ?
		 ORDER BY json_extract(doc, '$.priority'), id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*provider.Provider

	for rows.Next() {
		var (
			id  provider.ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		p, err := decodeProvider(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading provider %s: %w", id, err)
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// Save inserts or updates a provider.
func (r *ProviderRepository) Save(ctx context.Context, p *provider.Provider) error {
	raw, err := encodeProvider(p)
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO providers (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, p.ID(), raw)

	return err
}
```

Imports: `context`, `fmt`, `gamevault/internal/domain/provider`.

- [ ] **Step 5: Add a provider round trip** to `repository_test.go`

```go
func TestProviderRepository_roundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	repo := NewProviderRepository(openTest(t))
	now := time.Now().UTC()

	t.Run("GIVEN two cover providers and a barcode provider saved out of order", func(t *testing.T) {
		require.NoError(t, repo.Save(ctx, provider.Rehydrate("b", provider.KindCover, true, 1, schema.Settings{}, now)))
		require.NoError(t, repo.Save(ctx, provider.Rehydrate("a", provider.KindCover, false, 0, schema.Settings{"api_key": "k"}, now)))
		require.NoError(t, repo.Save(ctx, provider.Rehydrate("c", provider.KindBarcode, true, 0, schema.Settings{}, now)))

		t.Run("WHEN the cover chain is listed", func(t *testing.T) {
			got, err := repo.List(ctx, provider.KindCover)
			require.NoError(t, err)

			t.Run("THEN only covers come, by priority, with their state", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Equal(t, provider.ID("a"), got[0].ID())
				assert.False(t, got[0].Enabled())
				assert.Equal(t, "k", got[0].Settings()["api_key"])
				assert.Equal(t, provider.ID("b"), got[1].ID())
			})
		})
	})
}
```

Add the imports it needs (`github.com/stretchr/testify/assert`, `require`, `gamevault/internal/domain/provider`, `gamevault/internal/domain/schema`).

- [ ] **Step 6: Run the sqlite tests**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -v`
Expected: PASS, including `TestMigration0009`, `TestOpen_backupBeforeMigrating`, the existing `TestGameRepositoryRoundTripAndMove` and `TestSourceRepositoryAndBackup`, and `TestProviderRepository_roundTrip`.

- [ ] **Step 7: Run everything and commit**

Run: `task lint:fix && task lint && task test`
Expected: `0 issues.`, all tests green, `translations OK`.

```bash
git add internal/adapters/outbound/sqlite/
git commit -m "Store games, sources and providers as JSON documents (migration 0009)"
```

---

### Task 4: Check with the user's data, document, open the PR

**Files:**
- Modify: `docs/technical.md` (persistence), `.claude/memory/data-model.md`

- [ ] **Step 1: Write the capture script** to the session scratchpad (`S` is its path), not to the repository: `$S/capture.sh` with

```bash
#!/bin/bash
# Saves the API answers of the test server on :8093 into $1-<name>.json and prints how long each took.
set -eu
call() {
  curl -s -X POST -H 'Content-Type: application/json' -d "$3" -o "$1-$2.json" -w "$2 %{time_total}s\n" \
    "http://127.0.0.1:8093/gamevault.v1.$4"
}
call "$1" games '{"language":"en"}' GameService/ListGames
call "$1" sources '{}' SourceService/ListSources
call "$1" covers '{"kind":"cover"}' ProviderService/ListProviders
call "$1" metadata '{"kind":"metadata"}' ProviderService/ListProviders
call "$1" barcodes '{"kind":"barcode"}' ProviderService/ListProviders
```

and `chmod +x "$S/capture.sh"`. Each method gets only the fields it declares: Connect refuses unknown JSON fields.

- [ ] **Step 2: Capture main, then the branch**

```bash
git switch main && task test-server && "$S/capture.sh" "$S/before" && task test-server:stop
git switch feature/json-documents && task test-server && "$S/capture.sh" "$S/after"
```

Leave the branch's test server running. Check that its data directory (printed by `task test-server`) has the copy: `ls <data dir>/backups` shows `pre-migration-0009.db`.

- [ ] **Step 3: Compare**

```bash
for f in games sources covers metadata barcodes; do
  python3 -I -c 'import json,sys; a,b=(json.load(open(p)) for p in sys.argv[1:3]); print(sys.argv[3], "identical" if a==b else "DIFFERENT")' \
    "$S/before-$f.json" "$S/after-$f.json" "$f"
done
```

Expected: five lines ending in `identical`, and the timings printed in Step 2 similar or better. Any difference is a bug: write a test that reproduces it, fix it, and capture again.

- [ ] **Step 4: Look at it**

With the branch's test server still running, open http://127.0.0.1:8093/?v=<new number> in the browser pane: the library, a game sheet with copies, Sources, Providers, and System → Backups (the `pre-migration-0009.db` copy is listed). Then the mobile preset, reset the viewport, `task test-server:stop`.

- [ ] **Step 5: Docs and memory**

In `docs/technical.md`, in the persistence part (search for "SQLite"), add:

```markdown
Games (with their copies), sources and providers are stored as JSON documents, one row
`(id, doc)` each, mapped to the domain by the sqlite adapter (`internal/adapters/outbound/sqlite/docs.go`).
Adding a field is a code change only: older documents read it as empty. Every document carries a
format version (`"v": 1`); changing the meaning of a field bumps it and the adapter converts older
documents when it reads them. Indexes are on JSON expressions (title, source name, provider kind
and priority). Before migrating an existing database, Game Vault writes a copy to
`config/backups/pre-migration-<migration>.db`.
```

In `.claude/memory/data-model.md`, add a line: games (copies inside), sources and providers are JSON documents since migration 0009 (2026-10-09); document field names never change once released, like ExternalID prefixes; new fields need no migration.

- [ ] **Step 6: Verify, commit, push, open the PR**

Run `/verify` (lint, test, test server).

```bash
git add docs/technical.md .claude/memory/data-model.md docs/superpowers/plans/2026-10-09-json-documents.md
git commit -m "Document the JSON document storage"
git push -u origin feature/json-documents
gh pr create --base main --title "Store games, sources and providers as JSON documents" --body-file "$S/pr-body.md"
```

The PR body (written to `$S/pr-body.md` first) says what changes, links the spec and the plan, and lists what was verified (tests, the identical answers with the user's data on a copy, the List timings).
