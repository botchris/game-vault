---
name: write-go
description: Use when writing or editing Go in this repo (any *.go under cmd/ or internal/) — the house style, comment/naming rules the linters enforce, hexagonal layer boundaries, test design, and the mandatory lint verification step. Symptoms include adding/changing a type, func, handler, port, adapter or test; "write Go", a golangci-lint/revive/wsl/godot/errcheck error, or before declaring Go work done.
---

# Writing Go in Game Vault

## Sources of truth (read first, not after)

- Go house style = **Uber Go Style Guide** (https://github.com/uber-go/guide/blob/master/style.md),
  plus the conventions in `CLAUDE.md` (English everywhere, comments explain why, user-facing
  errors say what to do).
- Enforced rules = **`.golangci.yml`** at the repo root (golangci-lint v2, version pinned in
  `build/toolchain.Dockerfile`): the `standard` set (`errcheck`, `govet`, `ineffassign`,
  `staticcheck`, `unused`) plus `revive` (`exported`, `comment-spacings`, `error-strings`,
  `error-naming`, `early-return`, `superfluous-else`, `if-return`, `empty-lines`), `godot`,
  `wsl_v5`, `misspell`, `nakedret`, `prealloc`, and the `gofmt` formatter. Generated code
  (`internal/gen`) is excluded.
- New store or provider → `.claude/docs/integrations.md` and the `/new-source` or
  `/new-cover-provider` commands. Proto edits → `task generate`, then fix the handlers.

## The one non-negotiable: verify with the toolchain linter

Before treating any Go change as done:

```sh
task lint     # golangci-lint run ./... + buf lint, inside the toolchain container
task test     # go vet + go test ./... + TS type check + translations
```

golangci-lint only exists in the toolchain image: never install or run it on the host (the host
only has Docker and Task). `task lint:fix` applies the **safe** automatic fixes (`wsl_v5`, `godot`,
`misspell`, formatting). Never run a bare `golangci-lint run --fix`: staticcheck's fix for SA9004
types every constant in a mixed `const` block and breaks the build. If SA9004 fires, move the typed
constant into its own declaration instead.

## Comment & naming rules (the ones that bite)

- **Every exported identifier has a doc comment** (`revive:exported`), including getters,
  constructors and port implementations. It **starts with the identifier's name** (optionally
  after an article) and ends with a period — **never a colon**. `revive:exported` + `godot`.

  ```go
  // WRONG
  // Applies: only games with a copy in the GOG library.
  // RIGHT
  // Applies reports whether the game has a copy in the GOG library.
  ```

  For a method that just implements a port, name the port: `// Descriptor implements
  media.CoverProvider.` A grouped `const`/`var` block can share one comment above the block
  (`// Values of Kind.`).

- **Documented members are separated by a blank line.** In every grouped declaration — interface
  methods, struct fields, parenthesised `const` / `var` / `type` blocks — a member's doc comment has
  a blank line above it, separating it from the previous member. The only exception is the first
  member: its doc comment follows the opening `{` / `(` directly.

  ```go
  // WRONG: doc comments cuddled to the previous member
  var (
      // ErrNoSession means there is neither a stored session nor a code to start one.
      ErrNoSession = errors.New("amazon: open the sign-in link…")
      // ErrBadCode means Amazon rejected the code.
      ErrBadCode = errors.New("amazon rejected the code…")
  )

  // RIGHT
  var (
      // ErrNoSession means there is neither a stored session nor a code to start one.
      ErrNoSession = errors.New("amazon: open the sign-in link…")

      // ErrBadCode means Amazon rejected the code.
      ErrBadCode = errors.New("amazon rejected the code…")
  )
  ```

  ```go
  type Output struct {
                                 // ← WRONG: no blank line after the opening brace
      // Text is what the program printed.
      Text string
  }
  ```

  Members without doc comments may stay cuddled, and short trailing comments on enum values
  (`KindKey Kind = "key" // a redeemable CD key`) are fine.

- **Every method of a named interface has a doc comment**, `Descriptor()` included — interfaces
  are the ports other layers code against, so each method says what it promises:

  ```go
  // Provider is the port each source type implements (Humble, Steam...).
  type Provider interface {
      // Descriptor describes the source type: its id, name and settings.
      Descriptor() source.TypeDescriptor

      // Fetch returns every copy the account currently holds. Warnings are non-fatal issues.
      Fetch(ctx context.Context, settings source.Settings) (copies []game.ImportedCopy, warnings []string, err error)

      // Test checks the settings against the real service as cheaply as it can. A nil error means
      // they work; an error says why not.
      Test(ctx context.Context, settings source.Settings) error
  }
  ```

  Field and member docs start with the member's name and read as a sentence
  (`// Forwarded reports that…`), never `// Forwarded: …`.

  These rules are checked by `tools/docspacing`, which `task lint` runs (golangci-lint has no rule
  for them) and `task lint:fix` applies (`-fix` inserts and removes the blank lines; missing
  interface docs must be written by hand).

- **Error strings**: lowercase, no trailing punctuation (`revive:error-strings`). Sentinel errors
  are `ErrX` (exported) / `errX` (`error-naming`) and callers match them with `errors.Is`. Wrap with
  context: `fmt.Errorf("caching cover: %w", err)`. Errors that reach the UI say what happened and
  what to do ("… paste a new code").
- **Unchecked errors**: `errcheck` is on. When ignoring an error is correct, say so explicitly with
  `_ =` and a reason: `_ = raw.Close() // the handshake error is the one worth reporting`. Closing
  what was only read (`res.Body.Close`, `rows.Close`, `(*sql.Tx).Rollback`) and
  `http.ResponseWriter.Write` are excluded in `.golangci.yml`; closing a file that was **written**
  is not, because that error can mean lost data. `errcheck` is off in `_test.go` (fake servers
  write canned answers).
- **Early return / no superfluous else**: return early; don't wrap the happy path in `else`.
  `revive:early-return`, `superfluous-else`, `if-return`.
- **`wsl_v5` cuddling**: keep a statement cuddled only with lines that share a variable; otherwise
  separate with a blank line. A `return` after more than two lines in a block needs a blank line
  above it.
- **`prealloc`**: when appending in a loop over a known-length slice, `make([]T, 0, len(in))`.
- Fix spelling in US English — `misspell` flags e.g. `behaviour` → `behavior`.

## Logging

`log/slog`, messages lowercase and short, error under the key `"error"`:
`log.Warn("pruning backups", "error", err)`. Never log secrets (cookies, tokens, API keys, NPSSO,
`PRODrememberMe`…): they live in `config/gamevault.db` and must never reach logs or chat.

## Layering (hexagonal / DDD — not lint-enforced, still required)

Dependencies point inward; the compiler won't stop a violation, so watch it:

- `internal/domain/` imports no other layer — no `application`, no `adapters`, no HTTP or SQL
  packages. Domain subpackages (`game`, `source`, `provider`, `schema`, `auth`, `settings`) only
  import each other where the model really depends on it (e.g. `source` reuses `schema`).
- **Ports (interfaces) live in `internal/application`**: next to the use case that needs them
  (`sync.Provider`, `media.CoverProvider`, `logs.Files`, …), or in `application/port` when
  cross-cutting (`TxManager`, `Clock`). `internal/adapters/outbound/<name>` implements them;
  `cmd/gamevault/main.go` wires them by hand (no DI framework; registration order = default chain
  order).
- **Optional capabilities are separate small interfaces** checked with a type assertion
  (`sync.Preparer`, `sync.KeepAliver`, `media.ImageHoster`), never boolean flags. `Test(ctx,
  settings) error` is not optional: it is part of `sync.Provider` and `media.Provider`.
- `internal/adapters/inbound/rpc` maps Connect requests onto application calls and back
  (`mapper.go`); it must not leak domain aggregates or adapter types into the API, nor generated
  `pb` types into the application.
- Adapters reach external services through **base URL fields** (`BaseURL`, `APIURL`…) and an
  injectable `*http.Client`, so tests point them at `httptest`.
- SQLite: one writer, transactions via `TxManager.WithinTx`; schema changes are new numbered files
  in `internal/adapters/outbound/sqlite/migrations/`, never edits to old ones.
- Unattended requests (keep-alives, scheduled scans) are jittered; see
  `internal/application/sync`.

## Test design

- **GIVEN-WHEN-THEN.** Where practical, shape a test as nested `t.Run` blocks —
  `GIVEN <preconditions>` → `WHEN <action>` → `THEN <asserts>`. The reference in this repo is
  `TestAccessControl` in `internal/adapters/inbound/rpc/auth_test.go`:

  ```go
  func TestAccessControl(t *testing.T) {
      ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
      defer cancel()

      t.Run("GIVEN a new server with the default settings", func(t *testing.T) {
          c := newServer(t, &fakeProvider{})
          local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)
  
          t.Run("WHEN this computer asks for its status", func(t *testing.T) {
              st, err := local.GetAuthStatus(ctx, connect.NewRequest(&pb.GetAuthStatusRequest{}))
              require.NoError(t, err)

              t.Run("THEN it is trusted without signing in and may create the user", func(t *testing.T) {
                  assert.Equal(t, "trusted", st.Msg.Principal.GetMethod())
                  assert.True(t, st.Msg.CanSetup)
              })
          })
      })
  }
  ```

  In the assert phase, an **additional** verified condition nests as `t.Run("AND <condition>", …)`
  inside (or after) the `THEN` — and `AND` blocks can nest further. An **alternative** outcome is a
  sibling `t.Run("OR <condition>", …)`. Subtests run in order, so a later `WHEN` may build on the
  state an earlier one left (a scenario); say so in its name when it matters.

  *Exception:* quick table-driven tests covering many input values stay flat (e.g.
  `internal/domain/auth/auth_test.go`) — don't force GWT on them.

- **Never hand the code under test a bare context.** `context.Background()` / `context.TODO()`
  must NEVER be passed into the code under test. Always use a **time-bounded** context derived
  from the test's: `context.WithTimeout(t.Context(), 10*time.Second)`, created at the test level or
  per `t.Run`. **No exceptions.**

- **External services:** every adapter has a test against an `httptest` server that reproduces
  the real API's shape **and** its failure modes (expired session, 302 to a login page, rate
  limit, GraphQL errors with HTTP 200…). Paste real response shapes (with fake values) and say in a
  comment where they came from. See the existing `provider_test.go` files.
- **Mocks:** prefer `github.com/stretchr/testify/mock` for ports; hand-written fakes implementing
  the port interfaces (`fakeProvider` in the rpc tests) are also fine where they read more clearly.
- **Assertions:** `stretchr/testify` — `require` to stop, `assert` for soft checks. Name new tests
  `TestSubject_scenario`.
- A nil optional dependency degrades to a no-op so tests can pass `nil` (e.g. `catalog.NewService`
  accepts a nil cover cache), rather than forcing every test to build the whole graph.
- Tests never touch `config/` or the user's server on :8080; use `t.TempDir()` for files and
  databases.
- Older tests still use `t.Fatalf` and `context.Background()`. Don't mass-rewrite them, but when you
  substantially change one, bring it to these rules.
- Keep the same comment/style rules in `_test.go` — the linter checks them too (except `errcheck`).

## Definition of done for a Go change

1. `task lint` — 0 issues.
2. `task test` — green.
3. If a `.proto` changed, `task generate` ran and the generated code is committed with it.
4. For UI-visible or external-service changes, the rest of "How to verify a change" in
   `CLAUDE.md` (test server on :8093, bogus-credential probe of the real endpoint).
