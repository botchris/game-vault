# Ownership Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Answer "do I already own it?" where the question is asked: a label on Steam, GOG, Humble Bundle and Fanatical product pages, marks and a summary on Humble and Fanatical bundle pages (through the Game Vault Connector extension), and a "Do I own it?" paste box in the web library.

**Architecture:** A read-only use case (`internal/application/ownership`) matches items (`{store, id}` and/or `{title}`) against the catalog: store links first (automatic links included), then `game.SameTitle` through a new `game.TitleKey` index, then `game.MatchKey`, which can only make an item "possible". It is served as `OwnershipService/CheckOwnership`, the only RPC an access token may call. Access tokens (`gvt_` + 32 random bytes, only a SHA-256 stored) live in the auth domain and a new SQLite table; the auth middleware accepts `Authorization: Bearer gvt_…` for that one procedure, counts failures in the existing sign-in throttling and records the last use at most once a minute. The extension keeps what it reads as data (`extension/ownership/stores.js`), draws labels in closed shadow roots, follows single-page sites with a debounced MutationObserver, and only its background talks to the user's server (token, 200-item requests, a one-minute cache).

**Tech Stack:** Go 1.26, ConnectRPC/buf, SQLite; React 19 + TypeScript, i18next; browser extension in plain JavaScript (Manifest V3, Chrome and Firefox); Node test runner, happy-dom (new web dev dependency, used by the extension's tests only).

**Spec:** `docs/superpowers/specs/2026-10-12-ownership-check-design.md`

## Global Constraints

- Everything compiles, tests and generates in the toolchain container through Task: `task lint`, `task test`, `task generate`, `task lint:fix`. One Go package: `task go -- test ./internal/application/ownership/ -run TestCheck -v`. One web Node test: `docker run --rm -v "$PWD:/src" -w /src gamevault-toolchain:446777a3814f node --test --test-timeout=10000 web/tests/ownership.test.mjs`. One extension test (it needs the web's `node_modules` volume, where happy-dom lives): `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/ownership-pages.test.mjs`. The tag is the first 12 hex digits of the SHA-256 of `build/toolchain.Dockerfile` (`shasum -a 256 build/toolchain.Dockerfile | cut -c1-12`; it is `446777a3814f` today). `task test` runs every web and extension test. Never use host Go, Node, npm or linters.
- Load the `write-go` skill before writing Go: a doc comment on every exported identifier, one struct field per line, a blank line between documented members, GIVEN/WHEN/THEN tests with `context.WithTimeout(t.Context(), 10*time.Second)` and testify. Test code below is written compactly; run `task lint:fix` (it puts each struct field on its own line and fixes cuddling) before `task lint`.
- The repository is public: invented test data only (titles such as "Lantern Keep", "Echo Valley", "Harbor Lights"; ids such as Steam `4410`), no personal data, accounts, library counts or results from the user's config copy in code, comments, docs, commits or the PR.
- English in the repository. Every web UI string goes through `t()` with keys in `web/src/i18n/locales/en.json` and `es.json` (`task i18n` passes). Every extension string goes through `chrome.i18n.getMessage` with keys in `extension/_locales/en/messages.json` and `extension/_locales/es/messages.json` (the extension test `i18n.test.mjs` checks they match). No UI text names or compares with other products; the stores Game Vault reads (Steam, GOG, Humble Bundle, Fanatical) are named.
- Web files that Node tests import (`web/src/lib/ownership.ts`, `web/src/lib/gameRoute.ts`) use structural types only: no generated protobuf code, no `lib/model.ts`, imports with the `.ts` extension.
- Security model, unchanged: a request is trusted only when its peer is in a trusted network, it has no proxy headers and its `Host` is an address or `localhost`. An access token is a credential like a password, never a reason to trust the network, and never combined with a session cookie or the network's trust. A token reaches `/gamevault.v1.OwnershipService/CheckOwnership` and nothing else. Only `Authorization: Bearer gvt_…` is a token; other `Authorization` headers (a proxy's Basic or Bearer credentials) are ignored.
- Tokens: `gvt_` + 32 random bytes in unpadded base64url (47 characters); only `SHA-256(token)` in hex is stored; the token is shown once. Never log a token, a hash or the `Authorization` header.
- Ownership lookup: at most 200 items per request (more is `InvalidArgument` "at most 200 items can be checked at once; split the list"); at most 5 candidates; results in the order of the items; no side effects (no writes, no provider or network calls).
- Extension privacy: it reads only the page being viewed, sends only `{store, id, title}` of that page and only to the Game Vault address the user chose among the enabled ones (`credentials: 'omit'`), never to the store. Without the store's host permission it does nothing there.
- Safety: never touch `config/` or the server on port 8080; check the UI with `task test-server` (port 8093, a copy of the config, `-no-unattended`). On that copy never scan sources whose credentials rotate (Ubisoft, Epic, GOG, Xbox, PlayStation, Battle.net, EA). Live store pages are only read, signed out. The agent never drives Fanatical's site (its terms forbid automated access): its live check is done by the user in their own browser.
- No version is tagged, nothing is pushed or published.

## Review Focus

- A pasted line whose title contains words the cleaner targets ("DLC Hunters", "Soundtrack Hero", "1983 Revival: Night Shift", "7 Nights at Sea", "Ca$h Rally", "Mirror Run 2: 50% More Mirrors") must reach the lookup whole; only lines marked as add-ons ("[DLC]", "- Original Soundtrack", "… OST") are skipped, and they are counted. Test: Task 6 (`cleanTitles` "words that belong to a title are kept").
- When Game Vault cannot answer (network error, 5xx, 429, a response that is not JSON or has the wrong number of results, a refused token, the extension not set up), a store page must say "Game Vault unavailable" with a hint, never "Not in your collection", and a bundle summary must say unavailable too. Tests: Task 7 (`labelModel` unavailable cases; the content controller with a failing `send`), Task 8 (`createLookup` "is unavailable — never not owned"), Task 9 (`summaryModel` unavailable).
- A single-page store (Fanatical) that draws the title or the bundle cards after the script started, or moves to another product without a reload, must still be labelled, asked once per product, and lose the previous product's label. Tests: Task 7 ("draws its title late", "moving to another product"), Task 9 ("games a bundle page adds later").
- A bundle with hundreds of games (450) must be checked whole: the background splits it into requests of at most 200 and keeps the order, and the page gets one mark per game and one summary counting all of them. Tests: Task 8 ("more than 200 games is asked in parts"), Task 9 ("a bundle of 450 games").
- An access token must reach only the ownership lookup: any other RPC, the AuthService (creating more tokens included) and media are refused as unauthenticated even from this trusted computer; revoking it takes effect on the very next request (no cached validity); and a proxy's own `Authorization` header must not break the web UI. Tests: Task 4 (`TestAccessTokens_endToEnd`: "WHEN the token calls anything else, even from this trusted computer", "WHEN it is revoked", "WHEN a proxy in front of Game Vault adds its own credentials").

## Rulings made while planning (where the spec is silent or ambiguous)

- **Title matching in three steps.** 1. a store link equal to `{store, id}` (automatic links included, compared as trimmed id and lower-case store key); 2. `game.SameTitle`, through `game.TitleKey` (the joined words SameTitle compares), so a request does not compare every title with every game; 3. `game.MatchKey` (the consolidator's rule that ignores edition and store tags). A step that finds one game says owned (steps 1 and 2) and several games say possible; step 3 can only say possible ("Copper Coast - Deluxe Edition" is "possible: Copper Coast"), because the tags it ignores may name another product.
- **Current copies.** Copies sold, given away (gifted) or expired are left out of the summary, and a game without any other copy (or without copies at all) does not match: it is not owned any more. Lent copies and redeemed keys count.
- **Only `Bearer gvt_…` is a token.** Reverse proxies that authenticate users forward their own `Authorization` header (Basic, or an identity provider's Bearer token); treating every Bearer header as a Game Vault token would lock the web UI out behind them. Those headers are ignored and the request is handled as before.
- **A token request is judged by the token alone.** It must target the ownership procedure (else 401), its session cookie is dropped and the trusted network is not consulted. A token used from this computer for `ListGames` is refused.
- **Throttling.** Only `gvt_` tokens that match no stored token count as failures (the same counter as wrong passwords: 10 in 15 minutes). While throttled, even a valid token waits (as a valid password does), answered as `resource_exhausted` (HTTP 429). A valid token sent to the wrong procedure is not a failure.
- **Last used, once a minute, no cache.** The token is read from SQLite on every request (one indexed lookup), so revoking is immediate; `last_used_at` is written only when the stored value is a minute old or more.
- **Status and kind are strings** in `ownership.proto` (`"owned" | "possible" | "not_owned"`, `"library" | "key" | "physical"`), as `Principal.method` is in `auth.proto`: the extension reads Connect JSON and the web helpers stay structural.
- **`#/game/<id>` did not exist.** The library page opens the sheet of the game a `#/game/<id>` address names (and on every hash change), and closing that sheet returns the address to `#/library`.
- **The ownership service reads at most 256 KiB per request** (`connect.WithReadMaxBytes`): 200 items with long titles fit easily, and a token holder cannot send huge bodies.
- **Which Game Vault the extension asks.** The popup picks one of the enabled Game Vault addresses (the first by default) and keeps the token next to it; the background sends lookups only there, only while that address is still enabled and the option is on. Turning the option off unregisters the store scripts and gives back the store host permissions that no sign-in confirmation uses.
- **Extension languages.** The extension gets `_locales` (en, es, `default_locale: "en"`); labels and the whole popup use `chrome.i18n.getMessage`. The confirmation window keeps its English texts (not part of this feature). The web's "press Enable" hint stops naming the popup's button, which is now translated.
- **"DLC"/"Soundtrack" markers skip the line.** Stripping the marker would turn "Lantern Keep Soundtrack" into an owned "Lantern Keep". Lines marked as add-ons (bracketed `[DLC]`, `(Soundtrack)`, `[OST]`; a `DLC:` prefix; a `- DLC`, `- Original Soundtrack` suffix; a trailing `Soundtrack`, `DLC` or `OST` word) are skipped and counted ("2 lines skipped as DLC or soundtrack"); the words elsewhere in a title stay.
- **Counting.** "You own N of M" counts owned rows (web) or unique titles (bundle pages, case-insensitive); possible ones are shown apart and are not copied by "Copy the ones I don't own".
- **Links.** A product label links to the game's sheet (possible: to the first candidate) in a new tab. Bundle marks have no link: store item cards are often links themselves, and nested links break.
- **Unanswered bundle games are not asked again** until the page is reloaded (they get an invisible "checked" mark, so the MutationObserver does not loop).
- **A page sends at most 2000 items** to the background; the rest read as unavailable.
- **Extension tests need a DOM.** happy-dom is added to `web/package.json` dev dependencies (it is already in the container volume the tests run with) and loaded from `web/node_modules` by `extension/tests/helpers.mjs`; nothing is added to the packed extension.
- **The extension version becomes 1.1.0.**

## Files

| File | Responsibility |
|---|---|
| `internal/domain/auth/token.go` (new), `auth.go` | `AccessToken`, `NewAccessToken`, `HashToken`, `IsAccessToken`, `MethodToken`, `Principal.TokenID`, repository methods |
| `internal/adapters/outbound/sqlite/auth_repository.go`, `migrations/0011_access_tokens.sql` | the `access_tokens` table and its repository methods |
| `internal/domain/game/values.go` | `TitleKey` |
| `internal/application/ownership/service.go` (new) | the lookup: index, matching rules, current copies, limits |
| `internal/application/auth/service.go` | `CreateAccessToken`, `AccessTokens`, `RevokeAccessToken`, `AuthenticateToken` |
| `proto/gamevault/v1/ownership.proto` (new), `auth.proto` | `OwnershipService`; token RPCs in `AuthService` |
| `internal/adapters/inbound/rpc/ownership_handler.go` (new), `auth_handler.go`, `auth_http.go`, `server.go`, `mapper.go` | handlers, bearer tokens in the middleware |
| `cmd/gamevault/main.go` | wiring |
| `web/src/features/system/AccessTokens.tsx` (new), `SecurityCard.tsx` | System → Security → Access tokens |
| `web/src/lib/ownership.ts`, `web/src/lib/gameRoute.ts` (new), `web/src/features/library/OwnershipDialog.tsx` (new), `LibraryPage.tsx`, `web/src/components/ui.tsx`, `web/src/api/client.ts`, `web/src/styles.css` | bundle checker, `#/game/<id>` |
| `extension/ownership/{stores,read,label,content}.js` (new) | store data, page reading, labels, the content controller |
| `extension/lib/ownership.js` (new), `background.js`, `popup.{html,js}`, `ui.css`, `manifest.json`, `_locales/{en,es}/messages.json` (new) | settings, permissions, script registration, lookups |
| `extension/tests/*` | Node tests with a fake `chrome` and happy-dom, sample pages with invented titles |
| `docs/technical.md`, `README.md`, `extension/README.md`, `extension/PRIVACY.md`, `.claude/docs/ui.md`, `.claude/memory/{connector,security-model}.md`, `.claude/MEMORY.md` | documentation |

---

### Task 1: Access tokens in the auth domain and in SQLite

**Files:**
- Create: `internal/domain/auth/token.go`, `internal/domain/auth/token_test.go`, `internal/adapters/outbound/sqlite/migrations/0011_access_tokens.sql`, `internal/adapters/outbound/sqlite/token_repository_test.go`
- Modify: `internal/domain/auth/auth.go` (Method, Principal, Repository), `internal/adapters/outbound/sqlite/auth_repository.go`, `internal/adapters/outbound/sqlite/migrate_test.go`, `internal/application/auth/service.go` (`hashToken` → `auth.HashToken`)

**Interfaces:**
- Produces:
  - `auth.TokenPrefix = "gvt_"`, `auth.TokenBytes = 32`
  - `type auth.AccessToken struct { ID string; Name string; Hash string; CreatedAt time.Time; LastUsedAt time.Time }`
  - `auth.NewAccessToken(name string, random []byte, now time.Time) (AccessToken, string, error)` (the string is the secret)
  - `auth.HashToken(secret string) string` (SHA-256, hex), `auth.IsAccessToken(s string) bool`
  - `auth.MethodToken Method = "token"`, `Principal.TokenID string`
  - `auth.Repository` gains `AccessTokens(ctx) ([]AccessToken, error)` (newest first), `AccessTokenByHash(ctx, hash string) (AccessToken, error)` (`ErrNotFound`), `SaveAccessToken(ctx, AccessToken) error`, `TouchAccessToken(ctx, id string, at time.Time) error`, `DeleteAccessToken(ctx, id string) error` (`ErrNotFound`)
  - table `access_tokens(id, name, token_hash UNIQUE, created_at, last_used_at)`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing domain test** (`internal/domain/auth/token_test.go`)

```go
package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAccessToken(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	random := bytes.Repeat([]byte{0xA5}, TokenBytes)

	t.Run("GIVEN a name and 32 random bytes", func(t *testing.T) {
		tok, secret, err := NewAccessToken("  Extension,   laptop ", random, now)
		require.NoError(t, err)

		t.Run("THEN the secret starts with gvt_ and carries every random byte", func(t *testing.T) {
			assert.True(t, strings.HasPrefix(secret, TokenPrefix))
			assert.Len(t, secret, len(TokenPrefix)+43) // 32 bytes in unpadded base64url
			assert.True(t, IsAccessToken(secret))
		})

		t.Run("AND only its hash is kept", func(t *testing.T) {
			assert.Equal(t, HashToken(secret), tok.Hash)
			assert.Len(t, tok.Hash, 64)
			assert.NotContains(t, tok.Hash, strings.TrimPrefix(secret, TokenPrefix))
		})

		t.Run("AND the name is tidied, the id set, and it was never used", func(t *testing.T) {
			assert.Equal(t, "Extension, laptop", tok.Name)
			assert.NotEmpty(t, tok.ID)
			assert.Equal(t, now, tok.CreatedAt)
			assert.True(t, tok.LastUsedAt.IsZero())
		})
	})

	t.Run("GIVEN two tokens made from different random bytes", func(t *testing.T) {
		a, sa, err := NewAccessToken("a", bytes.Repeat([]byte{1}, TokenBytes), now)
		require.NoError(t, err)
		b, sb, err := NewAccessToken("b", bytes.Repeat([]byte{2}, TokenBytes), now)
		require.NoError(t, err)

		t.Run("THEN their secrets, hashes and ids differ", func(t *testing.T) {
			assert.NotEqual(t, sa, sb)
			assert.NotEqual(t, a.Hash, b.Hash)
			assert.NotEqual(t, a.ID, b.ID)
		})
	})

	t.Run("GIVEN bad input", func(t *testing.T) {
		t.Run("THEN names that are empty, too long or with control characters are refused", func(t *testing.T) {
			for _, name := range []string{"", "   ", strings.Repeat("x", 61), "lap\x00top"} {
				_, _, err := NewAccessToken(name, random, now)

				var ve *ValidationError
				assert.ErrorAs(t, err, &ve, "%q", name)
			}
		})

		t.Run("AND fewer than 32 random bytes are refused", func(t *testing.T) {
			_, _, err := NewAccessToken("short", random[:16], now)
			assert.Error(t, err)
		})
	})
}

func TestIsAccessToken(t *testing.T) {
	for s, want := range map[string]bool{
		"gvt_abc":                         true,
		"gvt_":                            false,
		"GVT_abc":                         false,
		"eyJhbGciOiJSUzI1NiJ9.e30.sig":    false, // a token an authenticating proxy forwards
		"YWRtaW46c2VjcmV0":                false,
		"":                                false,
	} {
		assert.Equal(t, want, IsAccessToken(s), "%q", s)
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `task go -- test ./internal/domain/auth/ -run 'TestNewAccessToken|TestIsAccessToken'`
Expected: FAIL to compile (`undefined: NewAccessToken`, `undefined: TokenBytes`).

- [ ] **Step 4: Write `internal/domain/auth/token.go`**

```go
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TokenPrefix starts every access token, so a leaked one is easy to recognize and to search for.
const TokenPrefix = "gvt_"

// TokenBytes is how many random bytes an access token carries.
const TokenBytes = 32

// maxTokenName bounds a token's name, in characters.
const maxTokenName = 60

// AccessToken lets a program, such as the browser extension, look up what the user owns and
// nothing else (see MethodToken). Only a hash of its secret is stored: the secret is shown once,
// when the token is created.
type AccessToken struct {
	ID string

	// Name says where the token is used ("Extension, laptop").
	Name string

	// Hash is HashToken of the secret.
	Hash      string
	CreatedAt time.Time

	// LastUsedAt is when the token last authenticated a request, kept to the minute; zero when never.
	LastUsedAt time.Time
}

// NewAccessToken creates a token named name from random bytes (at least TokenBytes, from a
// cryptographic source) and returns it with its secret: TokenPrefix and the bytes in unpadded
// base64url.
func NewAccessToken(name string, random []byte, now time.Time) (AccessToken, string, error) {
	name, err := normalizeTokenName(name)
	if err != nil {
		return AccessToken{}, "", err
	}

	if len(random) < TokenBytes {
		return AccessToken{}, "", fmt.Errorf("an access token needs %d random bytes, got %d", TokenBytes, len(random))
	}

	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(random)

	return AccessToken{
		ID:        uuid.Must(uuid.NewV7()).String(),
		Name:      name,
		Hash:      HashToken(secret),
		CreatedAt: now,
	}, secret, nil
}

// HashToken returns the stored form of a session or access token: its SHA-256, in hex. Tokens are
// long and random, so a fast hash is enough (passwords need bcrypt).
func HashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// IsAccessToken reports whether a bearer credential is one of Game Vault's access tokens. Others,
// such as the token an authenticating proxy in front of Game Vault forwards, are not Game Vault's
// to check.
func IsAccessToken(s string) bool {
	return len(s) > len(TokenPrefix) && strings.HasPrefix(s, TokenPrefix)
}

func normalizeTokenName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxTokenName || strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalid("give the token a name of at most %d characters, such as \"Extension, laptop\"", maxTokenName)
	}

	return name, nil
}
```

Run the domain test. Expected: PASS.

- [ ] **Step 5: The principal and the port** (`internal/domain/auth/auth.go`)
  - In the `Method` constants:

    ```go
    const (
    	MethodTrusted Method = "trusted" // from a trusted network, no sign-in
    	MethodSession Method = "session" // signed in with the password
    	MethodToken   Method = "token"   // an access token: it may only look up what the user owns
    )
    ```
  - In `Principal`, after `UserID string // session only`:

    ```go
    	UserID string // session only

    	// TokenID is the access token's id (token only).
    	TokenID string
    ```
  - Append to `Repository`, after `DeleteExpiredSessions`:

    ```go
    	// AccessTokens returns every access token, newest first.
    	AccessTokens(ctx context.Context) ([]AccessToken, error)

    	// AccessTokenByHash returns the access token whose secret has that hash, or ErrNotFound.
    	AccessTokenByHash(ctx context.Context, hash string) (AccessToken, error)

    	// SaveAccessToken creates an access token.
    	SaveAccessToken(ctx context.Context, t AccessToken) error

    	// TouchAccessToken records when an access token was last used.
    	TouchAccessToken(ctx context.Context, id string, at time.Time) error

    	// DeleteAccessToken removes an access token (revoking it), or returns ErrNotFound.
    	DeleteAccessToken(ctx context.Context, id string) error
    ```
  - In `internal/application/auth/service.go`, delete `func hashToken` and replace its three calls with `auth.HashToken(...)`; drop the now unused `crypto/sha256` and `encoding/hex` imports.

- [ ] **Step 6: Failing storage test** (`internal/adapters/outbound/sqlite/token_repository_test.go`)

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/auth"
)

func TestAccessTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	r := NewAuthRepository(openTest(t))
	t0 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	older := auth.AccessToken{ID: "t1", Name: "Extension, laptop", Hash: auth.HashToken("gvt_one"), CreatedAt: t0}
	newer := auth.AccessToken{ID: "t2", Name: "Extension, phone", Hash: auth.HashToken("gvt_two"), CreatedAt: t0.Add(time.Hour)}

	t.Run("GIVEN two saved tokens", func(t *testing.T) {
		require.NoError(t, r.SaveAccessToken(ctx, older))
		require.NoError(t, r.SaveAccessToken(ctx, newer))

		t.Run("WHEN they are listed", func(t *testing.T) {
			got, err := r.AccessTokens(ctx)
			require.NoError(t, err)

			t.Run("THEN the newest comes first, and neither was used", func(t *testing.T) {
				assert.Equal(t, []auth.AccessToken{newer, older}, got)
			})
		})

		t.Run("WHEN one is found by its hash and touched", func(t *testing.T) {
			got, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_one"))
			require.NoError(t, err)
			require.NoError(t, r.TouchAccessToken(ctx, got.ID, t0.Add(2*time.Hour)))

			again, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_one"))
			require.NoError(t, err)

			t.Run("THEN it records when it was last used", func(t *testing.T) {
				assert.Equal(t, "Extension, laptop", got.Name)
				assert.Equal(t, t0.Add(2*time.Hour), again.LastUsedAt)
			})
		})

		t.Run("WHEN a hash matches no token", func(t *testing.T) {
			_, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_unknown"))

			t.Run("THEN it is not found", func(t *testing.T) {
				assert.ErrorIs(t, err, auth.ErrNotFound)
			})
		})

		t.Run("WHEN one is deleted", func(t *testing.T) {
			require.NoError(t, r.DeleteAccessToken(ctx, "t1"))
			_, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_one"))

			t.Run("THEN it is gone, and deleting it again says so", func(t *testing.T) {
				assert.ErrorIs(t, err, auth.ErrNotFound)
				assert.ErrorIs(t, r.DeleteAccessToken(ctx, "t1"), auth.ErrNotFound)
			})
		})

		t.Run("AND the table holds hashes, never a token", func(t *testing.T) {
			var n int
			require.NoError(t, r.db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_tokens WHERE token_hash LIKE 'gvt_%'`).Scan(&n))
			assert.Zero(t, n)
		})
	})
}
```

Append to `internal/adapters/outbound/sqlite/migrate_test.go`:

```go
func TestMigration0011(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database migrated up to 0010", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0010")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN this version opens it", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)

			defer db.Close()

			t.Run("THEN it was copied first, and access tokens can be stored", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0011.db"))
				assert.NoError(t, NewAuthRepository(db).SaveAccessToken(ctx, auth.AccessToken{
					ID:        "t1",
					Name:      "Extension, laptop",
					Hash:      auth.HashToken("gvt_one"),
					CreatedAt: time.Now(),
				}))
			})
		})
	})
}
```

(add `"gamevault/internal/domain/auth"` to that file's imports if it is not there).

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run 'TestAccessTokens|TestMigration0011'`
Expected: FAIL to compile (`r.SaveAccessToken undefined`).

- [ ] **Step 7: The migration** (`internal/adapters/outbound/sqlite/migrations/0011_access_tokens.sql`)

```sql
-- Access tokens for programs that may only look up what the user owns (the browser extension).
-- Only a SHA-256 of each token is stored; last_used_at is '' until the token is first used.
CREATE TABLE access_tokens (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
```

- [ ] **Step 8: The repository methods** (append to `internal/adapters/outbound/sqlite/auth_repository.go`, before `const keyAuth`)

```go
// tokenRow is a *sql.Row or *sql.Rows positioned on an access token.
type tokenRow interface {
	// Scan copies the row's columns into dest.
	Scan(dest ...any) error
}

const accessTokenColumns = `id, name, token_hash, created_at, last_used_at`

func scanAccessToken(row tokenRow) (auth.AccessToken, error) {
	var (
		t             auth.AccessToken
		created, used string
	)

	err := row.Scan(&t.ID, &t.Name, &t.Hash, &created, &used)
	t.CreatedAt, t.LastUsedAt = parseTime(created), parseTime(used)

	return t, err
}

// AccessTokens returns every access token, newest first.
func (r *AuthRepository) AccessTokens(ctx context.Context) ([]auth.AccessToken, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx, `SELECT `+accessTokenColumns+` FROM access_tokens ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []auth.AccessToken

	for rows.Next() {
		t, err := scanAccessToken(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, t)
	}

	return out, rows.Err()
}

// AccessTokenByHash returns the access token whose secret has that hash, or auth.ErrNotFound.
func (r *AuthRepository) AccessTokenByHash(ctx context.Context, hash string) (auth.AccessToken, error) {
	t, err := scanAccessToken(r.db.conn(ctx).QueryRowContext(ctx, `SELECT `+accessTokenColumns+` FROM access_tokens WHERE token_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return t, auth.ErrNotFound
	}

	return t, err
}

// SaveAccessToken creates an access token.
func (r *AuthRepository) SaveAccessToken(ctx context.Context, t auth.AccessToken) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `INSERT INTO access_tokens (`+accessTokenColumns+`) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Hash, formatTime(t.CreatedAt), optionalTime(t.LastUsedAt))

	return err
}

// TouchAccessToken records when an access token was last used.
func (r *AuthRepository) TouchAccessToken(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `UPDATE access_tokens SET last_used_at = ? WHERE id = ?`, formatTime(at), id)
	return err
}

// DeleteAccessToken removes an access token, or returns auth.ErrNotFound when there is none with that id.
func (r *AuthRepository) DeleteAccessToken(ctx context.Context, id string) error {
	res, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM access_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return auth.ErrNotFound
	}

	return nil
}
```

`optionalTime` already exists in `docs.go` (`""` for a zero time).

Run the storage tests. Expected: PASS.

- [ ] **Step 9: Lint, test, commit**

Run: `task lint:fix`, then `task lint` (expected `0 issues.`) and `task test` (expected: green).

```bash
git add internal/domain/auth internal/adapters/outbound/sqlite internal/application/auth
git commit -m "Auth: access tokens in the domain and in SQLite (gvt_ prefix, only a hash stored)"
```

---

### Task 2: The ownership lookup use case

**Files:**
- Modify: `internal/domain/game/values.go` (`TitleKey`), `internal/domain/game/matchkey_test.go` (`TestSameTitle`)
- Create: `internal/application/ownership/service.go`, `internal/application/ownership/service_test.go`

**Interfaces:**
- Consumes: `game.Game` (`ID()`, `Title()`, `Links()` — automatic links included, `MatchKey()`, `Copies()`), `game.Copy` (`System()`, `IsPendingKey()`, `Status`, `Kind`, `Platform`), `game.MatchKey`.
- Produces:
  - `game.TitleKey(title string) string`
  - `ownership.MaxItems = 200`, `ownership.ErrTooManyItems`
  - `type ownership.Status string` with `StatusOwned = "owned"`, `StatusPossible = "possible"`, `StatusNotOwned = "not_owned"`
  - `type ownership.Item struct { Store string; ID string; Title string }`
  - `type ownership.Copy struct { Platform string; System string; Kind game.Kind; PendingKey bool }`
  - `type ownership.Match struct { ID game.ID; Title string; Copies []Copy }`
  - `type ownership.Result struct { Status Status; Game *Match; Candidates []Match }`
  - `type ownership.Catalog interface { List(ctx) ([]*game.Game, error) }`
  - `ownership.NewService(games Catalog) *Service`, `(*Service).Check(ctx, items []Item) ([]Result, error)`

- [ ] **Step 1: Failing test for `TitleKey`.** In `internal/domain/game/matchkey_test.go`, `TestSameTitle`, extend the loop body so every pair also checks the key:

```go
		assert.Equal(t, tc.want, SameTitle(tc.a, tc.b), "%q vs %q", tc.a, tc.b)

		ka, kb := TitleKey(tc.a), TitleKey(tc.b)
		assert.Equal(t, tc.want, ka != "" && ka == kb, "keys of %q vs %q", tc.a, tc.b)
```

Run: `task go -- test ./internal/domain/game/ -run TestSameTitle`
Expected: FAIL to compile (`undefined: TitleKey`).

- [ ] **Step 2: `TitleKey`** (`internal/domain/game/values.go`, right after `SameTitle`)

```go
// TitleKey returns the words SameTitle compares, joined by single spaces: two titles are the same
// for SameTitle exactly when their keys are equal and not empty. Lookups index titles by it instead
// of comparing every title with every other.
func TitleKey(title string) string { return strings.Join(titleWords(title), " ") }
```

Run the test. Expected: PASS.

- [ ] **Step 3: Failing use-case test** (`internal/application/ownership/service_test.go`)

```go
package ownership_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/ownership"
	"gamevault/internal/domain/game"
)

// catalog is an in-memory ownership.Catalog. It has no way to save anything, which is the point:
// the lookup only reads.
type catalog []*game.Game

func (c catalog) List(context.Context) ([]*game.Game, error) { return c, nil }

var now = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

// newGame builds a game with its links and copies. Every title is invented.
func newGame(t *testing.T, title string, links game.Links, copies ...game.CopyDetails) *game.Game {
	t.Helper()

	g, err := game.New(title, now)
	require.NoError(t, err)

	if links != nil {
		_, err = g.UpdateInfo(game.Info{Title: title, Links: links}, now)
		require.NoError(t, err)
	}

	for _, d := range copies {
		_, err := g.AddCopy(d, now)
		require.NoError(t, err)
	}

	return g
}

var (
	steamLibrary = game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam"}
	steamKey     = game.CopyDetails{Kind: game.KindKey, Platform: "Steam"} // unrevealed: pending
	ps4Disc      = game.CopyDetails{Kind: game.KindPhysical, Platform: "PS4"}
	switchCart   = game.CopyDetails{Kind: game.KindPhysical, Platform: "Switch"}
	soldDisc     = game.CopyDetails{Kind: game.KindPhysical, Platform: "PS4", Status: game.StatusSold}
	giftedKey    = game.CopyDetails{Kind: game.KindKey, Platform: "Steam", Status: game.StatusGifted}
)

func TestCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	lantern := newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary, soldDisc)
	echo := newGame(t, "Echo Valley", nil, steamKey)
	echo.AddAutoLinks(game.Links{"gog": "1207664663"}, now)
	harborDisc := newGame(t, "Harbor Lights", nil, ps4Disc)
	harborKey := newGame(t, "Harbor Lights", nil, steamKey)
	copper := newGame(t, "Copper Coast", nil, switchCart)
	gone := newGame(t, "Sold Out Story", game.Links{game.LinkSteam: "7770"}, soldDisc, giftedKey)
	empty := newGame(t, "Wishlist Only", nil)
	svc := ownership.NewService(catalog{lantern, echo, harborDisc, harborKey, copper, gone, empty})

	check := func(t *testing.T, items ...ownership.Item) []ownership.Result {
		t.Helper()

		got, err := svc.Check(ctx, items)
		require.NoError(t, err)
		require.Len(t, got, len(items))

		return got
	}

	t.Run("GIVEN a store id the game is linked to", func(t *testing.T) {
		got := check(t, ownership.Item{Store: "steam", ID: "4410"})[0]

		t.Run("THEN it is owned, with its current copies only", func(t *testing.T) {
			require.Equal(t, ownership.StatusOwned, got.Status)
			assert.Equal(t, lantern.ID(), got.Game.ID)
			assert.Equal(t, "Lantern Keep", got.Game.Title)
			assert.Equal(t, []ownership.Copy{{Platform: "Steam", System: "PC", Kind: game.KindLibrary}}, got.Game.Copies)
			assert.Empty(t, got.Candidates)
		})
	})

	t.Run("GIVEN a store id Game Vault found automatically, written loosely", func(t *testing.T) {
		got := check(t, ownership.Item{Store: "GOG", ID: " 1207664663 "})[0]

		t.Run("THEN it is owned, and the key reads as pending", func(t *testing.T) {
			require.Equal(t, ownership.StatusOwned, got.Status)
			assert.Equal(t, echo.ID(), got.Game.ID)
			assert.True(t, got.Game.Copies[0].PendingKey)
			assert.Equal(t, game.KindKey, got.Game.Copies[0].Kind)
		})
	})

	t.Run("GIVEN an unknown store id with a known title", func(t *testing.T) {
		got := check(t, ownership.Item{Store: "steam", ID: "9999", Title: "LANTERN KEEP™"})[0]

		t.Run("THEN the exact title decides", func(t *testing.T) {
			require.Equal(t, ownership.StatusOwned, got.Status)
			assert.Equal(t, lantern.ID(), got.Game.ID)
		})
	})

	t.Run("GIVEN a title two games share", func(t *testing.T) {
		got := check(t, ownership.Item{Title: "Harbor Lights"})[0]

		t.Run("THEN it is possible, with both as candidates", func(t *testing.T) {
			assert.Equal(t, ownership.StatusPossible, got.Status)
			assert.Nil(t, got.Game)
			require.Len(t, got.Candidates, 2)
			assert.ElementsMatch(t, []game.ID{harborDisc.ID(), harborKey.ID()}, []game.ID{got.Candidates[0].ID, got.Candidates[1].ID})
		})
	})

	t.Run("GIVEN a title that matches only once its edition tag is ignored", func(t *testing.T) {
		got := check(t, ownership.Item{Title: "Copper Coast - Deluxe Edition"})[0]

		t.Run("THEN it is only possible, never owned", func(t *testing.T) {
			assert.Equal(t, ownership.StatusPossible, got.Status)
			require.Len(t, got.Candidates, 1)
			assert.Equal(t, copper.ID(), got.Candidates[0].ID)
		})
	})

	t.Run("GIVEN games whose copies are all gone or that have none, and titles no game has", func(t *testing.T) {
		got := check(t,
			ownership.Item{Store: "steam", ID: "7770"},
			ownership.Item{Title: "Sold Out Story"},
			ownership.Item{Title: "Wishlist Only"},
			ownership.Item{Title: "Copper Coast 2"},
			ownership.Item{},
		)

		t.Run("THEN none is owned", func(t *testing.T) {
			for i, r := range got {
				assert.Equal(t, ownership.StatusNotOwned, r.Status, "item %d", i)
				assert.Nil(t, r.Game, "item %d", i)
			}
		})
	})

	t.Run("GIVEN items in a mixed order", func(t *testing.T) {
		got := check(t, ownership.Item{Title: "Nobody Owns This"}, ownership.Item{Store: "steam", ID: "4410"}, ownership.Item{Title: "Harbor Lights"})

		t.Run("THEN each result answers its own item", func(t *testing.T) {
			assert.Equal(t,
				[]ownership.Status{ownership.StatusNotOwned, ownership.StatusOwned, ownership.StatusPossible},
				[]ownership.Status{got[0].Status, got[1].Status, got[2].Status})
		})
	})
}

func TestCheck_limits(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	games := catalog{}
	for i := range 7 {
		games = append(games, newGame(t, "Mirror Run", nil, game.CopyDetails{Kind: game.KindPhysical, Platform: fmt.Sprintf("Console %d", i)}))
	}

	svc := ownership.NewService(games)

	t.Run("GIVEN seven games with the same title", func(t *testing.T) {
		got, err := svc.Check(ctx, []ownership.Item{{Title: "Mirror Run"}})
		require.NoError(t, err)

		t.Run("THEN five candidates are listed", func(t *testing.T) {
			assert.Equal(t, ownership.StatusPossible, got[0].Status)
			assert.Len(t, got[0].Candidates, 5)
		})
	})

	t.Run("GIVEN 200 items", func(t *testing.T) {
		got, err := svc.Check(ctx, make([]ownership.Item, ownership.MaxItems))

		t.Run("THEN every one is answered", func(t *testing.T) {
			require.NoError(t, err)
			assert.Len(t, got, ownership.MaxItems)
		})
	})

	t.Run("GIVEN 201 items", func(t *testing.T) {
		_, err := svc.Check(ctx, make([]ownership.Item, ownership.MaxItems+1))

		t.Run("THEN the check is refused", func(t *testing.T) {
			assert.ErrorIs(t, err, ownership.ErrTooManyItems)
		})
	})

	t.Run("GIVEN no items", func(t *testing.T) {
		got, err := svc.Check(ctx, nil)

		t.Run("THEN the answer is empty", func(t *testing.T) {
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})
}
```

Run: `task go -- test ./internal/application/ownership/`
Expected: FAIL to compile (package `ownership` has no Go files).

- [ ] **Step 4: Write `internal/application/ownership/service.go`**

```go
// Package ownership answers "do I already own it?" for what a store page or a pasted list shows:
// store ids and titles, matched against the catalog without changing anything.
package ownership

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"gamevault/internal/domain/game"
)

// MaxItems is the most items one check may hold.
const MaxItems = 200

// maxCandidates bounds the games a possible match lists.
const maxCandidates = 5

// ErrTooManyItems means a check held more than MaxItems items.
var ErrTooManyItems = errors.New("at most 200 items can be checked at once; split the list")

// Status says whether an item is in the catalog.
type Status string

// Values of Status.
const (
	StatusOwned    Status = "owned"     // exactly one game matches
	StatusPossible Status = "possible"  // several games match, or one only once edition and store tags are ignored
	StatusNotOwned Status = "not_owned" // no game with a current copy matches
)

// Item is something seen on a store page or in a pasted list: a store's id for a game, its title,
// or both.
type Item struct {
	// Store is the store's link key ("steam", "gog"); it only counts together with ID.
	Store string

	// ID is the game's id in that store ("4410").
	ID    string
	Title string
}

// Copy summarizes one current copy of a matching game.
type Copy struct {
	Platform string
	System   string
	Kind     game.Kind

	// PendingKey reports a key not redeemed yet, which reads differently from an owned copy.
	PendingKey bool
}

// Match is a catalog game that matches an item.
type Match struct {
	ID    game.ID
	Title string

	// Copies are its current copies: sold, given away and expired ones are left out.
	Copies []Copy
}

// Result answers one item.
type Result struct {
	Status Status

	// Game is the matching game when Status is StatusOwned.
	Game *Match

	// Candidates are the matching games, by title, when Status is StatusPossible (at most five).
	Candidates []Match
}

// Catalog is the port that reads the games. A game.Repository satisfies it.
type Catalog interface {
	// List returns every game with its copies.
	List(ctx context.Context) ([]*game.Game, error)
}

// Service exposes the ownership lookup. It only reads: it saves nothing and calls no provider.
type Service struct{ games Catalog }

// NewService builds the service on the catalog's games.
func NewService(games Catalog) *Service { return &Service{games: games} }

// Check answers each item, in order: first by a store link (automatic links included) equal to
// {Store, ID}, then by the exact title (game.SameTitle), then by the title without edition and
// store tags (game.MatchKey), which can only make an item possible. Games without a current copy
// are not owned.
func (s *Service) Check(ctx context.Context, items []Item) ([]Result, error) {
	if len(items) > MaxItems {
		return nil, ErrTooManyItems
	}

	results := make([]Result, 0, len(items))
	if len(items) == 0 {
		return results, nil
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	ix := newIndex(games)
	for _, it := range items {
		results = append(results, ix.check(it))
	}

	return results, nil
}

// index finds games by store link, by SameTitle's key and by MatchKey. It is built once per check
// and holds only games with a current copy.
type index struct {
	byLink  map[string][]*game.Game
	byTitle map[string][]*game.Game
	byKey   map[string][]*game.Game
}

func newIndex(games []*game.Game) index {
	ix := index{
		byLink:  map[string][]*game.Game{},
		byTitle: map[string][]*game.Game{},
		byKey:   map[string][]*game.Game{},
	}

	for _, g := range games {
		if len(currentCopies(g)) == 0 {
			continue
		}

		for store, id := range g.Links() {
			k := linkKey(store, id)
			ix.byLink[k] = append(ix.byLink[k], g)
		}

		if k := game.TitleKey(g.Title()); k != "" {
			ix.byTitle[k] = append(ix.byTitle[k], g)
		}

		if k := g.MatchKey(); k != "" {
			ix.byKey[k] = append(ix.byKey[k], g)
		}
	}

	return ix
}

func (ix index) check(it Item) Result {
	if strings.TrimSpace(it.Store) != "" && strings.TrimSpace(it.ID) != "" {
		if r, ok := decide(ix.byLink[linkKey(it.Store, it.ID)], false); ok {
			return r
		}
	}

	if r, ok := decide(ix.byTitle[game.TitleKey(it.Title)], false); ok {
		return r
	}

	if r, ok := decide(ix.byKey[game.MatchKey(it.Title)], true); ok {
		return r
	}

	return Result{Status: StatusNotOwned}
}

// decide turns the games one rule found into a result; ok is false when it found none. A loose
// rule (tags ignored) never says owned.
func decide(games []*game.Game, loose bool) (Result, bool) {
	switch {
	case len(games) == 0:
		return Result{}, false
	case len(games) == 1 && !loose:
		m := matchOf(games[0])

		return Result{
			Status: StatusOwned,
			Game:   &m,
		}, true
	}

	sorted := slices.Clone(games)
	slices.SortFunc(sorted, func(a, b *game.Game) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Title()), strings.ToLower(b.Title())), cmp.Compare(a.ID(), b.ID()))
	})

	sorted = sorted[:min(len(sorted), maxCandidates)]
	candidates := make([]Match, 0, len(sorted))

	for _, g := range sorted {
		candidates = append(candidates, matchOf(g))
	}

	return Result{
		Status:     StatusPossible,
		Candidates: candidates,
	}, true
}

func matchOf(g *game.Game) Match {
	return Match{
		ID:     g.ID(),
		Title:  g.Title(),
		Copies: currentCopies(g),
	}
}

// currentCopies summarizes the copies the user still has: sold, given away and expired ones are gone.
func currentCopies(g *game.Game) []Copy {
	copies := g.Copies()
	out := make([]Copy, 0, len(copies))

	for _, c := range copies {
		switch c.Status {
		case game.StatusSold, game.StatusGifted, game.StatusExpired:
			continue
		}

		out = append(out, Copy{
			Platform:   c.Platform,
			System:     c.System(),
			Kind:       c.Kind,
			PendingKey: c.IsPendingKey(),
		})
	}

	return out
}

// linkKey is the index key of a store link: the store's key in lower case and the trimmed id.
func linkKey(store, id string) string {
	return strings.ToLower(strings.TrimSpace(store)) + "\x00" + strings.TrimSpace(id)
}
```

- [ ] **Step 5: Run the tests**

Run: `task go -- test ./internal/application/ownership/ ./internal/domain/game/`
Expected: `ok` for both.

- [ ] **Step 6: Lint, test, commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`), `task test` (expected: green).

```bash
git add internal/domain/game internal/application/ownership
git commit -m "Ownership lookup: store links, exact titles, then possible matches without tags"
```

---

### Task 3: The ownership RPC

**Files:**
- Create: `proto/gamevault/v1/ownership.proto`, `internal/adapters/inbound/rpc/ownership_handler.go`, `internal/adapters/inbound/rpc/ownership_test.go`
- Modify: generated code (`task generate`), `internal/adapters/inbound/rpc/server.go`, `internal/adapters/inbound/rpc/mapper.go` (`toConnectError`), `internal/adapters/inbound/rpc/server_test.go` (`newServer`, `clients`), `cmd/gamevault/main.go`, `web/src/api/client.ts` (`ownershipClient`)

**Interfaces:**
- Consumes: Task 2 `ownership.NewService`, `Check`, `Item`, `Result`, `Match`, `Copy`, `ErrTooManyItems`.
- Produces:
  - proto `OwnershipItem { string store = 1; string id = 2; string title = 3; }`, `OwnedCopy { string platform = 1; string system = 2; string kind = 3; bool pending_key = 4; }`, `OwnedGame { string id = 1; string title = 2; repeated OwnedCopy copies = 3; string path = 4; }`, `OwnershipResult { string status = 1; OwnedGame game = 2; repeated OwnedGame candidates = 3; }`, `rpc CheckOwnership(CheckOwnershipRequest { repeated OwnershipItem items = 1; }) returns (CheckOwnershipResponse { repeated OwnershipResult results = 1; })` in `service OwnershipService`
  - Go: `gamevaultv1connect.OwnershipServiceCheckOwnershipProcedure` (generated), `rpc.NewOwnershipHandler(*ownership.Service) *OwnershipHandler`, `rpc.Handlers.Ownership`
  - web: `ownershipClient` in `web/src/api/client.ts`
  - Connect JSON shape the extension reads: `{"results":[{"status":"owned","game":{"id":"…","title":"…","copies":[{"platform":"Steam","system":"PC","kind":"library","pendingKey":true}],"path":"#/game/…"},"candidates":[…]}]}` (fields at their zero value are omitted)

- [ ] **Step 1: Proto** (`proto/gamevault/v1/ownership.proto`)

```proto
syntax = "proto3";

package gamevault.v1;

option go_package = "gamevault/internal/gen/gamevault/v1;gamevaultv1";

// OwnershipItem is something seen on a store page or in a pasted list: a store's id for a game,
// a title, or both.
message OwnershipItem {
  // The store's link key ("steam", "gog") and the game's id there ("4410"); used only together.
  string store = 1;
  string id = 2;
  // The title as the page or the list writes it.
  string title = 3;
}

// OwnedCopy summarizes one current copy of a matching game.
message OwnedCopy {
  string platform = 1;
  string system = 2;
  // "library", "key" or "physical".
  string kind = 3;
  // A key not redeemed yet (unrevealed or revealed).
  bool pending_key = 4;
}

// OwnedGame is a catalog game that matches an item.
message OwnedGame {
  string id = 1;
  string title = 2;
  // Its current copies: sold, given away and expired ones are left out.
  repeated OwnedCopy copies = 3;
  // The address of its sheet in the web UI, relative: "#/game/<id>".
  string path = 4;
}

// OwnershipResult answers one item.
message OwnershipResult {
  // "owned" (one game matches), "possible" (several match, or one only once edition and store
  // tags are ignored) or "not_owned".
  string status = 1;
  // The matching game, when owned.
  OwnedGame game = 2;
  // Up to five matching games, by title, when possible.
  repeated OwnedGame candidates = 3;
}

// CheckOwnershipRequest holds at most 200 items; more is InvalidArgument.
message CheckOwnershipRequest {
  repeated OwnershipItem items = 1;
}
// CheckOwnershipResponse has one result per item, in the same order.
message CheckOwnershipResponse {
  repeated OwnershipResult results = 1;
}

// OwnershipService answers "do I already own it?": first by store link (automatic links included),
// then by exact title, then by title without edition and store tags (possible only). It only reads.
// It is the only service an access token may call.
service OwnershipService {
  rpc CheckOwnership(CheckOwnershipRequest) returns (CheckOwnershipResponse);
}
```

Run: `task generate`
Expected: no errors; `internal/gen/gamevault/v1/ownership.pb.go`, `internal/gen/gamevault/v1/gamevaultv1connect/ownership.connect.go` and `web/src/gen/gamevault/v1/ownership_pb.ts` appear.

- [ ] **Step 2: Failing RPC test** (`internal/adapters/inbound/rpc/ownership_test.go`)

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
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

func TestCheckOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title:  "Lantern Keep",
		Links:  map[string]string{"steam": "4410"},
		Copies: []*pb.CopyDetails{{Kind: pb.CopyKind_COPY_KIND_KEY, Platform: "Steam"}},
	}))
	require.NoError(t, err)

	id := created.Msg.Game.Id

	t.Run("GIVEN this computer, a trusted network", func(t *testing.T) {
		t.Run("WHEN it checks a store id, a title and an unknown title", func(t *testing.T) {
			res, err := c.ownership.CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{Items: []*pb.OwnershipItem{
				{Store: "steam", Id: "4410"},
				{Title: "lantern keep"},
				{Title: "Nobody Owns This"},
			}}))
			require.NoError(t, err)

			r := res.Msg.Results

			t.Run("THEN the first two are the game, with its pending key and the address of its sheet", func(t *testing.T) {
				require.Len(t, r, 3)

				for _, owned := range r[:2] {
					assert.Equal(t, "owned", owned.Status)
					assert.Equal(t, id, owned.Game.GetId())
					assert.Equal(t, "#/game/"+id, owned.Game.GetPath())
					require.Len(t, owned.Game.GetCopies(), 1)
					assert.Equal(t, "key", owned.Game.Copies[0].Kind)
					assert.Equal(t, "PC", owned.Game.Copies[0].System)
					assert.True(t, owned.Game.Copies[0].PendingKey)
				}
			})

			t.Run("AND the last is not owned", func(t *testing.T) {
				assert.Equal(t, "not_owned", r[2].Status)
				assert.Nil(t, r[2].Game)
			})
		})

		t.Run("WHEN it checks more than 200 items", func(t *testing.T) {
			items := make([]*pb.OwnershipItem, 201)
			for i := range items {
				items[i] = &pb.OwnershipItem{Title: "Lantern Keep"}
			}

			_, err := c.ownership.CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{Items: items}))

			t.Run("THEN it is told to split the list", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				assert.Contains(t, err.Error(), "split the list")
			})
		})
	})

	t.Run("GIVEN a request that names the server by a host name, without a token", func(t *testing.T) {
		_, err := gamevaultv1connect.NewOwnershipServiceClient(clientWith("gamevault.example:8080", nil), c.baseURL).
			CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{Items: []*pb.OwnershipItem{{Title: "Lantern Keep"}}}))

		t.Run("THEN it is refused like any other API request", func(t *testing.T) {
			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		})
	})
}
```

Run: `task go -- test ./internal/adapters/inbound/rpc/ -run TestCheckOwnership`
Expected: FAIL to compile (`c.ownership undefined`).

- [ ] **Step 3: The handler** (`internal/adapters/inbound/rpc/ownership_handler.go`)

```go
package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/ownership"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.OwnershipServiceHandler = (*OwnershipHandler)(nil)

// ownershipMaxBytes bounds a lookup's request: 200 items with long titles fit easily, and a token
// holder cannot send huge bodies.
const ownershipMaxBytes = 256 << 10

// OwnershipHandler serves the ownership lookup: the web's bundle checker and the browser
// extension's store labels. It is the only service an access token may call (see authMiddleware).
type OwnershipHandler struct{ ownership *ownership.Service }

// NewOwnershipHandler builds the handler.
func NewOwnershipHandler(o *ownership.Service) *OwnershipHandler { return &OwnershipHandler{ownership: o} }

// CheckOwnership answers each item with owned, possible or not owned, in order.
func (h *OwnershipHandler) CheckOwnership(ctx context.Context, req *connect.Request[pb.CheckOwnershipRequest]) (*connect.Response[pb.CheckOwnershipResponse], error) {
	items := make([]ownership.Item, 0, len(req.Msg.Items))
	for _, it := range req.Msg.Items {
		items = append(items, ownership.Item{
			Store: it.Store,
			ID:    it.Id,
			Title: it.Title,
		})
	}

	results, err := h.ownership.Check(ctx, items)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.CheckOwnershipResponse{Results: make([]*pb.OwnershipResult, 0, len(results))}

	for _, r := range results {
		out.Results = append(out.Results, ownershipResultToPB(r))
	}

	return connect.NewResponse(out), nil
}

func ownershipResultToPB(r ownership.Result) *pb.OwnershipResult {
	out := &pb.OwnershipResult{Status: string(r.Status)}
	if r.Game != nil {
		out.Game = ownedGameToPB(*r.Game)
	}

	for _, c := range r.Candidates {
		out.Candidates = append(out.Candidates, ownedGameToPB(c))
	}

	return out
}

func ownedGameToPB(m ownership.Match) *pb.OwnedGame {
	out := &pb.OwnedGame{
		Id:    string(m.ID),
		Title: m.Title,
		Path:  "#/game/" + string(m.ID),
	}

	for _, c := range m.Copies {
		out.Copies = append(out.Copies, &pb.OwnedCopy{
			Platform:   c.Platform,
			System:     c.System,
			Kind:       string(c.Kind),
			PendingKey: c.PendingKey,
		})
	}

	return out
}
```

- [ ] **Step 4: Mount and wire it**
  - `internal/adapters/inbound/rpc/server.go`: in `Handlers`, after `Health *HealthHandler`:

    ```go
    	// Ownership serves the "do I own it?" lookup.
    	Ownership *OwnershipHandler
    ```
    and in `NewHTTPHandler`, after the HealthService line:

    ```go
    	mux.Handle(gamevaultv1connect.NewOwnershipServiceHandler(h.Ownership, ic, connect.WithReadMaxBytes(ownershipMaxBytes)))
    ```
  - `internal/adapters/inbound/rpc/mapper.go`, `toConnectError`: add `errors.Is(err, ownership.ErrTooManyItems)` to the `CodeInvalidArgument` case (import `gamevault/internal/application/ownership`).
  - `cmd/gamevault/main.go`: import `gamevault/internal/application/ownership`; after `catalogSvc := …` add `ownershipSvc := ownership.NewService(games)`; in `rpc.Handlers{…}` add `Ownership: rpc.NewOwnershipHandler(ownershipSvc),`.
  - `internal/adapters/inbound/rpc/server_test.go`: import `gamevault/internal/application/ownership`; in `newServer`'s `rpc.Handlers{…}` add `Ownership: rpc.NewOwnershipHandler(ownership.NewService(games)),`; add the field `ownership gamevaultv1connect.OwnershipServiceClient` to `clients` and `ownership: gamevaultv1connect.NewOwnershipServiceClient(http.DefaultClient, srv.URL),` to the returned value.
  - `web/src/api/client.ts`: `import { OwnershipService } from '../gen/gamevault/v1/ownership_pb';` and `export const ownershipClient = createClient(OwnershipService, transport);` after `healthClient`.

- [ ] **Step 5: Run the RPC tests**

Run: `task go -- test ./internal/adapters/inbound/rpc/`
Expected: `ok`.

- [ ] **Step 6: Lint, test, commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`, buf lint included), `task test` (expected: green).

```bash
git add proto internal cmd web/src/gen web/src/api/client.ts
git commit -m "API: OwnershipService/CheckOwnership (read only, 200 items per request)"
```

---

### Task 4: Access tokens: use cases, RPCs and the auth middleware

**Files:**
- Create: `internal/application/auth/tokens_test.go`, `internal/adapters/inbound/rpc/tokens_test.go`
- Modify: `internal/application/auth/service.go`, `proto/gamevault/v1/auth.proto`, generated code (`task generate`), `internal/adapters/inbound/rpc/auth_handler.go`, `internal/adapters/inbound/rpc/auth_http.go`

**Interfaces:**
- Consumes: Task 1 `auth.NewAccessToken`, `auth.HashToken`, `auth.IsAccessToken`, `auth.MethodToken`, repository methods; Task 3 `gamevaultv1connect.OwnershipServiceCheckOwnershipProcedure`.
- Produces:
  - `(*appauth.Service).CreateAccessToken(ctx, name string) (auth.AccessToken, string, error)`
  - `(*appauth.Service).AccessTokens(ctx) ([]auth.AccessToken, error)`
  - `(*appauth.Service).RevokeAccessToken(ctx, id string) error`
  - `(*appauth.Service).AuthenticateToken(ctx, secret string) (auth.Principal, error)` (`auth.ErrUnauthenticated`, `auth.ErrTooManyAttempts`)
  - proto (auth.proto): `AccessToken { string id = 1; string name = 2; google.protobuf.Timestamp created_at = 3; google.protobuf.Timestamp last_used_at = 4; }`; `rpc ListAccessTokens(ListAccessTokensRequest {}) returns (ListAccessTokensResponse { repeated AccessToken tokens = 1; })`; `rpc CreateAccessToken(CreateAccessTokenRequest { string name = 1; }) returns (CreateAccessTokenResponse { AccessToken token = 1; string secret = 2; })`; `rpc RevokeAccessToken(RevokeAccessTokenRequest { string id = 1; }) returns (RevokeAccessTokenResponse {})`
  - HTTP: `Authorization: Bearer gvt_…` accepted only on `/gamevault.v1.OwnershipService/CheckOwnership`; 401 `unauthenticated` elsewhere or when unknown; 429 `resource_exhausted` while throttled

- [ ] **Step 1: Failing use-case test** (`internal/application/auth/tokens_test.go`)

```go
package auth_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
)

// plainHasher stands in for bcrypt: these tests are about tokens, and bcrypt is slow on purpose.
type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "plain:" + p, nil }

func (plainHasher) Verify(h, p string) bool { return h == "plain:"+p }

type noCerts struct{}

func (noCerts) SetCertificateValidation(auth.CertificateValidation) {}

// clock is a settable port.Clock.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newService(t *testing.T, ctx context.Context, c *clock) *appauth.Service {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return appauth.NewService(sqlite.NewAuthRepository(db), sqlite.NewSettingsRepository(db), plainHasher{}, noCerts{}, c.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func lastUsed(t *testing.T, ctx context.Context, svc *appauth.Service) time.Time {
	t.Helper()

	list, err := svc.AccessTokens(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, list)

	return list[0].LastUsedAt
}

func TestAccessTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := &clock{now: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
	svc := newService(t, ctx, c)

	t.Run("GIVEN a token created for the extension", func(t *testing.T) {
		tok, secret, err := svc.CreateAccessToken(ctx, "Extension, laptop")
		require.NoError(t, err)

		t.Run("THEN the secret is a gvt_ token, and the list shows the token by its hash only", func(t *testing.T) {
			assert.True(t, auth.IsAccessToken(secret))

			list, err := svc.AccessTokens(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, tok.ID, list[0].ID)
			assert.Equal(t, auth.HashToken(secret), list[0].Hash)
			assert.True(t, list[0].LastUsedAt.IsZero())
		})

		t.Run("WHEN it authenticates a request", func(t *testing.T) {
			p, err := svc.AuthenticateToken(ctx, secret)
			require.NoError(t, err)

			t.Run("THEN the caller is the token", func(t *testing.T) {
				assert.Equal(t, auth.MethodToken, p.Method)
				assert.Equal(t, "Extension, laptop", p.Name)
				assert.Equal(t, tok.ID, p.TokenID)
				assert.Empty(t, p.UserID)
			})

			t.Run("AND its use is recorded", func(t *testing.T) {
				assert.Equal(t, c.now, lastUsed(t, ctx, svc))
			})
		})

		t.Run("WHEN it is used again within the minute", func(t *testing.T) {
			first := c.now
			c.now = c.now.Add(30 * time.Second)
			_, err := svc.AuthenticateToken(ctx, secret)
			require.NoError(t, err)

			t.Run("THEN the last use is not written again", func(t *testing.T) {
				assert.Equal(t, first, lastUsed(t, ctx, svc))
			})
		})

		t.Run("WHEN it is used a minute or more after the recorded use", func(t *testing.T) {
			c.now = c.now.Add(31 * time.Second)
			_, err := svc.AuthenticateToken(ctx, secret)
			require.NoError(t, err)

			t.Run("THEN the last use moves", func(t *testing.T) {
				assert.Equal(t, c.now, lastUsed(t, ctx, svc))
			})
		})

		t.Run("WHEN it is revoked", func(t *testing.T) {
			require.NoError(t, svc.RevokeAccessToken(ctx, tok.ID))
			_, err := svc.AuthenticateToken(ctx, secret)

			t.Run("THEN it stops working at once", func(t *testing.T) {
				assert.ErrorIs(t, err, auth.ErrUnauthenticated)
			})

			t.Run("AND revoking it again says it is not there", func(t *testing.T) {
				assert.ErrorIs(t, svc.RevokeAccessToken(ctx, tok.ID), auth.ErrNotFound)
			})
		})
	})

	t.Run("GIVEN a name that breaks the rules", func(t *testing.T) {
		_, _, err := svc.CreateAccessToken(ctx, "  ")

		t.Run("THEN no token is created", func(t *testing.T) {
			var ve *auth.ValidationError
			assert.ErrorAs(t, err, &ve)
		})
	})
}

func TestAccessTokens_throttling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := &clock{now: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
	svc := newService(t, ctx, c)
	_, secret, err := svc.CreateAccessToken(ctx, "Extension, laptop")
	require.NoError(t, err)

	t.Run("GIVEN ten wrong tokens", func(t *testing.T) {
		for i := range 10 {
			_, err := svc.AuthenticateToken(ctx, fmt.Sprintf("gvt_guess%d", i))
			require.ErrorIs(t, err, auth.ErrUnauthenticated)
		}

		t.Run("THEN even the right token waits", func(t *testing.T) {
			_, err := svc.AuthenticateToken(ctx, secret)
			assert.ErrorIs(t, err, auth.ErrTooManyAttempts)
		})

		t.Run("AND so does signing in with a password, since both count together", func(t *testing.T) {
			_, _, err := svc.Login(ctx, auth.Request{}, "admin", "correct horse")
			assert.ErrorIs(t, err, auth.ErrTooManyAttempts)
		})

		t.Run("WHEN the window has passed", func(t *testing.T) {
			c.now = c.now.Add(16 * time.Minute)
			_, err := svc.AuthenticateToken(ctx, secret)

			t.Run("THEN the right token works again", func(t *testing.T) {
				assert.NoError(t, err)
			})
		})
	})
}
```

Run: `task go -- test ./internal/application/auth/`
Expected: FAIL to compile (`svc.CreateAccessToken undefined`).

- [ ] **Step 2: The use cases** (`internal/application/auth/service.go`)
  - In the constants block, after `touchEvery`:

    ```go
    	touchEvery      = time.Hour   // how often a session's last-seen time is written
    	tokenTouchEvery = time.Minute // how often an access token's last use is written
    ```
  - Append the methods (after `Logout`):

    ```go
    // CreateAccessToken creates an access token named name and returns it with its secret. The
    // secret is shown once and never stored: only its hash is.
    func (s *Service) CreateAccessToken(ctx context.Context, name string) (auth.AccessToken, string, error) {
    	random := make([]byte, auth.TokenBytes)
    	if _, err := rand.Read(random); err != nil {
    		return auth.AccessToken{}, "", err
    	}

    	t, secret, err := auth.NewAccessToken(name, random, s.now())
    	if err != nil {
    		return auth.AccessToken{}, "", err
    	}

    	if err := s.repo.SaveAccessToken(ctx, t); err != nil {
    		return auth.AccessToken{}, "", err
    	}

    	s.log.Info("access token created", "name", t.Name)

    	return t, secret, nil
    }

    // AccessTokens lists the access tokens, newest first.
    func (s *Service) AccessTokens(ctx context.Context) ([]auth.AccessToken, error) {
    	return s.repo.AccessTokens(ctx)
    }

    // RevokeAccessToken deletes an access token: the next request that carries it is refused.
    func (s *Service) RevokeAccessToken(ctx context.Context, id string) error {
    	if err := s.repo.DeleteAccessToken(ctx, id); err != nil {
    		return err
    	}

    	s.log.Info("access token revoked", "id", id)

    	return nil
    }

    // AuthenticateToken recognizes a caller by an access token. Unknown tokens count towards the
    // sign-in throttling, like wrong passwords, and while it is on even a valid token waits. The
    // token is looked up on every request, so revoking one takes effect at once; its last use is
    // written at most once a minute.
    func (s *Service) AuthenticateToken(ctx context.Context, secret string) (auth.Principal, error) {
    	if s.throttled() {
    		return auth.Principal{}, auth.ErrTooManyAttempts
    	}

    	t, err := s.repo.AccessTokenByHash(ctx, auth.HashToken(secret))
    	if errors.Is(err, auth.ErrNotFound) {
    		s.recordFailure()
    		s.log.Warn("unknown access token") // never log the token, nor its hash

    		return auth.Principal{}, auth.ErrUnauthenticated
    	}

    	if err != nil {
    		return auth.Principal{}, err
    	}

    	if now := s.now(); now.Sub(t.LastUsedAt) >= tokenTouchEvery {
    		if err := s.repo.TouchAccessToken(ctx, t.ID, now); err != nil {
    			s.log.Warn("recording access token use", "error", err)
    		}
    	}

    	return auth.Principal{
    		Method:  auth.MethodToken,
    		Name:    t.Name,
    		TokenID: t.ID,
    	}, nil
    }
    ```

Run: `task go -- test ./internal/application/auth/`
Expected: `ok`.

- [ ] **Step 3: Proto** (`proto/gamevault/v1/auth.proto`)
  - After the `option go_package` line: `import "google/protobuf/timestamp.proto";`
  - Before `service AuthService`, add:

    ```proto
    // AccessToken lets a program (the browser extension) look up what the user owns, and nothing
    // else. Its secret is only in CreateAccessTokenResponse: only a hash is stored.
    message AccessToken {
      string id = 1;
      // Where it is used ("Extension, laptop").
      string name = 2;
      google.protobuf.Timestamp created_at = 3;
      // Absent when it was never used. Updated at most once a minute.
      google.protobuf.Timestamp last_used_at = 4;
    }

    message ListAccessTokensRequest {}
    message ListAccessTokensResponse {
      // Newest first.
      repeated AccessToken tokens = 1;
    }

    message CreateAccessTokenRequest {
      // At most 60 characters.
      string name = 1;
    }
    message CreateAccessTokenResponse {
      AccessToken token = 1;
      // The token itself ("gvt_…"), for the Authorization header ("Bearer gvt_…"). Shown only here.
      string secret = 2;
    }

    message RevokeAccessTokenRequest {
      string id = 1;
    }
    message RevokeAccessTokenResponse {}
    ```
  - Replace the service comment and add the methods:

    ```proto
    // AuthService handles access control. GetAuthStatus, Setup, Login and Logout work without
    // authentication; the other methods (and every other service) require it. Access tokens are
    // refused here: they only reach OwnershipService.
    service AuthService {
      rpc GetAuthStatus(GetAuthStatusRequest) returns (GetAuthStatusResponse);
      rpc Setup(SetupRequest) returns (SetupResponse);
      rpc Login(LoginRequest) returns (LoginResponse);
      rpc Logout(LogoutRequest) returns (LogoutResponse);
      rpc ChangePassword(ChangePasswordRequest) returns (ChangePasswordResponse);
      rpc GetAuthSettings(GetAuthSettingsRequest) returns (GetAuthSettingsResponse);
      rpc UpdateAuthSettings(UpdateAuthSettingsRequest) returns (UpdateAuthSettingsResponse);
      rpc ListAccessTokens(ListAccessTokensRequest) returns (ListAccessTokensResponse);
      rpc CreateAccessToken(CreateAccessTokenRequest) returns (CreateAccessTokenResponse);
      // Revoking an unknown id is NotFound.
      rpc RevokeAccessToken(RevokeAccessTokenRequest) returns (RevokeAccessTokenResponse);
    }
    ```

Run: `task generate`
Expected: no errors. The Go build fails until Step 5 (`*AuthHandler` does not implement the new methods).

- [ ] **Step 4: Failing end-to-end test** (`internal/adapters/inbound/rpc/tokens_test.go`)

```go
package rpc_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// bearer returns an HTTP client that sends the access token, with that Host (empty: the server's
// address) and extra headers.
func bearer(secret, host string, headers map[string]string) *http.Client {
	h := map[string]string{"Authorization": "Bearer " + secret}
	maps.Copy(h, headers)

	return clientWith(host, h)
}

func TestAccessTokens_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)
	_, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title:  "Lantern Keep",
		Copies: []*pb.CopyDetails{{Kind: pb.CopyKind_COPY_KIND_LIBRARY, Platform: "Steam"}},
	}))
	require.NoError(t, err)

	check := func(hc *http.Client) (*connect.Response[pb.CheckOwnershipResponse], error) {
		return gamevaultv1connect.NewOwnershipServiceClient(hc, c.baseURL).CheckOwnership(ctx,
			connect.NewRequest(&pb.CheckOwnershipRequest{Items: []*pb.OwnershipItem{{Title: "Lantern Keep"}}}))
	}

	t.Run("GIVEN a token created from this computer", func(t *testing.T) {
		created, err := local.CreateAccessToken(ctx, connect.NewRequest(&pb.CreateAccessTokenRequest{Name: "Extension, laptop"}))
		require.NoError(t, err)

		secret := created.Msg.Secret
		require.True(t, strings.HasPrefix(secret, "gvt_"))

		t.Run("THEN the list shows it without its secret, never used", func(t *testing.T) {
			list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
			require.NoError(t, err)
			require.Len(t, list.Msg.Tokens, 1)
			assert.Equal(t, "Extension, laptop", list.Msg.Tokens[0].Name)
			assert.Nil(t, list.Msg.Tokens[0].LastUsedAt)
		})

		t.Run("WHEN a browser elsewhere checks ownership with it, through a host name or a proxy", func(t *testing.T) {
			for name, hc := range map[string]*http.Client{
				"host name": bearer(secret, "gamevault.example:8080", nil),
				"proxied":   bearer(secret, "", map[string]string{"X-Forwarded-For": "203.0.113.9"}),
			} {
				res, err := check(hc)
				require.NoError(t, err, name)
				assert.Equal(t, "owned", res.Msg.Results[0].Status, name)
			}

			t.Run("THEN its last use shows in the list", func(t *testing.T) {
				list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
				require.NoError(t, err)
				assert.NotNil(t, list.Msg.Tokens[0].LastUsedAt)
			})
		})

		t.Run("WHEN the token calls anything else, even from this trusted computer", func(t *testing.T) {
			hc := bearer(secret, "", nil)
			_, listErr := gamevaultv1connect.NewGameServiceClient(hc, c.baseURL).ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
			_, settingsErr := gamevaultv1connect.NewAuthServiceClient(hc, c.baseURL).GetAuthSettings(ctx, connect.NewRequest(&pb.GetAuthSettingsRequest{}))
			_, mintErr := gamevaultv1connect.NewAuthServiceClient(hc, c.baseURL).CreateAccessToken(ctx,
				connect.NewRequest(&pb.CreateAccessTokenRequest{Name: "another"}))

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/media/covers/x", nil)
			require.NoError(t, err)

			res, err := hc.Do(req)
			require.NoError(t, err)
			res.Body.Close()

			t.Run("THEN it is refused as unauthenticated", func(t *testing.T) {
				for name, err := range map[string]error{"ListGames": listErr, "GetAuthSettings": settingsErr, "CreateAccessToken": mintErr} {
					assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), name)
				}

				assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
			})

			t.Run("AND no second token was made", func(t *testing.T) {
				list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
				require.NoError(t, err)
				assert.Len(t, list.Msg.Tokens, 1)
			})
		})

		t.Run("WHEN a proxy in front of Game Vault adds its own credentials", func(t *testing.T) {
			for _, header := range []string{"Basic YWRtaW46c2VjcmV0", "Bearer eyJhbGciOiJSUzI1NiJ9.e30.sig"} {
				hc := clientWith("", map[string]string{"Authorization": header})
				_, err := gamevaultv1connect.NewGameServiceClient(hc, c.baseURL).ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))

				t.Run("THEN they are ignored and this computer is still trusted: "+header[:6], func(t *testing.T) {
					assert.NoError(t, err)
				})
			}
		})

		t.Run("WHEN it is revoked", func(t *testing.T) {
			list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
			require.NoError(t, err)

			_, err = local.RevokeAccessToken(ctx, connect.NewRequest(&pb.RevokeAccessTokenRequest{Id: list.Msg.Tokens[0].Id}))
			require.NoError(t, err)

			_, err = check(bearer(secret, "gamevault.example:8080", nil))

			t.Run("THEN the very next request is refused", func(t *testing.T) {
				assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			})

			t.Run("AND revoking it again is not found", func(t *testing.T) {
				_, err := local.RevokeAccessToken(ctx, connect.NewRequest(&pb.RevokeAccessTokenRequest{Id: list.Msg.Tokens[0].Id}))
				assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			})
		})
	})

	t.Run("GIVEN a browser elsewhere without a token", func(t *testing.T) {
		_, err := check(clientWith("gamevault.example:8080", nil))

		t.Run("THEN it must sign in, as for any other request", func(t *testing.T) {
			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		})
	})
}

func TestAccessTokens_guessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)
	guess := func(secret string) error {
		_, err := gamevaultv1connect.NewOwnershipServiceClient(bearer(secret, "gamevault.example:8080", nil), c.baseURL).
			CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{}))

		return err
	}

	t.Run("GIVEN ten wrong tokens from a host name", func(t *testing.T) {
		for i := range 10 {
			require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(guess(fmt.Sprintf("gvt_guess%d", i))))
		}

		t.Run("THEN the next attempt is told to wait", func(t *testing.T) {
			assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(guess("gvt_guess10")))
		})

		t.Run("AND signing in with a password waits too", func(t *testing.T) {
			_, err := local.Login(ctx, connect.NewRequest(&pb.LoginRequest{
				Username: "admin",
				Password: "correct horse",
			}))
			assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
		})
	})
}
```

- [ ] **Step 5: The handlers** (`internal/adapters/inbound/rpc/auth_handler.go`)
  - `requirePrincipal`: a token is never enough for the AuthService (defense in depth; the middleware already refuses it):

    ```go
    func requirePrincipal(ctx context.Context) (auth.Principal, error) {
    	p, ok := principalFrom(ctx)
    	if !ok || p.Method == auth.MethodToken {
    		return p, connect.NewError(connect.CodeUnauthenticated, auth.ErrUnauthenticated)
    	}

    	return p, nil
    }
    ```
  - Append:

    ```go
    func accessTokenToPB(t auth.AccessToken) *pb.AccessToken {
    	return &pb.AccessToken{
    		Id:         t.ID,
    		Name:       t.Name,
    		CreatedAt:  ts(t.CreatedAt),
    		LastUsedAt: optionalTS(t.LastUsedAt),
    	}
    }

    // ListAccessTokens lists the access tokens, without their secrets.
    func (h *AuthHandler) ListAccessTokens(ctx context.Context, _ *connect.Request[pb.ListAccessTokensRequest]) (*connect.Response[pb.ListAccessTokensResponse], error) {
    	if _, err := requirePrincipal(ctx); err != nil {
    		return nil, err
    	}

    	tokens, err := h.auth.AccessTokens(ctx)
    	if err != nil {
    		return nil, authError(err)
    	}

    	out := &pb.ListAccessTokensResponse{Tokens: make([]*pb.AccessToken, 0, len(tokens))}

    	for _, t := range tokens {
    		out.Tokens = append(out.Tokens, accessTokenToPB(t))
    	}

    	return connect.NewResponse(out), nil
    }

    // CreateAccessToken creates an access token and returns its secret, this once.
    func (h *AuthHandler) CreateAccessToken(ctx context.Context, req *connect.Request[pb.CreateAccessTokenRequest]) (*connect.Response[pb.CreateAccessTokenResponse], error) {
    	if _, err := requirePrincipal(ctx); err != nil {
    		return nil, err
    	}

    	t, secret, err := h.auth.CreateAccessToken(ctx, req.Msg.Name)
    	if err != nil {
    		return nil, authError(err)
    	}

    	return connect.NewResponse(&pb.CreateAccessTokenResponse{
    		Token:  accessTokenToPB(t),
    		Secret: secret,
    	}), nil
    }

    // RevokeAccessToken deletes an access token: programs using it are refused from their next request.
    func (h *AuthHandler) RevokeAccessToken(ctx context.Context, req *connect.Request[pb.RevokeAccessTokenRequest]) (*connect.Response[pb.RevokeAccessTokenResponse], error) {
    	if _, err := requirePrincipal(ctx); err != nil {
    		return nil, err
    	}

    	if err := h.auth.RevokeAccessToken(ctx, req.Msg.Id); err != nil {
    		return nil, authError(err)
    	}

    	return connect.NewResponse(&pb.RevokeAccessTokenResponse{}), nil
    }
    ```

- [ ] **Step 6: Bearer tokens in the middleware** (`internal/adapters/inbound/rpc/auth_http.go`)
  - Imports: add `"slices"` and `"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"`.
  - Add, after `isProtected`:

    ```go
    // tokenProcedures are the only paths an access token may call: it looks up what the user owns
    // and nothing else.
    var tokenProcedures = []string{gamevaultv1connect.OwnershipServiceCheckOwnershipProcedure}

    // accessToken returns the Game Vault access token of an "Authorization: Bearer gvt_…" header.
    // Other credentials (a proxy's Basic or Bearer header) are not Game Vault's and are ignored.
    func accessToken(r *http.Request) (string, bool) {
    	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
    	token = strings.TrimSpace(token)

    	return token, ok && strings.EqualFold(scheme, "Bearer") && auth.IsAccessToken(token)
    }

    // serveWithToken lets an access token call the ownership lookup and nothing else. The token alone
    // identifies the caller: neither a session cookie nor a trusted network adds to it, and the
    // proxy and host name rules are unaffected (a token never makes a request trusted).
    func serveWithToken(svc *appauth.Service, next http.Handler, w http.ResponseWriter, r *http.Request, token string) {
    	path := r.URL.Path
    	if !slices.Contains(tokenProcedures, path) {
    		denyUnauthenticated(w, path)
    		return
    	}

    	req := authRequest(r)
    	req.SessionToken = ""
    	ctx := context.WithValue(r.Context(), ctxRequest, req)

    	p, err := svc.AuthenticateToken(ctx, token)
    	switch {
    	case err == nil:
    		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxPrincipal, p)))
    	case errors.Is(err, auth.ErrTooManyAttempts):
    		denyThrottled(w)
    	case errors.Is(err, auth.ErrUnauthenticated):
    		denyUnauthenticated(w, path)
    	default:
    		http.Error(w, "authentication failed", http.StatusInternalServerError)
    	}
    }

    func denyThrottled(w http.ResponseWriter) {
    	w.Header().Set("Content-Type", "application/json")
    	w.WriteHeader(http.StatusTooManyRequests)
    	w.Write([]byte(`{"code":"resource_exhausted","message":"too many failed attempts; wait a few minutes"}`))
    }
    ```
  - In `authMiddleware`, as the first statement of the handler func (before `path := r.URL.Path`):

    ```go
    		if token, ok := accessToken(r); ok {
    			serveWithToken(svc, next, w, r, token)
    			return
    		}
    ```

- [ ] **Step 7: Run the tests**

Run: `task go -- test ./internal/adapters/inbound/rpc/ ./internal/application/auth/`
Expected: `ok` for both (`TestAccessControl` unchanged and green).

- [ ] **Step 8: Lint, test, commit**

Run: `task lint:fix`, `task lint` (expected `0 issues.`), `task test` (expected: green; the web type check passes with the regenerated `auth_pb.ts`).

```bash
git add proto internal web/src/gen
git commit -m "Auth: access tokens for the ownership lookup only (Bearer gvt_, throttled, last used)"
```

---
### Task 5: Access tokens in System → Security

**Files:**
- Create: `web/src/features/system/AccessTokens.tsx`
- Modify: `web/src/features/system/SecurityCard.tsx`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 4 `authClient.listAccessTokens({})` → `{ tokens: AccessToken[] }`, `authClient.createAccessToken({ name })` → `{ token, secret }`, `authClient.revokeAccessToken({ id })`; `toDate` from `lib/model.ts`; `useFormatters().dateTime`.
- Produces: `AccessTokens` (default export, no props), rendered at the end of `SecurityCard`.

- [ ] **Step 1: Texts.** Add under `security` in both locales (`task i18n` checks they match):

`en.json`:

```json
"tokens": {
  "title": "Access tokens",
  "intro": "An access token lets the browser extension show what you own on store pages from a browser that is not on a trusted network. It can only look up what you own: it cannot read or change anything else.",
  "name": "Name",
  "namePlaceholder": "Extension, laptop",
  "create": "Create token",
  "created": "Copy this token now and paste it in the extension: it will not be shown again.",
  "secret": "New access token",
  "copy": "Copy",
  "copied": "Copied",
  "none": "No access tokens yet.",
  "createdAt": "Created {{date}}",
  "lastUsed": "last used {{date}}",
  "neverUsed": "never used",
  "revoke": "Revoke",
  "confirmRevoke": "Revoke the token \"{{name}}\"? Whatever uses it stops working at once."
}
```

`es.json`:

```json
"tokens": {
  "title": "Tokens de acceso",
  "intro": "Un token de acceso permite que la extensión del navegador muestre lo que tienes en las páginas de las tiendas desde un navegador que no está en una red de confianza. Solo sirve para consultar lo que tienes: no puede leer ni cambiar nada más.",
  "name": "Nombre",
  "namePlaceholder": "Extensión, portátil",
  "create": "Crear token",
  "created": "Copia este token ahora y pégalo en la extensión: no se volverá a mostrar.",
  "secret": "Nuevo token de acceso",
  "copy": "Copiar",
  "copied": "Copiado",
  "none": "Todavía no hay tokens de acceso.",
  "createdAt": "Creado el {{date}}",
  "lastUsed": "último uso el {{date}}",
  "neverUsed": "sin usar",
  "revoke": "Revocar",
  "confirmRevoke": "¿Revocar el token \"{{name}}\"? Lo que lo use dejará de funcionar al momento."
}
```

- [ ] **Step 2: The section** (`web/src/features/system/AccessTokens.tsx`)

```tsx
import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { authClient, errorMessage } from '../../api/client';
import { Alert, useFormatters } from '../../components/ui';
import type { AccessToken } from '../../gen/gamevault/v1/auth_pb';
import { toDate } from '../../lib/model';

/** Access tokens for the browser extension: create one (its secret is shown once), list, revoke. */
export default function AccessTokens() {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const [tokens, setTokens] = useState<AccessToken[]>([]);
  const [name, setName] = useState('');
  const [secret, setSecret] = useState('');
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setTokens((await authClient.listAccessTokens({})).tokens);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const res = await authClient.createAccessToken({ name });
      setSecret(res.secret);
      setCopied(false);
      setName('');
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (tok: AccessToken) => {
    if (!confirm(t('security.tokens.confirmRevoke', { name: tok.name }))) return;
    try {
      await authClient.revokeAccessToken({ id: tok.id });
      await load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      /* no clipboard (plain http on the network): the field selects itself on focus for a manual copy */
    }
  };

  return (
    <>
      <h3>{t('security.tokens.title')}</h3>
      <p className="muted small">{t('security.tokens.intro')}</p>
      {error && <Alert tone="error">{error}</Alert>}
      {secret && (
        <div className="token-secret">
          <Alert tone="warn">{t('security.tokens.created')}</Alert>
          <div className="token-secret-row">
            <input readOnly value={secret} aria-label={t('security.tokens.secret')} spellCheck={false}
              onFocus={(e) => e.currentTarget.select()} />
            <button type="button" onClick={copy}>{copied ? t('security.tokens.copied') : t('security.tokens.copy')}</button>
          </div>
        </div>
      )}
      {tokens.length === 0 ? <p className="muted small">{t('security.tokens.none')}</p> : (
        <ul className="token-list">
          {tokens.map((tok) => {
            const used = toDate(tok.lastUsedAt);
            return (
              <li key={tok.id}>
                <span className="token-name">{tok.name}</span>
                <span className="muted small">
                  {t('security.tokens.createdAt', { date: fmt.dateTime(toDate(tok.createdAt)) })}
                  {' · '}
                  {used ? t('security.tokens.lastUsed', { date: fmt.dateTime(used) }) : t('security.tokens.neverUsed')}
                </span>
                <button type="button" className="danger" onClick={() => revoke(tok)}>{t('security.tokens.revoke')}</button>
              </li>
            );
          })}
        </ul>
      )}
      <form className="token-form" onSubmit={create}>
        <label>
          {t('security.tokens.name')}
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('security.tokens.namePlaceholder')}
            maxLength={60} required />
        </label>
        <button type="submit" className="primary" disabled={busy || !name.trim()}>{t('security.tokens.create')}</button>
      </form>
    </>
  );
}
```

- [ ] **Step 3: Show it** (`web/src/features/system/SecurityCard.tsx`): `import AccessTokens from './AccessTokens';` and, right after the `<UserForm … />` element, `<AccessTokens />`.

- [ ] **Step 4: Styles** (`web/src/styles.css`, after the `.security-form` rules; search for `security-form`)

```css
/* ---------- Access tokens (System → Security) ---------- */
.token-list { list-style: none; margin: 8px 0; padding: 0; }
.token-list li { display: flex; align-items: center; gap: 4px 12px; flex-wrap: wrap; padding: 10px 0; border-bottom: 1px solid var(--border); }
.token-name { font-weight: 650; }
.token-list li button { margin-left: auto; }
.token-form { display: flex; gap: 10px; align-items: end; flex-wrap: wrap; margin-top: 12px; }
.token-form label { display: flex; flex-direction: column; gap: 5px; flex: 1 1 220px; font-size: 13px; color: var(--muted); font-weight: 550; }
.token-form input { font-size: 15px; }
.token-secret-row { display: flex; gap: 8px; margin-top: 8px; }
.token-secret-row input { flex: 1; min-width: 0; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 14px; }
```

- [ ] **Step 5: Type check and translations**

Run: `task test`
Expected: green (TypeScript type check, `translations OK`).

- [ ] **Step 6: Browser check.** `task test-server`, then open http://127.0.0.1:8093/?v=<a new number>#/system in the browser pane:
  - System → Security shows **Access tokens** with "No access tokens yet."; create "Extension, laptop": the token appears once with **Copy**, and the list shows it "never used";
  - `curl -s -X POST -H 'Content-Type: application/json' -H "Authorization: Bearer <the token>" -H 'Host: gamevault.example:8093' -d '{"items":[{"title":"x"}]}' http://127.0.0.1:8093/gamevault.v1.OwnershipService/CheckOwnership` answers `{"results":[{"status":"not_owned"}]}` (or owned, depending on the copy's catalog: do not paste the result anywhere), and after a reload the list says "last used …";
  - the same `curl` with `gvt_bogus` answers HTTP 401 `{"code":"unauthenticated",…}`; with the real token against `/gamevault.v1.GameService/ListGames` also 401;
  - **Revoke** asks for confirmation and removes it; the `curl` with the revoked token is 401;
  - `resize_window` preset `mobile`, reload: the section wraps without horizontal scroll; reset with preset `desktop`. Then `task test-server:stop`.

- [ ] **Step 7: Commit**

```bash
git add web/src
git commit -m "System → Security: access tokens (create once-shown, last used, revoke)"
```

---

### Task 6: "Do I own it?" in the library, and `#/game/<id>`

**Files:**
- Create: `web/src/lib/ownership.ts`, `web/src/lib/gameRoute.ts`, `web/tests/ownership.test.mjs`, `web/tests/gameRoute.test.mjs`, `web/src/features/library/OwnershipDialog.tsx`
- Modify: `web/src/features/library/LibraryPage.tsx`, `web/src/components/ui.tsx` (`Modal` `phoneFull`), `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 3 `ownershipClient.checkOwnership({ items: [{ title }] })` → `{ results: OwnershipResult[] }` (`status`, `game?`, `candidates`; games with `id`, `title`, `copies` (`platform`, `system`, `kind`, `pendingKey`), `path`); `PlatformBadge` (`platform`, `holding`, `kind`, `full`); `CopyKind`.
- Produces:
  - `web/src/lib/ownership.ts`: `MAX_CHECK_ITEMS = 200`; `cleanTitles(text: string): CleanedList` (`{ titles: string[]; skipped: number }`); `chunk<T>(items: T[], size?: number): T[][]`; `toRows(titles: string[], results: ResultLike[]): Row[]`; `summarize(rows: Row[]): { owned: number; possible: number; total: number }`; `notOwnedText(rows: Row[]): string`; types `CopyLike`, `GameLike`, `ResultLike`, `RowStatus`, `Row`
  - `web/src/lib/gameRoute.ts`: `gameIdFromHash(hash: string): string`
  - `OwnershipDialog({ onClose })`; `Modal` prop `phoneFull?: boolean`

- [ ] **Step 1: Failing Node tests**

`web/tests/ownership.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_CHECK_ITEMS, chunk, cleanTitles, notOwnedText, summarize, toRows } from '../src/lib/ownership.ts';

test('bullets, numbering, prices, discounts and currency signs go; the title stays whole', () => {
  const { titles, skipped } = cleanTitles([
    '• Lantern Keep',
    '- Echo Valley $19.99',
    '1. Harbor Lights',
    '2) Copper Coast — 4,99 €',
    '(3) Mirror Run -75% $4.99',
    '#4 Night Ferry',
    '€9.99 Paper Comet',
    'Glass Orchard\t$14.99\tSteam',
    '',
    '   ',
  ].join('\n'));
  assert.deepEqual(titles, ['Lantern Keep', 'Echo Valley', 'Harbor Lights', 'Copper Coast', 'Mirror Run', 'Night Ferry', 'Paper Comet', 'Glass Orchard']);
  assert.equal(skipped, 0);
});

test('words that belong to a title are kept', () => {
  const lines = [
    'DLC Hunters',
    'Soundtrack Hero',
    '1983 Revival: Night Shift',
    '7 Nights at Sea',
    'The Price of Silence',
    'Free Runner 2',
    'Ca$h Rally',
    'Mirror Run 2: 50% More Mirrors',
    'Year 2049: 3.5 Moons',
  ];
  assert.deepEqual(cleanTitles(lines.join('\n')).titles, lines);
});

test('add-ons are skipped and counted; duplicates, blank and overlong lines are dropped', () => {
  const { titles, skipped } = cleanTitles([
    'Lantern Keep',
    'lantern  keep',
    'Lantern Keep [DLC]',
    'Lantern Keep - Original Soundtrack',
    'DLC: Lantern Keep Map Pack',
    'Lantern Keep Soundtrack',
    'Lantern Keep OST',
    'Echo Valley (Soundtrack)',
    'Echo Valley',
    'x'.repeat(201),
    '',
  ].join('\r\n'));
  assert.deepEqual(titles, ['Lantern Keep', 'Echo Valley']);
  assert.equal(skipped, 6);
});

test('a long list is split into requests of at most 200 titles, in order', () => {
  const titles = Array.from({ length: 450 }, (_, i) => `Game ${i}`);
  const parts = chunk(titles);
  assert.deepEqual(parts.map((p) => p.length), [MAX_CHECK_ITEMS, MAX_CHECK_ITEMS, 50]);
  assert.deepEqual(parts.flat(), titles);
  assert.deepEqual(chunk([]), []);
});

test('rows follow the pasted order and count what is owned', () => {
  const game = (id) => ({ id, title: id, copies: [], path: `#/game/${id}` });
  const rows = toRows(['A', 'B', 'C', 'D'], [
    { status: 'owned', game: game('a'), candidates: [] },
    { status: 'possible', candidates: [game('b1'), game('b2')] },
    { status: 'not_owned', candidates: [] },
  ]);
  assert.deepEqual(rows.map((r) => r.status), ['owned', 'possible', 'not_owned', 'not_owned']);
  assert.equal(rows[0].game.id, 'a');
  assert.equal(rows[1].candidates.length, 2);
  assert.deepEqual(summarize(rows), { owned: 1, possible: 1, total: 4 });
  assert.equal(notOwnedText(rows), 'C\nD', 'possible ones may be owned: they are not copied');
});

test('an owned answer without a game, or an unknown status, is not owned', () => {
  const rows = toRows(['A', 'B'], [{ status: 'owned', candidates: [] }, { status: 'maybe', candidates: [] }]);
  assert.deepEqual(rows.map((r) => r.status), ['not_owned', 'not_owned']);
});
```

`web/tests/gameRoute.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { gameIdFromHash } from '../src/lib/gameRoute.ts';

test('a game address names its game; any other address names none', () => {
  assert.equal(gameIdFromHash('#/game/0192c3a4-5b6c-7d8e-9f00-112233445566'), '0192c3a4-5b6c-7d8e-9f00-112233445566');
  assert.equal(gameIdFromHash('#/game/abc/'), 'abc');
  assert.equal(gameIdFromHash('#/library'), '');
  assert.equal(gameIdFromHash('#/game/'), '');
  assert.equal(gameIdFromHash('#/game/<script>'), '');
  assert.equal(gameIdFromHash('#/game/a/b'), '');
  assert.equal(gameIdFromHash(''), '');
});
```

Run: `docker run --rm -v "$PWD:/src" -w /src gamevault-toolchain:446777a3814f node --test --test-timeout=10000 web/tests/ownership.test.mjs web/tests/gameRoute.test.mjs`
Expected: FAIL (`Cannot find module …/src/lib/ownership.ts`).

- [ ] **Step 2: `web/src/lib/gameRoute.ts`**

```ts
// The address of a game's sheet, "#/game/<id>": the bundle checker and the browser extension link
// to it, and the library opens the sheet it names. Plain functions, Node-tested.

const GAME_HASH = /^#\/game\/([A-Za-z0-9-]{1,64})\/?$/;

/** The game id a "#/game/<id>" address names, or '' for any other address. */
export function gameIdFromHash(hash: string): string {
  return GAME_HASH.exec(hash)?.[1] ?? '';
}
```

- [ ] **Step 3: `web/src/lib/ownership.ts`**

```ts
// The bundle checker's pure parts: a pasted list cleaned into titles, split into requests, and the
// answers turned into rows. Structural types only, so Node tests import this file as it is.

/** Most titles one request may hold (the server's limit). */
export const MAX_CHECK_ITEMS = 200;

/** Longest line taken as a title: longer lines are prose, not titles. */
const MAX_TITLE = 200;

const BULLET = /^(?:[-–—*•·●▪►>+]\s*)+/;
const NUMBERING = /^(?:\(\d{1,3}\)|\d{1,3}[.)]|#\d{1,3}[.):]?)\s+/;
const PRICE = String.raw`(?:(?:US|CA|AU|NZ)?[$€£¥]\s?\d+(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?\s?(?:[$€£¥]|USD|EUR|GBP))`;
const LEADING_PRICE = new RegExp(String.raw`^${PRICE}\s+`, 'i');
const TRAILING_PRICE = new RegExp(String.raw`\s*(?:[-–—|:]\s*)?${PRICE}$`, 'i');
const TRAILING_DISCOUNT = /\s+-\d{1,3}\s?%$/;
const LONE_CURRENCY = /(^|\s)[$€£¥](?=\s|$)/g;
const DANGLING = /[\s\-–—|:,;]+$/;

/** Lines marked as add-ons rather than games: "[DLC]", "(Soundtrack)", "DLC: …", "… - Soundtrack",
 *  "… Soundtrack", "… OST". A marker word elsewhere ("DLC Hunters", "Soundtrack Hero") is part of a
 *  title. */
const ADD_ON = [
  /[([]\s*(?:dlc|(?:original\s+)?soundtrack|ost)\s*[)\]]/i,
  /^(?:dlc|soundtrack|ost)\s*[:\-–—]\s*/i,
  /\s[-–—:]\s*(?:dlc|(?:original\s+)?soundtrack|ost)$/i,
  /\s(?:original\s+)?soundtrack$/i,
  /\s(?:DLC|OST)$/,
];

export interface CleanedList {
  titles: string[];
  /** Lines left out because they name an add-on (DLC, soundtrack), not a game. */
  skipped: number;
}

function cleanLine(line: string): string {
  let s = line.replace(/\s+/g, ' ').trim().replace(BULLET, '').replace(NUMBERING, '').replace(LEADING_PRICE, '');
  // "Mirror Run -75% $4.99": a price, then a discount, then whatever separator they leave.
  for (let before = ''; before !== s;) {
    before = s;
    s = s.replace(TRAILING_PRICE, '').replace(TRAILING_DISCOUNT, '').replace(DANGLING, '').trim();
  }
  return s.replace(LONE_CURRENCY, '$1').replace(/\s+/g, ' ').trim();
}

/** Cleans pasted text into one title per line: bullets, numbering, prices, discounts and currency
 *  signs go; add-on lines are skipped (and counted); blank, overlong and duplicate lines are
 *  dropped. A line copied from a table keeps its first cell. */
export function cleanTitles(text: string): CleanedList {
  const titles: string[] = [];
  const seen = new Set<string>();
  let skipped = 0;
  for (const raw of text.split(/\r?\n/)) {
    const title = cleanLine(raw.split('\t').find((cell) => cell.trim()) ?? '');
    if (!title || title.length > MAX_TITLE) continue;
    if (ADD_ON.some((re) => re.test(title))) {
      skipped++;
      continue;
    }
    const key = title.normalize('NFKC').toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    titles.push(title);
  }
  return { titles, skipped };
}

/** Splits items into requests of at most `size`, in order. */
export function chunk<T>(items: T[], size = MAX_CHECK_ITEMS): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) out.push(items.slice(i, i + size));
  return out;
}

export interface CopyLike { platform: string; system: string; kind: string; pendingKey: boolean }
export interface GameLike { id: string; title: string; copies: CopyLike[]; path: string }
export interface ResultLike { status: string; game?: GameLike; candidates: GameLike[] }
export type RowStatus = 'owned' | 'possible' | 'not_owned';
export interface Row { title: string; status: RowStatus; game?: GameLike; candidates: GameLike[] }

/** One row per title, in the order pasted. A missing or unknown answer counts as not owned. */
export function toRows(titles: string[], results: ResultLike[]): Row[] {
  return titles.map((title, i) => {
    const r = results[i];
    if (r?.status === 'owned' && r.game) return { title, status: 'owned', game: r.game, candidates: [] };
    if (r?.status === 'possible') return { title, status: 'possible', candidates: r.candidates };
    return { title, status: 'not_owned', candidates: [] };
  });
}

/** "You own N of M": owned rows of all rows; possible ones are counted apart. */
export function summarize(rows: Row[]): { owned: number; possible: number; total: number } {
  return {
    owned: rows.filter((r) => r.status === 'owned').length,
    possible: rows.filter((r) => r.status === 'possible').length,
    total: rows.length,
  };
}

/** The titles not owned, one per line, for the clipboard. Possible ones may be owned: left out. */
export function notOwnedText(rows: Row[]): string {
  return rows.filter((r) => r.status === 'not_owned').map((r) => r.title).join('\n');
}
```

Run the Node tests. Expected: PASS.

- [ ] **Step 4: Texts.** Add to both locales: `library.checkOwnership` and a top-level `ownership` section (`task i18n` checks they match).

`en.json`:

```json
"checkOwnership": "Do I own it?",
```
(inside `library`), and

```json
"ownership": {
  "title": "Do I own it?",
  "intro": "Paste a list of titles, one per line: a bundle's games, a sale, a wishlist. Bullets, numbering, prices and DLC or soundtrack lines are cleaned up. Nothing is saved.",
  "placeholder": "Paste titles here…",
  "check": "Check",
  "checking": "Checking…",
  "results": "Results",
  "nothing": "Nothing to check: paste some titles first.",
  "skipped_one": "{{count}} line skipped as DLC or soundtrack.",
  "skipped_other": "{{count}} lines skipped as DLC or soundtrack.",
  "summary": "You own {{owned}} of {{total}}",
  "possibleCount_one": "{{count}} possible",
  "possibleCount_other": "{{count}} possible",
  "copyMissing": "Copy the ones I don't own",
  "copied": "Copied",
  "maybe": "Maybe:",
  "pendingKey": "key not redeemed",
  "status": {
    "owned": "Owned",
    "possible": "Possible",
    "not_owned": "Not owned"
  }
}
```

`es.json`:

```json
"checkOwnership": "¿Lo tengo?",
```
and

```json
"ownership": {
  "title": "¿Lo tengo?",
  "intro": "Pega una lista de títulos, uno por línea: los juegos de un bundle, unas ofertas, una lista de deseos. Se limpian viñetas, numeración, precios y líneas de DLC o banda sonora. No se guarda nada.",
  "placeholder": "Pega los títulos aquí…",
  "check": "Comprobar",
  "checking": "Comprobando…",
  "results": "Resultados",
  "nothing": "Nada que comprobar: pega antes algunos títulos.",
  "skipped_one": "{{count}} línea omitida por ser DLC o banda sonora.",
  "skipped_other": "{{count}} líneas omitidas por ser DLC o banda sonora.",
  "summary": "Tienes {{owned}} de {{total}}",
  "possibleCount_one": "{{count}} posible",
  "possibleCount_other": "{{count}} posibles",
  "copyMissing": "Copiar los que no tengo",
  "copied": "Copiado",
  "maybe": "Quizá:",
  "pendingKey": "clave sin canjear",
  "status": {
    "owned": "Lo tienes",
    "possible": "Posible",
    "not_owned": "No lo tienes"
  }
}
```

- [ ] **Step 5: Full screen on phones** (`web/src/components/ui.tsx`, `Modal`): add `phoneFull?: boolean` to the props and use `className={`modal ${props.wide ? 'wide' : ''} ${props.phoneFull ? 'phone-full' : ''}`}`.

- [ ] **Step 6: The dialog** (`web/src/features/library/OwnershipDialog.tsx`)

```tsx
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, ownershipClient } from '../../api/client';
import { PlatformBadge } from '../../components/PlatformBadge';
import { Alert, Modal } from '../../components/ui';
import { CopyKind } from '../../lib/model';
import { chunk, cleanTitles, notOwnedText, summarize, toRows, type GameLike, type ResultLike, type Row } from '../../lib/ownership';

const KINDS: Record<string, CopyKind> = { key: CopyKind.KEY, library: CopyKind.LIBRARY, physical: CopyKind.PHYSICAL };
const MARKS: Record<Row['status'], string> = { owned: '✓', possible: '?', not_owned: '–' };

/** "Do I own it?": checks a pasted list of titles (a bundle, a sale) against the catalog. Nothing is
 *  saved. On phones it is full screen, with the list above the results. */
export default function OwnershipDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const [text, setText] = useState('');
  const [rows, setRows] = useState<Row[] | null>(null);
  const [skipped, setSkipped] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  const check = async (e: FormEvent) => {
    e.preventDefault();
    const cleaned = cleanTitles(text);
    setSkipped(cleaned.skipped);
    setError('');
    setCopied(false);
    if (!cleaned.titles.length) {
      setRows(null);
      setError(t('ownership.nothing'));
      return;
    }
    setBusy(true);
    try {
      const results: ResultLike[] = [];
      for (const part of chunk(cleaned.titles)) {
        results.push(...(await ownershipClient.checkOwnership({ items: part.map((title) => ({ title })) })).results);
      }
      setRows(toRows(cleaned.titles, results));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const copyMissing = async () => {
    if (!rows) return;
    try {
      await navigator.clipboard.writeText(notOwnedText(rows));
      setCopied(true);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const sum = rows ? summarize(rows) : null;
  return (
    <Modal title={t('ownership.title')} onClose={onClose} wide phoneFull
      footer={<>
        <span className="spacer" />
        <button type="button" onClick={onClose}>{t('common.close')}</button>
        <button type="submit" form="ownership-form" className="primary" disabled={busy || !text.trim()}>
          {busy ? t('ownership.checking') : t('ownership.check')}
        </button>
      </>}>
      <div className="ownership">
        <form id="ownership-form" className="ownership-input" onSubmit={check}>
          <label>
            <span className="muted small">{t('ownership.intro')}</span>
            <textarea value={text} onChange={(e) => setText(e.target.value)} placeholder={t('ownership.placeholder')}
              spellCheck={false} autoFocus />
          </label>
        </form>
        <section className="ownership-results" aria-live="polite" aria-label={t('ownership.results')}>
          {error && <Alert tone="error">{error}</Alert>}
          {skipped > 0 && <p className="muted small">{t('ownership.skipped', { count: skipped })}</p>}
          {rows && sum && (
            <>
              <div className="ownership-summary">
                <span>{t('ownership.summary', { owned: sum.owned, total: sum.total })}</span>
                {sum.possible > 0 && <span className="muted">{t('ownership.possibleCount', { count: sum.possible })}</span>}
                <span className="spacer" />
                {sum.owned + sum.possible < sum.total && (
                  <button type="button" onClick={copyMissing}>{copied ? t('ownership.copied') : t('ownership.copyMissing')}</button>
                )}
              </div>
              <ul className="ownership-rows">
                {rows.map((r, i) => <ResultRow key={i} row={r} onOpen={onClose} />)}
              </ul>
            </>
          )}
        </section>
      </div>
    </Modal>
  );
}

function ResultRow({ row, onOpen }: { row: Row; onOpen: () => void }) {
  const { t } = useTranslation();
  const label = t(`ownership.status.${row.status}`);
  return (
    <li className={row.status}>
      <span className="state" aria-hidden="true">{MARKS[row.status]}</span>
      <span className="ownership-title">{row.title}<span className="sr-only"> · {label}</span></span>
      {row.status === 'owned' && row.game && (
        <span className="ownership-detail">
          <GameLink game={row.game} onOpen={onOpen} />
          {row.game.copies.map((c, i) => (
            <PlatformBadge key={i} platform={c.platform} holding={c.pendingKey ? 'key' : 'owned'} kind={KINDS[c.kind]} full />
          ))}
          {row.game.copies.some((c) => c.pendingKey) && <span>{t('ownership.pendingKey')}</span>}
        </span>
      )}
      {row.status === 'possible' && (
        <span className="ownership-detail">
          {t('ownership.maybe')}
          {row.candidates.map((g) => <GameLink key={g.id} game={g} onOpen={onOpen} />)}
        </span>
      )}
      {row.status === 'not_owned' && <span className="ownership-detail">{label}</span>}
    </li>
  );
}

/** A link to a game's sheet ("#/game/<id>"): the library opens it once the dialog closes. */
function GameLink({ game, onOpen }: { game: GameLike; onOpen: () => void }) {
  const href = game.path.startsWith('#/game/') ? game.path : '#/library';
  return <a href={href} onClick={onOpen}>{game.title}</a>;
}
```

- [ ] **Step 7: The library page** (`web/src/features/library/LibraryPage.tsx`)
  - Imports: `import OwnershipDialog from './OwnershipDialog';` and `import { gameIdFromHash } from '../../lib/gameRoute';`.
  - State, next to `creating`: `const [checking, setChecking] = useState(false);`
  - After the existing effects (before `return`), follow game addresses:

    ```tsx
    // "#/game/<id>" (links from the bundle checker and the browser extension) opens that game's
    // sheet, now and whenever the address changes.
    useEffect(() => {
      const follow = () => {
        const id = gameIdFromHash(window.location.hash);
        if (id) setOpen({ id, system: '' });
      };
      follow();
      window.addEventListener('hashchange', follow);
      return () => window.removeEventListener('hashchange', follow);
    }, []);
    ```
  - Closing the sheet leaves the game address, so a reload does not reopen it: in `<GameDetail … onClose={…}>` use

    ```tsx
    onClose={() => {
      setOpen(null);
      if (gameIdFromHash(window.location.hash)) history.replaceState(null, '', '#/library');
    }}
    ```
  - In the header, before the **Add game** button:

    ```tsx
    <button onClick={() => setChecking(true)} title={t('library.checkOwnership')} aria-label={t('library.checkOwnership')}>
      <Icon name="check" size={18} /><span className="hide-narrow">{t('library.checkOwnership')}</span>
    </button>
    ```
  - Next to the `NewGameDialog` render: `{checking && <OwnershipDialog onClose={() => setChecking(false)} />}`.

- [ ] **Step 8: Styles** (`web/src/styles.css`, after the `.modal` rules)

```css
/* ---------- Bundle checker ("Do I own it?") ---------- */
.ownership { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr); gap: 20px; align-items: start; }
.ownership-input label { display: flex; flex-direction: column; gap: 8px; }
.ownership-input textarea { width: 100%; min-height: 340px; font-size: 15px; resize: vertical; }
.ownership-summary { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 8px; font-weight: 700; }
.ownership-rows { list-style: none; margin: 0; padding: 0; }
.ownership-rows li { display: grid; grid-template-columns: 20px minmax(0, 1fr); gap: 2px 8px; padding: 9px 0; border-bottom: 1px solid var(--border); }
.ownership-rows .state { grid-row: span 2; font-weight: 800; }
.ownership-rows .owned .state { color: var(--ok); }
.ownership-rows .possible .state { color: var(--warn); }
.ownership-rows .not_owned .state { color: var(--muted); }
.ownership-title { font-weight: 600; overflow-wrap: anywhere; }
.ownership-detail { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); }

@media (max-width: 640px) {
  .ownership { grid-template-columns: 1fr; }
  .ownership-input textarea { min-height: 140px; }
  .modal.phone-full { height: 100dvh; max-height: 100dvh; border-radius: 0; }
}
```

- [ ] **Step 9: Tests and type check**

Run: `task test`
Expected: green (Node tests, TypeScript, `translations OK`).

- [ ] **Step 10: Browser check.** `task test-server`, http://127.0.0.1:8093/?v=<a new number> in the browser pane:
  - **Do I own it?** opens the dialog; paste a few lines mixing titles from the copy's library (pick them in the library first; do not write them down anywhere), invented ones ("Nobody Owns This"), a "- Original Soundtrack" line, a bullet and a price: results show owned (badges, pending key), possible and not owned, "You own N of M", the skipped line count; **Copy the ones I don't own** copies the not-owned titles;
  - clicking an owned game's link closes the dialog and opens its sheet; closing the sheet leaves `#/library`; loading `http://127.0.0.1:8093/?v=<n>#/game/<an id from a sheet's address>` opens that sheet directly;
  - `resize_window` preset `mobile`, reload: the dialog is full screen, the text area above the results, no horizontal scroll; reset with preset `desktop`. Then `task test-server:stop`.

- [ ] **Step 11: Commit**

```bash
git add web/src web/tests
git commit -m "Library: \"Do I own it?\" bundle checker and #/game/<id> addresses"
```

---

### Task 7: Extension store pages: readers as data, labels in a shadow root, product pages

The store-page side of the extension, tested in Node with saved pages; Task 8 wires it into the browser.

**Files:**
- Create: `extension/ownership/stores.js`, `extension/ownership/read.js`, `extension/ownership/label.js`, `extension/ownership/content.js`, `extension/_locales/en/messages.json`, `extension/_locales/es/messages.json`, `extension/tests/helpers.mjs`, `extension/tests/i18n.test.mjs`, `extension/tests/ownership-pages.test.mjs`, `extension/tests/ownership-labels.test.mjs`, `extension/tests/ownership-content.test.mjs`, `extension/tests/samples/steam-app.html`, `extension/tests/samples/gog-game.html`, `extension/tests/samples/humble-store.html`, `extension/tests/samples/fanatical-game.html`
- Modify: `extension/manifest.json` (`default_locale`), `web/package.json` and `web/package-lock.json` (dev dependency `happy-dom`)

**Interfaces:**
- Produces (all on `globalThis.gvOwnership`, files written as classic scripts that also load as ES modules):
  - `STORES: Array<{ store: string; name: string; host: string; pages: Page[] }>`; product `Page`: `{ kind: 'product', path: string /* RegExp source on the pathname */, id?: { fromPath: number } | { selectors: string[]; attr: string }, title: string[], anchor: string[] }`
  - `storeForUrl(url: string): Store | null`, `pageFor(url: string): { store, page, match } | null`
  - `first(root, selectors): Element | null`, `textOf(el): string`, `hasLabel(el): boolean`
  - `readPage(doc, found): { kind: 'product', items: Array<{ item: { store?, id?, title? }, element }> } | null`
  - `copiesText(copies, t): string`, `unavailableModel(reason, t)`, `labelModel(answer, t)` → `{ kind, tone, text, title?, href?, block? }` with `answer = { status: 'ok', result } | { status: 'unavailable', reason }`; result = `{ status, game: { title, url, copies: [{ platform, system, kind, pendingKey }] } | null, candidates: [...] }`
  - `renderLabel(doc, parent, model): { host, root }` (host carries `data-gamevault="<kind>"`), `rootOf(host): ShadowRoot | undefined` (tests)
  - `start({ doc, url, send, t, MutationObserver, setTimer, clearTimer, debounceMs }): { scan, stop }`
  - message names in `_locales/*/messages.json`: `labelOwned`, `labelNotOwned`, `labelPossible`, `labelUnavailable`, `unavailableUnreachable`, `unavailableToken`, `unavailableSignIn`, `unavailableSetup`, `kindDisc`, `kindKey`, `kindKeyPending`, `kindLibrary`, `moreCopies`
  - test helpers (`extension/tests/helpers.mjs`): `ns`, `pageWindow(html, url)`, `sample(file)`, `makeT(lang)`, `settle(ms)`, `labelText(host)`

- [ ] **Step 1: happy-dom for the tests.** Add it to the web's dev dependencies from inside the toolchain:

Run: `docker run --rm -v "$PWD:/src" -w /src/web -v gamevault-node-modules:/src/web/node_modules -v gamevault-npm:/root/.npm gamevault-toolchain:446777a3814f npm install --save-dev happy-dom`
Expected: `web/package.json` gains `"happy-dom"` under `devDependencies`, `web/package-lock.json` changes.

Check it loads the way the tests load it and has what they need:

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node -e "const r=require('node:module').createRequire('/src/web/package.json'); const {Window}=r('happy-dom'); const w=new Window({url:'https://www.gog.com/en/game/x'}); w.document.write('<!doctype html><html><body><h1 id=t>Echo Valley</h1></body></html>'); console.log(typeof w.document.createElement('span').attachShadow, typeof w.MutationObserver, w.document.getElementById('t').textContent)"`
Expected: `function function Echo Valley`. (If `document.write` does not give that title, change `pageWindow` in Step 3 to `window.document.documentElement.innerHTML = html.replace(/^[\s\S]*?<html[^>]*>|<\/html>[\s\S]*$/gi, '')` and run the check again with that line.)

- [ ] **Step 2: Texts.** Create `extension/_locales/en/messages.json`:

```json
{
  "labelOwned": { "message": "In your collection", "description": "Label next to a game's title on a store page when Game Vault has it." },
  "labelNotOwned": { "message": "Not in your collection" },
  "labelPossible": {
    "message": "Possible: $TITLES$",
    "placeholders": { "titles": { "content": "$1", "example": "Harbor Lights, Harbor Lights (2019)" } }
  },
  "labelUnavailable": { "message": "Game Vault unavailable" },
  "unavailableUnreachable": { "message": "Game Vault did not answer. Is it running, and is its address right in the extension?" },
  "unavailableToken": { "message": "Game Vault refused the access token. Create a new one in System → Security and paste it in the extension." },
  "unavailableSignIn": { "message": "Game Vault needs an access token from this browser. Create one in System → Security and paste it in the extension." },
  "unavailableSetup": { "message": "Choose your Game Vault address in the extension's menu and turn on \"Show what I own on store pages\"." },
  "kindDisc": { "message": "disc" },
  "kindKey": { "message": "key" },
  "kindKeyPending": { "message": "key not redeemed" },
  "kindLibrary": { "message": "library" },
  "moreCopies": {
    "message": "+$COUNT$",
    "placeholders": { "count": { "content": "$1", "example": "2" } }
  }
}
```

and `extension/_locales/es/messages.json`:

```json
{
  "labelOwned": { "message": "En tu colección" },
  "labelNotOwned": { "message": "No está en tu colección" },
  "labelPossible": {
    "message": "Posible: $TITLES$",
    "placeholders": { "titles": { "content": "$1", "example": "Harbor Lights, Harbor Lights (2019)" } }
  },
  "labelUnavailable": { "message": "Game Vault no disponible" },
  "unavailableUnreachable": { "message": "Game Vault no ha respondido. ¿Está en marcha y su dirección es correcta en la extensión?" },
  "unavailableToken": { "message": "Game Vault ha rechazado el token de acceso. Crea uno nuevo en Sistema → Seguridad y pégalo en la extensión." },
  "unavailableSignIn": { "message": "Game Vault necesita un token de acceso desde este navegador. Créalo en Sistema → Seguridad y pégalo en la extensión." },
  "unavailableSetup": { "message": "Elige la dirección de tu Game Vault en el menú de la extensión y activa \"Mostrar lo que tengo en las tiendas\"." },
  "kindDisc": { "message": "disco" },
  "kindKey": { "message": "clave" },
  "kindKeyPending": { "message": "clave sin canjear" },
  "kindLibrary": { "message": "biblioteca" },
  "moreCopies": {
    "message": "+$COUNT$",
    "placeholders": { "count": { "content": "$1", "example": "2" } }
  }
}
```

In `extension/manifest.json`, add `"default_locale": "en",` after `"version"`.

- [ ] **Step 3: Test helpers** (`extension/tests/helpers.mjs`)

```js
// Shared by the extension's store-page tests: a DOM for saved pages (happy-dom, a dev dependency of
// the web, loaded from web/node_modules; nothing is added to the extension) and chrome.i18n's
// message lookup over _locales.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import '../ownership/stores.js';
import '../ownership/read.js';
import '../ownership/label.js';
import '../ownership/content.js';

/** The content scripts' namespace. */
export const ns = globalThis.gvOwnership;

const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { Window } = require('happy-dom');

/** A window showing html at url; scripts never run. Close it with window.happyDOM.close(). */
export function pageWindow(html, url) {
  const window = new Window({
    url,
    settings: { disableJavaScriptEvaluation: true, disableJavaScriptFileLoading: true, disableCSSFileLoading: true, disableIframePageLoading: true },
  });
  window.document.write(html);
  return window;
}

/** A saved store page from tests/samples/ (invented titles). */
export function sample(file) {
  return readFileSync(new URL(`./samples/${file}`, import.meta.url), 'utf8');
}

/** chrome.i18n.getMessage over a locale's messages.json: named placeholders, then $1… substitutions. */
export function makeT(lang = 'en') {
  const messages = JSON.parse(readFileSync(new URL(`../_locales/${lang}/messages.json`, import.meta.url), 'utf8'));
  return (key, subs = []) => {
    const m = messages[key];
    if (!m) return '';
    const list = Array.isArray(subs) ? subs : [subs];
    return m.message
      .replace(/\$([A-Za-z0-9_@]+)\$/g, (_, name) => m.placeholders?.[name.toLowerCase()]?.content ?? '')
      .replace(/\$(\d)/g, (_, n) => String(list[Number(n) - 1] ?? ''));
  };
}

/** Waits for debounced scans and MutationObserver callbacks. */
export const settle = (ms = 40) => new Promise((resolve) => setTimeout(resolve, ms));

/** The text a label shows (its shadow root is closed to the page, not to the extension). */
export const labelText = (host) => ns.rootOf(host)?.querySelector('.gv')?.textContent ?? '';
```

- [ ] **Step 4: Saved sample pages** (invented titles and ids; only the markup the readers use, in the stores' shape)

`extension/tests/samples/steam-app.html`:

```html
<!doctype html>
<html lang="en">
<head><title>Lantern Keep on Steam</title></head>
<body class="v6 app game_bg">
  <div class="page_content_ctn">
    <div class="apphub_HomeHeaderContent">
      <div class="apphub_HeaderStandardTop">
        <div class="apphub_OtherSiteInfo"><a class="btnv6_blue_hoverfade" href="#">Community Hub</a></div>
        <div id="appHubAppName" class="apphub_AppName">Lantern Keep</div>
      </div>
    </div>
    <div id="game_area_purchase">
      <div class="game_area_purchase_game"><h1>Buy Lantern Keep</h1><div class="game_purchase_price price">$19.99</div></div>
    </div>
  </div>
</body>
</html>
```

`extension/tests/samples/gog-game.html`:

```html
<!doctype html>
<html lang="en">
<head><title>Echo Valley on GOG.com</title></head>
<body>
  <div class="layout" card-product="1207664663">
    <div class="productcard-basics">
      <h1 class="productcard-basics__title" selenium-id="ProductCardTitle">Echo Valley</h1>
    </div>
    <div class="product-actions-price">$9.99</div>
  </div>
</body>
</html>
```

`extension/tests/samples/humble-store.html`:

```html
<!doctype html>
<html lang="en">
<head><title>Harbor Lights | Humble Bundle Store</title></head>
<body>
  <div class="page-wrap">
    <div class="product-header-view">
      <h1 class="human_name-view">Harbor Lights</h1>
      <div class="developer-view">Invented Studio</div>
    </div>
    <div class="current-price">$14.99</div>
  </div>
</body>
</html>
```

`extension/tests/samples/fanatical-game.html` (Fanatical renders in the browser; this is the drawn page):

```html
<!doctype html>
<html lang="en">
<head><title>Copper Coast | PC Steam Game | Fanatical</title></head>
<body>
  <div id="root">
    <main>
      <div class="product-commerce-container">
        <h1 class="product-name">Copper Coast</h1>
        <div class="price-container">€12.49</div>
      </div>
    </main>
  </div>
</body>
</html>
```

- [ ] **Step 5: Failing tests**

`extension/tests/i18n.test.mjs`:

```js
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const load = (path) => JSON.parse(readFileSync(new URL(path, import.meta.url), 'utf8'));

test('every extension text exists in English and Spanish, with the same placeholders', () => {
  const en = load('../_locales/en/messages.json');
  const es = load('../_locales/es/messages.json');
  assert.deepEqual(Object.keys(es).sort(), Object.keys(en).sort());
  for (const key of Object.keys(en)) {
    assert.deepEqual(Object.keys(es[key].placeholders ?? {}).sort(), Object.keys(en[key].placeholders ?? {}).sort(), key);
  }
});

test('the manifest falls back to English', () => {
  assert.equal(load('../manifest.json').default_locale, 'en');
});
```

`extension/tests/ownership-pages.test.mjs`:

```js
// Each store's reader against a saved page. When a store changes its markup, fix the selectors in
// ownership/stores.js and update the sample here.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ns, pageWindow, sample } from './helpers.mjs';

const products = [
  { file: 'steam-app.html', url: 'https://store.steampowered.com/app/4410/Lantern_Keep/', item: { store: 'steam', id: '4410', title: 'Lantern Keep' } },
  { file: 'gog-game.html', url: 'https://www.gog.com/en/game/echo_valley', item: { store: 'gog', id: '1207664663', title: 'Echo Valley' } },
  { file: 'humble-store.html', url: 'https://www.humblebundle.com/store/harbor-lights', item: { title: 'Harbor Lights' } },
  { file: 'fanatical-game.html', url: 'https://www.fanatical.com/en/game/copper-coast', item: { title: 'Copper Coast' } },
];

for (const p of products) {
  test(`${p.file}: the product page gives its item and the element its label goes in`, async (t) => {
    const window = pageWindow(sample(p.file), p.url);
    t.after(() => window.happyDOM.close());
    const found = ns.pageFor(p.url);
    assert.ok(found, 'a known store page');
    const read = ns.readPage(window.document, found);
    assert.equal(read.kind, 'product');
    assert.deepEqual(read.items.map((e) => e.item), [p.item]);
    assert.match(read.items[0].element.textContent, new RegExp(p.item.title));
  });
}

test('other pages of the stores, and other sites, are left alone', () => {
  for (const url of [
    'https://store.steampowered.com/',
    'https://store.steampowered.com/agecheck/app/4410/',
    'https://www.gog.com/en/games',
    'https://example.com/app/4410',
    'https://store.steampowered.com.example.com/app/4410/',
    'not an address',
  ]) assert.equal(ns.pageFor(url), null, url);
});

test('a product page that has not drawn its title yet reads as nothing, for now', async (t) => {
  const url = 'https://www.fanatical.com/en/game/copper-coast';
  const window = pageWindow('<!doctype html><html><body><div id="root"></div></body></html>', url);
  t.after(() => window.happyDOM.close());
  assert.equal(ns.readPage(window.document, ns.pageFor(url)), null);
});

test('a GOG product id that is not a number is not sent; the title still is', async (t) => {
  const url = 'https://www.gog.com/en/game/echo_valley';
  const window = pageWindow('<!doctype html><html><body><div card-product="{{ id }}"><h1 class="productcard-basics__title">Echo Valley</h1></div></body></html>', url);
  t.after(() => window.happyDOM.close());
  assert.deepEqual(ns.readPage(window.document, ns.pageFor(url)).items[0].item, { title: 'Echo Valley' });
});
```

`extension/tests/ownership-labels.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { makeT, ns, pageWindow } from './helpers.mjs';

const t = makeT('en');
const GV = 'http://127.0.0.1:8093';
const game = (title, copies = [], url = `${GV}/#/game/g1`) => ({ title, url, copies });
const disc = (platform) => ({ platform, system: platform, kind: 'physical', pendingKey: false });

test('an owned game says so, with each kind of copy once', () => {
  const m = ns.labelModel({ status: 'ok', result: { status: 'owned', candidates: [], game: game('Lantern Keep', [
    disc('PS4'),
    { platform: 'Steam', system: 'PC', kind: 'key', pendingKey: true },
    { platform: 'Steam', system: 'PC', kind: 'key', pendingKey: true },
    { platform: 'GOG', system: 'PC', kind: 'library', pendingKey: false },
  ]) } }, t);
  assert.equal(m.kind, 'owned');
  assert.equal(m.text, 'In your collection: PS4 · disc, Steam · key not redeemed, GOG · library');
  assert.equal(m.href, `${GV}/#/game/g1`);
  assert.equal(m.title, 'Lantern Keep');
});

test('more than three kinds of copy end in +N', () => {
  assert.equal(ns.copiesText(['PS4', 'PS5', 'Switch', 'Xbox One', 'Wii U'].map(disc), t), 'PS4 · disc, PS5 · disc, Switch · disc +2');
});

test('a possible match names its candidates and links to the first', () => {
  const m = ns.labelModel({ status: 'ok', result: { status: 'possible', game: null, candidates: [
    game('Harbor Lights'), game('Harbor Lights (2019)', [], `${GV}/#/game/g2`),
  ] } }, t);
  assert.equal(m.kind, 'possible');
  assert.equal(m.text, 'Possible: Harbor Lights, Harbor Lights (2019)');
  assert.equal(m.href, `${GV}/#/game/g1`);
});

test('not owned is quiet; an unanswered lookup is "unavailable", never "not in your collection"', () => {
  assert.equal(ns.labelModel({ status: 'ok', result: { status: 'not_owned', game: null, candidates: [] } }, t).text, 'Not in your collection');
  for (const reason of ['unreachable', 'token', 'signin', 'setup', 'denied', undefined]) {
    const m = ns.labelModel({ status: 'unavailable', reason }, t);
    assert.equal(m.kind, 'unavailable', String(reason));
    assert.equal(m.text, 'Game Vault unavailable');
    assert.ok(m.title.length > 0, `a hint for ${reason}`);
  }
  assert.equal(ns.labelModel(undefined, t).kind, 'unavailable', 'no answer at all');
  assert.equal(ns.labelModel({ status: 'ok' }, t).kind, 'unavailable', 'an answer without a result');
});

test('labels render in a closed shadow root with their own styles, text as text, and only web links', async (tt) => {
  const window = pageWindow('<!doctype html><html><head><style>span, a { display: none !important; }</style></head><body><h1 id="t">Lantern Keep</h1></body></html>',
    'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const h1 = window.document.getElementById('t');
  const { host, root } = ns.renderLabel(window.document, h1, { kind: 'owned', tone: 'owned', text: '<b>In</b> your collection', href: `${GV}/#/game/g1` });
  assert.equal(host.shadowRoot, null, 'closed: the store page cannot reach into it');
  assert.equal(ns.rootOf(host), root);
  assert.match(host.getAttribute('style'), /all:\s*initial/, 'the store page styles do not reach the host');
  assert.match(root.querySelector('style').textContent, /\.gv/);
  assert.equal(h1.textContent, 'Lantern Keep', 'the title the reader sees is unchanged');
  assert.ok(ns.hasLabel(h1));
  const a = root.querySelector('a');
  assert.equal(a.textContent, '<b>In</b> your collection');
  assert.equal(root.querySelector('b'), null);
  assert.equal(a.getAttribute('target'), '_blank');
  assert.equal(a.getAttribute('rel'), 'noopener noreferrer');
  const { root: other } = ns.renderLabel(window.document, h1, { kind: 'possible', tone: 'possible', text: 'x', href: 'javascript:alert(1)' });
  assert.equal(other.querySelector('a'), null, 'not a web address: no link');
});
```

`extension/tests/ownership-content.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { makeT, ns, pageWindow, sample, settle } from './helpers.mjs';

const t = makeT('en');
const owned = (title) => ({
  status: 'owned',
  game: { title, url: 'http://127.0.0.1:8093/#/game/g1', copies: [{ platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false }] },
  candidates: [],
});
const notOwned = () => ({ status: 'not_owned', game: null, candidates: [] });

/** Follows window's page as the content script does; url.current is the address, send answers lookups. */
function follow(window, url, send) {
  const sent = [];
  const ctl = ns.start({
    doc: window.document,
    url: () => url.current,
    send: async (items) => { sent.push(items); return send(items); },
    t,
    MutationObserver: window.MutationObserver,
    setTimer: (fn, ms) => setTimeout(fn, ms),
    clearTimer: (h) => clearTimeout(h),
    debounceMs: 5,
  });
  return { sent, ctl };
}

const done = (tt, window, ctl) => tt.after(async () => { ctl.stop(); await window.happyDOM.close(); });

test('a product page gets one label, asked for once, even while the page keeps changing', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  const { sent, ctl } = follow(window, url, async () => ({ status: 'ok', results: [owned('Lantern Keep')] }));
  done(tt, window, ctl);
  await settle();
  window.document.body.append(window.document.createElement('div')); // the store's own late changes
  await settle();
  assert.deepEqual(sent, [[{ store: 'steam', id: '4410', title: 'Lantern Keep' }]]);
  assert.equal(window.document.querySelectorAll('[data-gamevault="owned"]').length, 1);
});

test('a single-page store that draws its title late is labelled once the title appears', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/game/copper-coast' };
  const window = pageWindow('<!doctype html><html><body><div id="root"></div></body></html>', url.current);
  const { sent, ctl } = follow(window, url, async () => ({ status: 'ok', results: [owned('Copper Coast')] }));
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 0, 'nothing to ask yet');
  window.document.getElementById('root').innerHTML = '<main><h1 class="product-name">Copper Coast</h1></main>';
  await settle();
  assert.deepEqual(sent, [[{ title: 'Copper Coast' }]]);
  assert.ok(ns.hasLabel(window.document.querySelector('h1.product-name')));
});

test('moving to another product without a reload asks again and keeps only the new label', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/game/copper-coast' };
  const window = pageWindow(sample('fanatical-game.html'), url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({ status: 'ok', results: items.map(notOwned) }));
  done(tt, window, ctl);
  await settle();
  url.current = 'https://www.fanatical.com/en/game/paper-comet';
  window.document.querySelector('h1.product-name').textContent = 'Paper Comet'; // the label's host stays inside the h1
  window.document.body.append(window.document.createElement('div'));
  await settle();
  assert.deepEqual(sent.map((items) => items[0].title), ['Copper Coast', 'Paper Comet']);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 1);
});

test('when Game Vault cannot answer, the label says unavailable, never "not in your collection"', async (tt) => {
  for (const send of [
    async () => ({ status: 'unavailable', reason: 'unreachable' }),
    async () => { throw new Error('the extension was reloaded'); },
    async () => undefined,
  ]) {
    const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
    const window = pageWindow(sample('steam-app.html'), url.current);
    const { ctl } = follow(window, url, send);
    done(tt, window, ctl);
    await settle();
    assert.equal(window.document.querySelectorAll('[data-gamevault="unavailable"]').length, 1);
    assert.equal(window.document.querySelectorAll('[data-gamevault="not-owned"]').length, 0);
  }
});
```

Note on the third test: setting the h1's `textContent` removes its children, label host included, which is what a single-page store does when it redraws.

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/i18n.test.mjs extension/tests/ownership-pages.test.mjs extension/tests/ownership-labels.test.mjs extension/tests/ownership-content.test.mjs`
Expected: `i18n.test.mjs` passes; the others FAIL (`Cannot find module …/ownership/stores.js`).

- [ ] **Step 6: The stores as data** (`extension/ownership/stores.js`)

```js
// Store pages where Game Vault Connector shows what you own. Data, not code: when a store changes
// its pages, fix the patterns and selectors here and the sample page in tests/samples/. A page's
// `path` is a regular expression on the address's path; every selector list is tried in order and
// the first that matches wins. The selectors were checked on the live sites when this feature was
// built (see docs/technical.md).
// Loaded as a content script (a classic script) and imported by the background and the tests, so
// it only adds to globalThis.gvOwnership.
(() => {
  const ns = (globalThis.gvOwnership ??= {});

  ns.STORES = [
    {
      store: 'steam',
      name: 'Steam',
      host: 'store.steampowered.com',
      pages: [
        {
          kind: 'product',
          path: '^/app/(\\d+)',
          id: { fromPath: 1 },
          title: ['#appHubAppName', '.apphub_AppName'],
          anchor: ['#appHubAppName', '.apphub_AppName'],
        },
      ],
    },
    {
      store: 'gog',
      name: 'GOG',
      host: 'www.gog.com',
      pages: [
        {
          kind: 'product',
          path: '^/(?:[a-z]{2}/)?game/[^/?#]+',
          id: { selectors: ['[card-product]'], attr: 'card-product' },
          title: ['h1.productcard-basics__title', '.productcard-basics__title'],
          anchor: ['h1.productcard-basics__title', '.productcard-basics__title'],
        },
      ],
    },
    {
      store: 'humble',
      name: 'Humble Bundle',
      host: 'www.humblebundle.com',
      pages: [
        {
          kind: 'product',
          path: '^/store/[^/?#]+/?$',
          title: ['h1.human_name-view', '.product-header-view h1'],
          anchor: ['h1.human_name-view', '.product-header-view h1'],
        },
      ],
    },
    {
      store: 'fanatical',
      name: 'Fanatical',
      host: 'www.fanatical.com',
      pages: [
        {
          kind: 'product',
          path: '^/[a-z]{2}(?:-[a-z]{2})?/game/[^/?#]+',
          title: ['h1.product-name', 'main h1'],
          anchor: ['h1.product-name', 'main h1'],
        },
      ],
    },
  ];

  /** The store a page address belongs to (by exact host name), or null. */
  ns.storeForUrl = (url) => {
    let host;
    try {
      host = new URL(url).hostname;
    } catch {
      return null;
    }
    return ns.STORES.find((s) => s.host === host) ?? null;
  };

  /** The store page an address is, with the path's match (ids in the path), or null. */
  ns.pageFor = (url) => {
    const store = ns.storeForUrl(url);
    if (!store) return null;
    const { pathname } = new URL(url);
    for (const page of store.pages) {
      const match = new RegExp(page.path).exec(pathname);
      if (match) return { store, page, match };
    }
    return null;
  };
})();
```

- [ ] **Step 7: Reading a page** (`extension/ownership/read.js`)

```js
// Reads a store page the way its store's data describes it (stores.js): the items to look up and
// the elements their labels go in. Runs in the page (content script) and in Node tests.
(() => {
  const ns = (globalThis.gvOwnership ??= {});

  /** The first element matching one of the selectors, in the list's order. */
  ns.first = (root, selectors) => {
    for (const s of selectors ?? []) {
      const el = root?.querySelector(s);
      if (el) return el;
    }
    return null;
  };

  /** An element's text on one line. Labels live in shadow roots, so they never add to it. */
  ns.textOf = (el) => (el?.textContent ?? '').replace(/\s+/g, ' ').trim();

  /** Whether the extension already put a label (or an invisible "checked" mark) in this element. */
  ns.hasLabel = (el) => Boolean(el) && [...el.children].some((c) => c.hasAttribute('data-gamevault'));

  function productId(doc, found) {
    const { page, match } = found;
    if (page.id?.fromPath) return match[page.id.fromPath] ?? '';
    if (page.id?.attr) {
      const v = ns.first(doc, page.id.selectors)?.getAttribute(page.id.attr)?.trim() ?? '';
      return /^\d{1,20}$/.test(v) ? v : '';
    }
    return '';
  }

  /**
   * What the page shows, or null while it has not drawn it (or it is no game): `items` pairs each
   * item to look up ({store, id} and/or {title}) with the element its label goes in.
   */
  ns.readPage = (doc, found) => {
    const { store, page } = found;
    if (page.kind === 'product') {
      const anchor = ns.first(doc, page.anchor);
      if (!anchor) return null;
      const item = {};
      const id = productId(doc, found);
      if (id) {
        item.store = store.store;
        item.id = id;
      }
      const title = ns.textOf(ns.first(doc, page.title));
      if (title) item.title = title;
      if (!item.id && !item.title) return null;
      return { kind: 'product', items: [{ item, element: anchor }] };
    }
    return null; // a kind of page this version does not read
  };
})();
```

- [ ] **Step 8: Labels** (`extension/ownership/label.js`)

```js
// Labels on store pages: what they say (pure models, tested in Node) and how they are drawn (a
// closed shadow root with its own styles: the store's styles do not reach in, ours do not leak out).
(() => {
  const ns = (globalThis.gvOwnership ??= {});
  const MAX_COPIES = 3;
  const roots = new WeakMap(); // host → its closed shadow root, for the extension's own tests

  ns.LABEL_CSS = `
    .gv { display: inline-flex; align-items: center; gap: 6px; max-width: 36em; padding: 3px 10px; border-radius: 999px;
      font: 600 12px/1.4 system-ui, -apple-system, "Segoe UI", sans-serif; text-decoration: none; white-space: nowrap;
      overflow: hidden; text-overflow: ellipsis; box-sizing: border-box; }
    .gv::before { content: ""; flex: none; width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
    .owned { background: #181c2c; color: #ffffff; }
    .possible { background: #edf0f5; color: #181c2c; outline: 1px dashed #181c2c; outline-offset: -1px; }
    .muted { background: #edf0f5; color: #4b5266; }
    a.gv:hover { filter: brightness(1.15); }
    a.gv:focus-visible { outline: 2px solid #ffb020; outline-offset: 2px; }
  `;

  function kindWord(copy, t) {
    if (copy.kind === 'physical') return t('kindDisc');
    if (copy.kind === 'key') return t(copy.pendingKey ? 'kindKeyPending' : 'kindKey');
    return t('kindLibrary');
  }

  /** "PS4 · disc, Steam · key not redeemed": each different copy once, at most three, then "+N". */
  ns.copiesText = (copies, t) => {
    const parts = [...new Set((copies ?? []).map((c) => `${c.platform || c.system} · ${kindWord(c, t)}`))];
    const shown = parts.slice(0, MAX_COPIES).join(', ');
    return parts.length > MAX_COPIES ? `${shown} ${t('moreCopies', [String(parts.length - MAX_COPIES)])}` : shown;
  };

  const HINTS = { token: 'unavailableToken', signin: 'unavailableSignIn', setup: 'unavailableSetup' };

  /** The label when Game Vault could not answer: never "not in your collection". */
  ns.unavailableModel = (reason, t) => ({
    kind: 'unavailable',
    tone: 'muted',
    text: t('labelUnavailable'),
    title: t(HINTS[reason] ?? 'unavailableUnreachable'),
  });

  /** The product label for one item's answer: {status: 'ok', result} or {status: 'unavailable', reason}. */
  ns.labelModel = (answer, t) => {
    if (answer?.status !== 'ok' || !answer.result) return ns.unavailableModel(answer?.reason, t);
    const r = answer.result;
    if (r.status === 'owned' && r.game) {
      const copies = ns.copiesText(r.game.copies, t);
      return {
        kind: 'owned',
        tone: 'owned',
        text: copies ? `${t('labelOwned')}: ${copies}` : t('labelOwned'),
        title: r.game.title,
        href: r.game.url,
      };
    }
    if (r.status === 'possible' && r.candidates?.length) {
      const titles = r.candidates.map((c) => c.title);
      return { kind: 'possible', tone: 'possible', text: t('labelPossible', [titles.join(', ')]), title: titles.join('\n'), href: r.candidates[0].url };
    }
    return { kind: 'not-owned', tone: 'muted', text: t('labelNotOwned') };
  };

  /**
   * Draws a label at the end of parent, in a closed shadow root, and returns its host and root.
   * Text is set as text, never as HTML, and only http(s) links are kept. A model of kind 'none' is
   * an invisible mark that only says "already checked".
   */
  ns.renderLabel = (doc, parent, model) => {
    const host = doc.createElement('span');
    host.setAttribute('data-gamevault', model.kind);
    host.style.cssText = model.kind === 'none'
      ? 'display: none !important;'
      : `all: initial; display: ${model.block ? 'block' : 'inline-block'}; vertical-align: middle; margin: ${model.block ? '6px 0 0' : '0 0 0 10px'};`;
    const root = host.attachShadow({ mode: 'closed' });
    roots.set(host, root);
    if (model.kind !== 'none') {
      const style = doc.createElement('style');
      style.textContent = ns.LABEL_CSS;
      const href = /^https?:\/\//.test(model.href ?? '') ? model.href : '';
      const el = doc.createElement(href ? 'a' : 'span');
      el.className = `gv ${model.tone}`;
      el.textContent = model.text;
      if (model.title) el.title = model.title;
      if (href) {
        el.href = href;
        el.target = '_blank';
        el.rel = 'noopener noreferrer';
      }
      root.append(style, el);
    }
    parent.append(host);
    return { host, root };
  };

  /** The shadow root of a label's host (the extension's tests read labels through it). */
  ns.rootOf = (host) => roots.get(host);
})();
```

- [ ] **Step 9: The content controller** (`extension/ownership/content.js`)

```js
// Content script on the store sites the user allowed: reads the page (read.js), asks the background
// what the user owns, and draws the labels (label.js). Single-page navigation and late-loaded
// content are followed with a debounced MutationObserver; only new items are asked about.
(() => {
  const ns = (globalThis.gvOwnership ??= {});
  const UNREACHABLE = { status: 'unavailable', reason: 'unreachable' };

  const answerAt = (outcome, i) => (outcome?.status === 'ok' ? { status: 'ok', result: outcome.results?.[i] } : outcome);

  /**
   * Starts following a page. deps: doc; url() (the current address); send(items), resolving to
   * {status: 'ok', results} or {status: 'unavailable', reason}; t (chrome.i18n.getMessage);
   * MutationObserver; setTimer(fn, ms) and clearTimer(handle); debounceMs. Returns {scan, stop}.
   */
  ns.start = (deps) => {
    const { doc, url, t, debounceMs = 400 } = deps;
    let timer = null;
    let running = false;
    let again = false;
    let lastUrl = '';

    const ask = async (items) => {
      try {
        return (await deps.send(items)) ?? UNREACHABLE;
      } catch {
        return UNREACHABLE;
      }
    };

    async function scan() {
      if (running) {
        again = true;
        return;
      }
      running = true;
      try {
        const href = url();
        if (href !== lastUrl) {
          // Another page of a single-page site: the last page's labels no longer apply.
          lastUrl = href;
          for (const old of doc.querySelectorAll('[data-gamevault]')) old.remove();
        }
        const found = ns.pageFor(href);
        const read = found && ns.readPage(doc, found);
        if (!read) return;
        const fresh = read.items.filter((e) => !ns.hasLabel(e.element));
        if (!fresh.length) return;
        const outcome = await ask(fresh.map((e) => e.item));
        if (url() !== href) return; // the page changed while Game Vault answered
        fresh.forEach((e, i) => {
          if (!ns.hasLabel(e.element)) ns.renderLabel(doc, e.element, ns.labelModel(answerAt(outcome, i), t));
        });
      } finally {
        running = false;
        if (again) {
          again = false;
          schedule();
        }
      }
    }

    function schedule() {
      if (timer !== null) deps.clearTimer(timer);
      timer = deps.setTimer(() => {
        timer = null;
        scan();
      }, debounceMs);
    }

    const observer = new deps.MutationObserver(schedule);
    observer.observe(doc.documentElement, { childList: true, subtree: true, characterData: true });
    schedule();
    return {
      scan,
      stop() {
        observer.disconnect();
        if (timer !== null) deps.clearTimer(timer);
      },
    };
  };

  // In a store page (not in Node tests): start once per page.
  if (typeof document !== 'undefined' && globalThis.chrome?.runtime?.id && !globalThis.__gvOwnershipStarted) {
    globalThis.__gvOwnershipStarted = true;
    ns.start({
      doc: document,
      url: () => location.href,
      send: (items) => chrome.runtime.sendMessage({ op: 'ownership', items }),
      t: (key, subs) => chrome.i18n.getMessage(key, subs),
      MutationObserver,
      setTimer: (fn, ms) => setTimeout(fn, ms),
      clearTimer: (h) => clearTimeout(h),
    });
  }
})();
```

- [ ] **Step 10: Run the extension tests**

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/*.test.mjs`
Expected: PASS, the older `background`, `bridge`, `engine` and `recipe` tests included.

- [ ] **Step 11: Test and commit**

Run: `task test` (expected: green; `deps:web` reinstalls the web dependencies once, the lock changed).

```bash
git add extension web/package.json web/package-lock.json
git commit -m "Connector: store pages as data, labels in a shadow root, product pages of four stores"
```

---
### Task 8: Extension settings, permissions, script registration and lookups

**Files:**
- Create: `extension/lib/ownership.js`, `extension/tests/ownership-lookup.test.mjs`, `extension/tests/ownership-background.test.mjs`
- Modify: `extension/background.js`, `extension/popup.html`, `extension/popup.js`, `extension/ui.css`, `extension/manifest.json` (version `1.1.0`), `extension/_locales/en/messages.json`, `extension/_locales/es/messages.json`, `web/src/i18n/locales/en.json` and `es.json` (`connector.install`)

**Interfaces:**
- Consumes: Task 7 `globalThis.gvOwnership.STORES` and `storeForUrl` (from `ownership/stores.js`); Task 3's Connect JSON (`POST <origin>/gamevault.v1.OwnershipService/CheckOwnership`, body `{"items":[…]}`, answer `{"results":[…]}`).
- Produces (`extension/lib/ownership.js`, an ES module):
  - `MAX_ITEMS = 200`, `MAX_PAGE_ITEMS = 2000`, `CACHE_MS = 60000`
  - `storeForUrl(url)`, `storeOrigin(store): string`, `STORE_ORIGINS: string[]`, `scriptId(store): string` (`ownership-<store>`), `scriptFor(store)` (registration: ids, `matches`, `js: ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js']`, `runAt: 'document_idle'`, `persistAcrossSessions: true`)
  - `grantedStores(api): Promise<string[]>` (store names), `removableOrigins(grants): string[]`, `syncStoreScripts(api): Promise<void>`
  - `createLookup({ storage, fetch, now }): { check(items): Promise<{ status: 'ok', results } | { status: 'unavailable', reason: 'setup' | 'unreachable' | 'token' | 'signin' }>, clear() }`; each result `{ status: 'owned' | 'possible' | 'not_owned', game: { title, url, copies: [{ platform, system, kind, pendingKey }] } | null, candidates: [same as game] }`
  - storage (`chrome.storage.local`): `ownership: { enabled: boolean, origin: string, token: string }`
  - background message: `{ op: 'ownership', items }` from a store content script → the lookup's answer; anything else → `{ status: 'unavailable', reason: 'denied' }`

- [ ] **Step 1: Failing lookup test** (`extension/tests/ownership-lookup.test.mjs`)

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CACHE_MS, MAX_ITEMS, STORE_ORIGINS, createLookup, removableOrigins, scriptFor } from '../lib/ownership.js';

const GV = 'http://127.0.0.1:8093';
const json = (body, status = 200) => ({ ok: status >= 200 && status < 300, status, json: async () => body });

/** A lookup over fake storage and a fake Game Vault that owns "Lantern Keep" only. */
function setup({ ownership = { enabled: true, origin: GV, token: 'gvt_test' }, origins = [GV], answer } = {}) {
  const data = { ownership, origins };
  const calls = [];
  let clock = 1_000_000;
  const lookup = createLookup({
    storage: { get: async (k) => ({ [k]: data[k] }) },
    fetch: async (url, init) => {
      const body = JSON.parse(init.body);
      calls.push({ url, init, body });
      if (answer) return answer(body);
      return json({ results: body.items.map((it) => (it.title === 'Lantern Keep'
        ? { status: 'owned', game: { id: 'g1', title: 'Lantern Keep', path: '#/game/g1', copies: [{ platform: 'Steam', system: 'PC', kind: 'library' }] } }
        : { status: 'not_owned' })) });
    },
    now: () => clock,
  });
  return { lookup, calls, tick: (ms) => { clock += ms; } };
}

test('a lookup goes only to the chosen Game Vault, with the token, no cookies, and only what the page showed', async () => {
  const { lookup, calls } = setup();
  const out = await lookup.check([{ store: 'Steam', id: ' 4410 ', title: '  Lantern Keep ', price: '$19.99', cookie: 'x' }]);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, `${GV}/gamevault.v1.OwnershipService/CheckOwnership`);
  assert.equal(calls[0].init.method, 'POST');
  assert.equal(calls[0].init.credentials, 'omit');
  assert.equal(calls[0].init.headers.Authorization, 'Bearer gvt_test');
  assert.deepEqual(calls[0].body, { items: [{ store: 'steam', id: '4410', title: 'Lantern Keep' }] });
  assert.deepEqual(out, { status: 'ok', results: [{
    status: 'owned',
    game: { title: 'Lantern Keep', url: `${GV}/#/game/g1`, copies: [{ platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false }] },
    candidates: [],
  }] });
});

test('without a token no Authorization header is sent', async () => {
  const { lookup, calls } = setup({ ownership: { enabled: true, origin: GV, token: '' } });
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls[0].init.headers.Authorization, undefined);
});

test('a bundle with more than 200 games is asked in parts of at most 200, and the answers keep their order', async () => {
  const { lookup, calls } = setup();
  const items = Array.from({ length: 450 }, (_, i) => ({ title: i === 449 ? 'Lantern Keep' : `Invented Game ${i}` }));
  const out = await lookup.check(items);
  assert.deepEqual(calls.map((c) => c.body.items.length), [MAX_ITEMS, MAX_ITEMS, 50]);
  assert.equal(out.results.length, 450);
  assert.equal(out.results[0].status, 'not_owned');
  assert.equal(out.results[449].status, 'owned');
});

test('answers are reused for a minute, and forgotten when the settings change', async () => {
  const { lookup, calls, tick } = setup();
  await lookup.check([{ title: 'Lantern Keep' }]);
  await lookup.check([{ title: 'lantern keep' }]);
  assert.equal(calls.length, 1);
  tick(CACHE_MS);
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 2);
  lookup.clear();
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 3);
});

test('Game Vault not answering, or answering wrong, is unavailable — never not owned', async () => {
  for (const [name, answer, reason] of [
    ['network error', () => { throw new TypeError('Failed to fetch'); }, 'unreachable'],
    ['server error', () => json({ code: 'internal' }, 500), 'unreachable'],
    ['throttled', () => json({ code: 'resource_exhausted' }, 429), 'unreachable'],
    ['not JSON', () => ({ ok: true, status: 200, json: async () => { throw new SyntaxError('Unexpected token <'); } }), 'unreachable'],
    ['wrong number of results', () => json({ results: [] }), 'unreachable'],
    ['token refused', () => json({ code: 'unauthenticated' }, 401), 'token'],
  ]) {
    const { lookup } = setup({ answer });
    assert.deepEqual(await lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason }, name);
  }
});

test('without a token, a refusal asks for one', async () => {
  const { lookup } = setup({ ownership: { enabled: true, origin: GV }, answer: () => json({}, 401) });
  assert.deepEqual(await lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason: 'signin' });
});

test('a refused token forgets the cached answers', async () => {
  let refuse = false;
  const { lookup, calls } = setup({ answer: (body) => (refuse ? json({}, 401) : json({ results: body.items.map(() => ({ status: 'not_owned' })) })) });
  await lookup.check([{ title: 'Lantern Keep' }]);
  refuse = true;
  assert.equal((await lookup.check([{ title: 'Echo Valley' }])).reason, 'token');
  await lookup.check([{ title: 'Lantern Keep' }]);
  assert.equal(calls.length, 3, 'asked again: the answer cached with the old token was dropped');
});

test('nothing is sent unless the option is on and its address is still enabled', async () => {
  for (const [name, opts] of [
    ['off', { ownership: { enabled: false, origin: GV } }],
    ['no address', { ownership: { enabled: true } }],
    ['address removed', { ownership: { enabled: true, origin: GV }, origins: [] }],
    ['never set up', { ownership: undefined }],
  ]) {
    const { lookup, calls } = setup(opts);
    assert.deepEqual(await lookup.check([{ title: 'Lantern Keep' }]), { status: 'unavailable', reason: 'setup' }, name);
    assert.equal(calls.length, 0, name);
  }
});

test('a sheet link is only ever an address on the chosen Game Vault', async () => {
  for (const path of ['javascript:alert(1)', '//evil.example/#/game/g1', '#/game/../../x', undefined]) {
    const { lookup } = setup({ answer: () => json({ results: [{ status: 'owned', game: { title: 'Lantern Keep', path } }] }) });
    const out = await lookup.check([{ title: 'Lantern Keep' }]);
    assert.equal(out.results[0].game.url, `${GV}/`, String(path));
  }
});

test('turning the option off gives back only the store sites no sign-in uses', () => {
  assert.deepEqual(removableOrigins({ [GV]: ['www.humblebundle.com'] }), STORE_ORIGINS.filter((o) => o !== 'https://www.humblebundle.com/*'));
  assert.deepEqual(removableOrigins(undefined), STORE_ORIGINS);
});

test('each store gets its own content script, made of the four ownership files', () => {
  assert.deepEqual(STORE_ORIGINS, ['https://store.steampowered.com/*', 'https://www.gog.com/*', 'https://www.humblebundle.com/*', 'https://www.fanatical.com/*']);
  assert.deepEqual(scriptFor({ store: 'gog', host: 'www.gog.com' }), {
    id: 'ownership-gog',
    matches: ['https://www.gog.com/*'],
    js: ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js'],
    runAt: 'document_idle',
    persistAcrossSessions: true,
  });
});
```

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/ownership-lookup.test.mjs`
Expected: FAIL (`Cannot find module …/lib/ownership.js`).

- [ ] **Step 2: Write `extension/lib/ownership.js`**

```js
// The background's side of "what you own on store pages": which store sites get the content
// scripts, and the lookups they ask for, sent only to the user's own Game Vault. Pages never see
// the token: only this module talks to Game Vault.
import '../ownership/stores.js';

const { STORES } = globalThis.gvOwnership;

/** The store a page address belongs to, or null. */
export const storeForUrl = (url) => globalThis.gvOwnership.storeForUrl(url);

/** Most items in one request (the server's limit). */
export const MAX_ITEMS = 200;

/** Most items one page may ask about; the rest read as unavailable. */
export const MAX_PAGE_ITEMS = 2000;

/** How long an answer is reused for the same item, in milliseconds. */
export const CACHE_MS = 60_000;

const SCRIPTS = ['ownership/stores.js', 'ownership/read.js', 'ownership/label.js', 'ownership/content.js'];
const PROCEDURE = '/gamevault.v1.OwnershipService/CheckOwnership';
const SHEET = /^#\/game\/[A-Za-z0-9-]{1,64}$/;

/** The host permission a store needs. */
export const storeOrigin = (store) => `https://${store.host}/*`;

/** Every store's host permission, asked for together when the option is turned on. */
export const STORE_ORIGINS = STORES.map(storeOrigin);

/** The id of a store's registered content script. */
export const scriptId = (store) => `ownership-${store.store}`;

/** The content script registration of a store. */
export function scriptFor(store) {
  return { id: scriptId(store), matches: [storeOrigin(store)], js: SCRIPTS, runAt: 'document_idle', persistAcrossSessions: true };
}

/** The names of the stores the browser allows the extension on. */
export async function grantedStores(api) {
  const out = [];
  for (const s of STORES) if (await api.permissions.contains({ origins: [storeOrigin(s)] })) out.push(s.name);
  return out;
}

/** The store host permissions no sign-in needs: those no Game Vault address has a confirmed sign-in on. */
export function removableOrigins(grants) {
  const used = new Set(Object.values(grants ?? {}).flat());
  return STORES.filter((s) => !used.has(s.host)).map(storeOrigin);
}

/** Registers the content script of each store the browser allows while the option is on, and unregisters the rest. */
export async function syncStoreScripts(api) {
  const { ownership } = await api.storage.local.get('ownership');
  const registered = new Set((await api.scripting.getRegisteredContentScripts({ ids: STORES.map(scriptId) })).map((s) => s.id));
  for (const store of STORES) {
    const id = scriptId(store);
    const wanted = Boolean(ownership?.enabled) && await api.permissions.contains({ origins: [storeOrigin(store)] });
    if (wanted && !registered.has(id)) await api.scripting.registerContentScripts([scriptFor(store)]);
    if (!wanted && registered.has(id)) await api.scripting.unregisterContentScripts({ ids: [id] });
  }
}

/** Only what a page shows is sent: a store's id for the game, its title. */
function cleanItem(it) {
  const text = (v, n) => (typeof v === 'string' ? v.replace(/\s+/g, ' ').trim().slice(0, n) : '');
  const out = {};
  const store = text(it?.store, 32).toLowerCase();
  const id = text(it?.id, 200);
  const title = text(it?.title, 300);
  if (store && id) {
    out.store = store;
    out.id = id;
  }
  if (title) out.title = title;
  return out;
}

const keyOf = (origin, item) => [origin, item.store ?? '', item.id ?? '', (item.title ?? '').toLowerCase()].join('\u0000');

/** The sheet's address on the chosen Game Vault; anything else becomes its home page. */
const sheetUrl = (origin, path) => (SHEET.test(path ?? '') ? `${origin}/${path}` : `${origin}/`);

function gameOf(g, origin) {
  return {
    title: String(g?.title ?? ''),
    url: sheetUrl(origin, g?.path),
    copies: (g?.copies ?? []).map((c) => ({
      platform: String(c?.platform ?? ''),
      system: String(c?.system ?? ''),
      kind: String(c?.kind ?? ''),
      pendingKey: Boolean(c?.pendingKey),
    })),
  };
}

function resultOf(r, origin) {
  const status = ['owned', 'possible', 'not_owned'].includes(r?.status) ? r.status : 'not_owned';
  return {
    status,
    game: status === 'owned' && r.game ? gameOf(r.game, origin) : null,
    candidates: (r?.candidates ?? []).map((c) => gameOf(c, origin)),
  };
}

/**
 * The lookups store pages ask for. storage is chrome.storage.local (or a fake), fetch and now are
 * injected for tests. Answers are cached per item for CACHE_MS; clear() forgets them (settings
 * changed). A Game Vault that cannot answer is "unavailable", never "not owned".
 */
export function createLookup({ storage, fetch, now }) {
  const cache = new Map();
  const unavailable = (reason) => ({ status: 'unavailable', reason });

  return {
    clear() {
      cache.clear();
    },

    async check(raw) {
      const { ownership } = await storage.get('ownership');
      const { origins = [] } = await storage.get('origins');
      if (!ownership?.enabled || !ownership.origin || !origins.includes(ownership.origin)) return unavailable('setup');
      const origin = ownership.origin;
      const token = typeof ownership.token === 'string' ? ownership.token.trim() : '';
      const items = (Array.isArray(raw) ? raw : []).slice(0, MAX_PAGE_ITEMS).map(cleanItem);
      const results = new Array(items.length);
      const missing = [];
      items.forEach((item, i) => {
        const hit = cache.get(keyOf(origin, item));
        if (hit && now() - hit.at < CACHE_MS) results[i] = hit.result;
        else missing.push(i);
      });

      for (let start = 0; start < missing.length; start += MAX_ITEMS) {
        const part = missing.slice(start, start + MAX_ITEMS);
        let res;
        try {
          res = await fetch(origin + PROCEDURE, {
            method: 'POST',
            credentials: 'omit',
            headers: {
              'Content-Type': 'application/json',
              'Connect-Protocol-Version': '1',
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({ items: part.map((i) => items[i]) }),
          });
        } catch {
          return unavailable('unreachable');
        }
        if (res.status === 401) {
          cache.clear();
          return unavailable(token ? 'token' : 'signin');
        }
        if (!res.ok) return unavailable('unreachable');
        let got;
        try {
          got = (await res.json())?.results;
        } catch {
          return unavailable('unreachable');
        }
        if (!Array.isArray(got) || got.length !== part.length) return unavailable('unreachable');
        part.forEach((idx, j) => {
          results[idx] = resultOf(got[j], origin);
          cache.set(keyOf(origin, items[idx]), { result: results[idx], at: now() });
        });
      }
      return { status: 'ok', results };
    },
  };
}
```

Run the lookup test. Expected: PASS.

- [ ] **Step 3: Failing background test** (`extension/tests/ownership-background.test.mjs`)

```js
// The background's store-page side against a fake `chrome`: which content scripts it registers,
// and who gets lookups. Each test imports a fresh copy of background.js after installing its fake.
import assert from 'node:assert/strict';
import { test } from 'node:test';

const GV = 'http://127.0.0.1:8093';
const STEAM = 'https://store.steampowered.com/*';
const GOG = 'https://www.gog.com/*';
const flush = async () => { for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r)); };

function ev() {
  const listeners = [];
  return {
    listeners,
    addListener: (fn) => listeners.push(fn),
    removeListener: (fn) => { const i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1); },
    fire: (...args) => listeners.slice().forEach((fn) => fn(...args)),
  };
}

function fakeChrome({ data = {}, granted = [] } = {}) {
  const c = {
    data,
    granted: new Set(granted),
    registered: new Map(),
    runtime: {
      id: 'ext',
      onConnect: ev(), onMessage: ev(), onStartup: ev(), onInstalled: ev(),
      getManifest: () => ({ version: '1.1.0' }),
      getURL: (p) => `chrome-extension://ext/${p}`,
    },
    storage: {
      local: {
        get: async (k) => ({ [k]: data[k] }),
        set: async (o) => { Object.assign(data, o); },
        remove: async (k) => { delete data[k]; },
      },
      onChanged: ev(),
    },
    windows: { onRemoved: ev(), create: async () => ({ id: 1, tabs: [{ id: 2 }] }), remove: async () => {} },
    permissions: {
      onAdded: ev(),
      onRemoved: ev(),
      contains: async ({ origins }) => origins.every((o) => c.granted.has(o)),
    },
    tabs: { onRemoved: ev() },
    webNavigation: { onCommitted: ev(), onCompleted: ev(), onHistoryStateUpdated: ev() },
    scripting: {
      registerContentScripts: async (list) => { for (const s of list) c.registered.set(s.id, s); },
      unregisterContentScripts: async ({ ids }) => { for (const id of ids) c.registered.delete(id); },
      getRegisteredContentScripts: async ({ ids } = {}) => [...c.registered.values()].filter((s) => !ids || ids.includes(s.id)),
      executeScript: async () => [],
    },
    cookies: { getAllCookieStores: async () => [] },
    extension: { isAllowedIncognitoAccess: async () => false },
  };
  return c;
}

let loaded = 0;
async function load(options) {
  const chrome = fakeChrome(options);
  globalThis.chrome = chrome;
  await import(`../background.js?ownership=${loaded++}`);
  return chrome;
}

const ownershipScripts = (chrome) => [...chrome.registered.keys()].filter((id) => id.startsWith('ownership-')).sort();
const changed = (chrome) => chrome.storage.onChanged.fire({ ownership: { newValue: chrome.data.ownership } }, 'local');

/** Sends a message to the background as a content script would and waits for the answer. */
function ask(chrome, msg, sender) {
  return new Promise((resolve) => {
    const async = chrome.runtime.onMessage.listeners.map((fn) => fn(msg, sender, resolve));
    if (!async.includes(true)) setImmediate(() => resolve(undefined));
  });
}

test('turning the option on registers the store scripts only where the browser allowed them', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM, GOG] });
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-gog', 'ownership-steam']);
  assert.deepEqual(chrome.registered.get('ownership-steam').matches, [STEAM]);
});

test('a permission taken back in the browser, or the option turned off, removes the scripts', async () => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV } }, granted: [STEAM] });
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-steam']);

  chrome.granted.delete(STEAM);
  chrome.permissions.onRemoved.fire({ origins: [STEAM] });
  await flush();
  assert.deepEqual(ownershipScripts(chrome), []);

  chrome.granted.add(STEAM);
  chrome.permissions.onAdded.fire({ origins: [STEAM] });
  await flush();
  assert.deepEqual(ownershipScripts(chrome), ['ownership-steam']);

  chrome.data.ownership = { ...chrome.data.ownership, enabled: false };
  changed(chrome);
  await flush();
  assert.deepEqual(ownershipScripts(chrome), []);
});

test('a store page gets its answer from the chosen Game Vault through the background', async (t) => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV, token: 'gvt_test' } } });
  const sent = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    sent.push({ url, auth: init.headers.Authorization });
    return { ok: true, status: 200, json: async () => ({ results: JSON.parse(init.body).items.map(() => ({ status: 'not_owned' })) }) };
  };
  t.after(() => { globalThis.fetch = realFetch; });
  const answer = await ask(chrome, { op: 'ownership', items: [{ title: 'Lantern Keep' }] },
    { id: 'ext', url: 'https://store.steampowered.com/app/4410/', tab: { id: 3 } });
  assert.deepEqual(answer, { status: 'ok', results: [{ status: 'not_owned', game: null, candidates: [] }] });
  assert.deepEqual(sent, [{ url: `${GV}/gamevault.v1.OwnershipService/CheckOwnership`, auth: 'Bearer gvt_test' }]);
});

test('other pages, other extensions and non-tabs get no lookup', async (t) => {
  const chrome = await load({ data: { origins: [GV], ownership: { enabled: true, origin: GV, token: 'gvt_test' } } });
  let fetched = 0;
  const realFetch = globalThis.fetch;
  globalThis.fetch = async () => { fetched++; return { ok: true, status: 200, json: async () => ({ results: [] }) }; };
  t.after(() => { globalThis.fetch = realFetch; });
  for (const sender of [
    { id: 'ext', url: `${GV}/#/library`, tab: { id: 3 } },
    { id: 'ext', url: 'https://evil.example/app/4410/', tab: { id: 3 } },
    { id: 'other', url: 'https://store.steampowered.com/app/4410/', tab: { id: 3 } },
    { id: 'ext', url: 'https://store.steampowered.com/app/4410/' },
  ]) {
    const answer = await ask(chrome, { op: 'ownership', items: [{ title: 'Lantern Keep' }] }, sender);
    assert.deepEqual(answer, { status: 'unavailable', reason: 'denied' }, sender.url);
  }
  assert.equal(fetched, 0);
});
```

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/ownership-background.test.mjs`
Expected: FAIL (no scripts registered, no answer).

- [ ] **Step 4: The background** (`extension/background.js`)
  - Imports, after the existing ones: `import { createLookup, storeForUrl, syncStoreScripts } from './lib/ownership.js';`
  - After the `running` / `pending` maps:

    ```js
    // "What you own" on store pages: the store content scripts ask here, and only the background
    // talks to the user's Game Vault (with the token, which pages never see).
    const ownership = createLookup({ storage: chrome.storage.local, fetch: (url, init) => fetch(url, init), now: () => Date.now() });
    const syncStores = () => syncStoreScripts(chrome).catch(() => {});
    ```
  - At the end of the file:

    ```js
    // Lookups from the store content scripts: only from this extension's own scripts, in a tab, on a
    // store page.
    chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
      if (msg?.op !== 'ownership') return undefined;
      if (sender.id !== chrome.runtime.id || !sender.tab || !storeForUrl(sender.url ?? '')) {
        sendResponse({ status: 'unavailable', reason: 'denied' });
        return undefined;
      }
      ownership.check(msg.items).then(sendResponse, () => sendResponse({ status: 'unavailable', reason: 'unreachable' }));
      return true; // the answer comes later
    });

    // The store scripts follow the option and the browser's permissions; changed settings drop the
    // cached answers (another address, another token).
    chrome.permissions.onAdded?.addListener(syncStores);
    chrome.permissions.onRemoved?.addListener(syncStores);
    chrome.storage.onChanged?.addListener((changes, area) => {
      if (area !== 'local' || !(changes.ownership || changes.origins)) return;
      ownership.clear();
      syncStores();
    });
    ```
  - In `reregister`, after the loop: `await syncStores();` (the browser drops dynamic scripts on an update).
  - `extension/manifest.json`: `"version": "1.1.0"`.

Run both new extension tests and the old ones:

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/*.test.mjs`
Expected: PASS (`background.test.mjs` unchanged and green: its fake has no `storage.onChanged` or `permissions.onRemoved`, hence the `?.`).

- [ ] **Step 5: Popup texts.** Add to `extension/_locales/en/messages.json`:

```json
  "popupEnableOn": { "message": "Enable on $ORIGIN$", "placeholders": { "origin": { "content": "$1", "example": "http://127.0.0.1:8080" } } },
  "popupEnableHelp": { "message": "Only on your own Game Vault: this lets that address ask the extension to connect stores." },
  "popupEnabledHere": { "message": "Enabled on this address. Use Connect in a source's settings." },
  "popupAddresses": { "message": "Enabled Game Vault addresses" },
  "popupRemove": { "message": "Remove" },
  "popupForget": { "message": "Forget $HOST$", "placeholders": { "host": { "content": "$1", "example": "www.humblebundle.com" } } },
  "popupNoAddresses": { "message": "None yet: open your Game Vault and enable it here." },
  "popupVersion": { "message": "Version $VERSION$", "placeholders": { "version": { "content": "$1", "example": "1.1.0" } } },
  "ownershipTitle": { "message": "What you own, on store pages" },
  "ownershipToggle": { "message": "Show what I own on store pages" },
  "ownershipAddress": { "message": "Game Vault address" },
  "ownershipToken": { "message": "Access token (optional)" },
  "ownershipTokenHelp": { "message": "Needed when this browser is not on a trusted network: create one in Game Vault, System → Security. Only Steam, GOG, Humble Bundle and Fanatical pages are read, and only the game ids and titles they show are sent, only to your Game Vault." },
  "ownershipNoAddress": { "message": "Enable the extension on your Game Vault first." },
  "ownershipStores": { "message": "Shown on: $STORES$", "placeholders": { "stores": { "content": "$1", "example": "Steam, GOG" } } },
  "ownershipNoStores": { "message": "The browser has not allowed the store sites: turn the option off and on again." }
```

and to `extension/_locales/es/messages.json`:

```json
  "popupEnableOn": { "message": "Activar en $ORIGIN$", "placeholders": { "origin": { "content": "$1", "example": "http://127.0.0.1:8080" } } },
  "popupEnableHelp": { "message": "Solo en tu propio Game Vault: permite que esa dirección pida a la extensión conectar tiendas." },
  "popupEnabledHere": { "message": "Activada en esta dirección. Usa Conectar en los ajustes de una fuente." },
  "popupAddresses": { "message": "Direcciones de Game Vault activadas" },
  "popupRemove": { "message": "Quitar" },
  "popupForget": { "message": "Olvidar $HOST$", "placeholders": { "host": { "content": "$1", "example": "www.humblebundle.com" } } },
  "popupNoAddresses": { "message": "Ninguna todavía: abre tu Game Vault y actívala aquí." },
  "popupVersion": { "message": "Versión $VERSION$", "placeholders": { "version": { "content": "$1", "example": "1.1.0" } } },
  "ownershipTitle": { "message": "Lo que tienes, en las tiendas" },
  "ownershipToggle": { "message": "Mostrar lo que tengo en las tiendas" },
  "ownershipAddress": { "message": "Dirección de Game Vault" },
  "ownershipToken": { "message": "Token de acceso (opcional)" },
  "ownershipTokenHelp": { "message": "Hace falta cuando este navegador no está en una red de confianza: créalo en Game Vault, Sistema → Seguridad. Solo se leen páginas de Steam, GOG, Humble Bundle y Fanatical, y solo se envían los identificadores y títulos que muestran, y solo a tu Game Vault." },
  "ownershipNoAddress": { "message": "Activa antes la extensión en tu Game Vault." },
  "ownershipStores": { "message": "Se muestra en: $STORES$", "placeholders": { "stores": { "content": "$1", "example": "Steam, GOG" } } },
  "ownershipNoStores": { "message": "El navegador no ha permitido las tiendas: desactiva la opción y vuelve a activarla." }
```

The web's hint names the popup's button, which is now translated: in `web/src/i18n/locales/en.json` set `connector.install` to `"One click with the Game Vault Connector browser extension. Already installed? Open its icon on this page and enable this address."`, and in `es.json` to `"En un clic con la extensión Game Vault Connector. ¿Ya la tienes? Abre su icono en esta página y activa esta dirección."`.

- [ ] **Step 6: The popup** (`extension/popup.html`)

```html
<!doctype html>
<meta charset="utf-8">
<link rel="stylesheet" href="ui.css">
<main>
  <h1>Game Vault Connector</h1>
  <section id="current"></section>
  <h2 data-i18n="popupAddresses"></h2>
  <ul id="origins"></ul>
  <h2 data-i18n="ownershipTitle"></h2>
  <section id="ownership">
    <label class="row"><input type="checkbox" id="own-enabled"> <span data-i18n="ownershipToggle"></span></label>
    <label class="stack"><span data-i18n="ownershipAddress"></span><select id="own-origin"></select></label>
    <label class="stack"><span data-i18n="ownershipToken"></span>
      <input id="own-token" type="password" autocomplete="off" spellcheck="false" placeholder="gvt_…"></label>
    <p class="muted" data-i18n="ownershipTokenHelp"></p>
    <p class="muted" id="own-status"></p>
  </section>
  <p class="muted" id="version"></p>
</main>
<script type="module" src="popup.js"></script>
```

`extension/popup.js` (whole file):

```js
// The extension's menu: the Game Vault addresses enabled for sign-ins, and the store-page option.
import { bridgeId, enableOrigin } from './lib/bridges.js';
import { STORE_ORIGINS, grantedStores, removableOrigins } from './lib/ownership.js';

const $ = (id) => document.getElementById(id);
const load = async (k, d) => (await chrome.storage.local.get(k))[k] ?? d;
const t = (key, subs) => chrome.i18n.getMessage(key, subs);

// Static texts carry the name of their message.
for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);

async function saveOwnership(patch) {
  const ownership = await load('ownership', {});
  await chrome.storage.local.set({ ownership: { ...ownership, ...patch } });
}

async function render() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const origins = await load('origins', []);
  const grants = await load('grants', {});
  const current = $('current');
  current.textContent = '';
  let here = '';
  try { here = new URL(tab?.url ?? '').origin; } catch { /* not a web page */ }
  if (/^https?:/.test(here) && !origins.includes(here)) {
    const b = document.createElement('button');
    b.textContent = t('popupEnableOn', [here]);
    b.onclick = async () => {
      const { protocol, hostname } = new URL(here);
      // Firefox closes this popup for the permission prompt: the background finishes from here.
      await chrome.storage.local.set({ pendingEnable: { origin: here, tabId: tab.id } });
      if (!await chrome.permissions.request({ origins: [`${protocol}//${hostname}/*`] })) {
        await chrome.storage.local.remove('pendingEnable');
        return;
      }
      await chrome.storage.local.remove('pendingEnable');
      await enableOrigin(here, tab.id).catch(() => {}); // the background may be finishing it too
      render();
    };
    const p = document.createElement('p');
    p.className = 'muted';
    p.textContent = t('popupEnableHelp');
    current.append(b, p);
  } else if (origins.includes(here)) {
    current.textContent = t('popupEnabledHere');
  }

  const list = $('origins');
  list.textContent = '';
  for (const origin of origins) {
    const li = document.createElement('li');
    const remove = document.createElement('button');
    remove.textContent = t('popupRemove');
    remove.onclick = async () => {
      await chrome.scripting.unregisterContentScripts({ ids: [bridgeId(origin)] }).catch(() => {});
      delete grants[origin];
      await chrome.storage.local.set({ origins: origins.filter((o) => o !== origin), grants });
      render();
    };
    li.append(origin, ' ', remove);
    const hosts = grants[origin] ?? [];
    if (hosts.length) {
      const ul = document.createElement('ul');
      for (const h of hosts) {
        const hl = document.createElement('li');
        const x = document.createElement('button');
        x.textContent = '×';
        x.title = t('popupForget', [h]);
        x.onclick = async () => {
          grants[origin] = hosts.filter((o) => o !== h);
          await chrome.storage.local.set({ grants });
          render();
        };
        hl.append(h, ' ', x);
        ul.append(hl);
      }
      li.append(ul);
    }
    list.append(li);
  }
  if (!origins.length) {
    const li = document.createElement('li');
    li.className = 'muted';
    li.textContent = t('popupNoAddresses');
    list.append(li);
  }
  $('version').textContent = t('popupVersion', [chrome.runtime.getManifest().version]);
  await renderOwnership(origins);
}

async function renderOwnership(origins) {
  const ownership = await load('ownership', {});
  const select = $('own-origin');
  select.textContent = '';
  for (const o of origins) {
    const opt = document.createElement('option');
    opt.value = o;
    opt.textContent = o;
    select.append(opt);
  }
  const origin = origins.includes(ownership.origin) ? ownership.origin : origins[0] ?? '';
  select.value = origin;
  select.disabled = !origins.length;
  $('own-enabled').checked = Boolean(ownership.enabled);
  $('own-enabled').disabled = !origins.length;
  $('own-token').value = ownership.token ?? '';
  if (origin && origin !== ownership.origin) await saveOwnership({ origin });
  const stores = await grantedStores(chrome);
  $('own-status').textContent = !origins.length ? t('ownershipNoAddress')
    : !ownership.enabled ? ''
      : stores.length ? t('ownershipStores', [stores.join(', ')]) : t('ownershipNoStores');
}

$('own-origin').onchange = (e) => saveOwnership({ origin: e.target.value });
$('own-token').onchange = (e) => saveOwnership({ token: e.target.value.trim() });
$('own-enabled').onchange = async (e) => {
  if (e.target.checked) {
    // Saved first: Firefox closes this popup for the permission prompt, and the background
    // registers the store scripts once the browser grants them.
    await saveOwnership({ enabled: true });
    if (!await chrome.permissions.request({ origins: STORE_ORIGINS })) await saveOwnership({ enabled: false });
  } else {
    await saveOwnership({ enabled: false });
    const origins = removableOrigins(await load('grants', {}));
    if (origins.length) await chrome.permissions.remove({ origins }).catch(() => {});
  }
  render();
};

render();
```

`extension/ui.css`, append:

```css
.row { display: flex; align-items: center; gap: 6px; margin: 6px 0; }
.stack { display: flex; flex-direction: column; gap: 3px; margin: 8px 0; }
input[type="password"], select { font: inherit; padding: 4px 6px; border-radius: 6px; border: 1px solid #c8cdd8; background: transparent; color: inherit; }
```

- [ ] **Step 7: Test and commit**

Run: `task test`
Expected: green (`node --check` of `background.js` and `popup.js`, every extension test, `translations OK`).

```bash
git add extension web/src/i18n
git commit -m "Connector: store-page option, token, per-store scripts and lookups through the background"
```

---

### Task 9: Bundle pages (Humble, Fanatical): marks and a summary

**Files:**
- Create: `extension/tests/ownership-bundles.test.mjs`, `extension/tests/samples/humble-bundle.html`, `extension/tests/samples/fanatical-bundle.html`
- Modify: `extension/ownership/stores.js` (bundle pages), `extension/ownership/read.js` (bundle reading), `extension/ownership/label.js` (`markModel`, `summaryModel`), `extension/ownership/content.js` (bundle flow), `extension/_locales/en/messages.json`, `extension/_locales/es/messages.json`, `extension/tests/ownership-pages.test.mjs`, `extension/tests/ownership-labels.test.mjs`

**Interfaces:**
- Consumes: Task 7 `pageFor`, `first`, `textOf`, `hasLabel`, `renderLabel`, `unavailableModel`, `start`; Task 8's background chunking (pages send every item in one message).
- Produces:
  - bundle `Page`: `{ kind: 'bundle', path, items: { selectors: string[], title: string[] }, summary: string[] }`
  - `readPage` → `{ kind: 'bundle', items: Array<{ item: { title }, element }>, summary: Element } | null`
  - `markModel(answer, t)` → `{ kind: 'owned' | 'possible' | 'none', tone, block: true, text, title }` (no `href`)
  - `summaryModel(outcome, items, t)` → `{ kind: 'summary', tone, text, title? }`
  - messages `markOwned`, `markPendingKey`, `markPossible`, `summaryOwned`, `summaryPossible`

- [ ] **Step 1: Sample pages** (invented titles)

`extension/tests/samples/humble-bundle.html`:

```html
<!doctype html>
<html lang="en">
<head><title>Humble Invented Games Bundle</title></head>
<body>
  <div class="bundle-page">
    <div class="bundle-info"><h1 class="heading-medium">Invented Games Bundle</h1></div>
    <section class="tier">
      <div class="tier-header">Pay $1 or more</div>
      <div class="tier-item-view"><div class="item-details"><span class="item-title">Lantern Keep</span></div></div>
      <div class="tier-item-view"><div class="item-details"><span class="item-title">Paper Comet</span></div></div>
    </section>
    <section class="tier">
      <div class="tier-header">Pay $10 or more</div>
      <div class="tier-item-view"><div class="item-details"><span class="item-title">Harbor Lights</span></div></div>
      <div class="tier-item-view"><div class="item-details"><span class="item-title">Night Ferry</span></div></div>
      <div class="tier-item-view"><div class="item-details"><span class="item-title">  Lantern   Keep </span></div></div>
    </section>
  </div>
</body>
</html>
```

`extension/tests/samples/fanatical-bundle.html`:

```html
<!doctype html>
<html lang="en">
<head><title>Invented Mystery Bundle | Fanatical</title></head>
<body>
  <div id="root">
    <main>
      <h1>Invented Mystery Bundle</h1>
      <div class="bundle-games">
        <div class="bundle-game-card"><a href="/en/game/echo-valley"><h3 class="bundle-game-card__title">Echo Valley</h3></a></div>
        <div class="bundle-game-card"><a href="/en/game/copper-coast"><h3 class="bundle-game-card__title">Copper Coast</h3></a></div>
        <div class="bundle-game-card"><a href="/en/game/glass-orchard"><h3 class="bundle-game-card__title">Glass Orchard</h3></a></div>
      </div>
    </main>
  </div>
</body>
</html>
```

- [ ] **Step 2: Failing tests**

Append to `extension/tests/ownership-pages.test.mjs`:

```js
const bundles = [
  {
    file: 'humble-bundle.html',
    url: 'https://www.humblebundle.com/games/invented-games-bundle',
    titles: ['Lantern Keep', 'Paper Comet', 'Harbor Lights', 'Night Ferry', 'Lantern Keep'],
    summary: 'Invented Games Bundle',
  },
  {
    file: 'fanatical-bundle.html',
    url: 'https://www.fanatical.com/en/bundle/invented-mystery-bundle',
    titles: ['Echo Valley', 'Copper Coast', 'Glass Orchard'],
    summary: 'Invented Mystery Bundle',
  },
];

for (const b of bundles) {
  test(`${b.file}: the bundle page gives each game and the heading its summary goes in`, async (t) => {
    const window = pageWindow(sample(b.file), b.url);
    t.after(() => window.happyDOM.close());
    const read = ns.readPage(window.document, ns.pageFor(b.url));
    assert.equal(read.kind, 'bundle');
    assert.deepEqual(read.items.map((e) => e.item), b.titles.map((title) => ({ title })));
    assert.equal(ns.textOf(read.summary), b.summary);
  });
}

test('Fanatical pick-and-mix pages are bundles too', () => {
  assert.equal(ns.pageFor('https://www.fanatical.com/en/pick-and-mix/invented-build-your-own').page.kind, 'bundle');
  assert.equal(ns.pageFor('https://www.fanatical.com/es/bundle/invented-mystery-bundle').page.kind, 'bundle');
});
```

Append to `extension/tests/ownership-labels.test.mjs`:

```js
test('bundle marks say where a game is owned, or that only a key waits; never a link', () => {
  const answer = (result) => ({ status: 'ok', result });
  const both = ns.markModel(answer({ status: 'owned', candidates: [], game: game('Lantern Keep', [
    disc('PS4'), { platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false },
  ]) }), t);
  assert.equal(both.text, 'Owned on PS4, PC');
  assert.equal(both.href, undefined, 'store cards are often links: no link inside them');
  assert.equal(ns.markModel(answer({ status: 'owned', candidates: [], game: game('Echo Valley', [
    { platform: 'Steam', system: 'PC', kind: 'key', pendingKey: true },
  ]) }), t).text, 'Key not redeemed (PC)');
  assert.equal(ns.markModel(answer({ status: 'possible', game: null, candidates: [game('Harbor Lights')] }), t).text, 'Maybe owned');
  assert.equal(ns.markModel(answer({ status: 'not_owned', game: null, candidates: [] }), t).kind, 'none');
  assert.equal(ns.markModel({ status: 'unavailable', reason: 'unreachable' }, t).kind, 'none');
});

test('the bundle summary counts different titles, possible ones apart, and says unavailable when unanswered', () => {
  const items = ['Lantern Keep', 'Paper Comet', 'lantern keep', 'Harbor Lights'].map((title) => ({ title }));
  const results = [{ status: 'not_owned' }, { status: 'possible' }, { status: 'owned' }, { status: 'not_owned' }];
  const ok = ns.summaryModel({ status: 'ok', results }, items, t);
  assert.equal(ok.kind, 'summary');
  assert.equal(ok.text, 'You own 1 of 3 · 1 possible');
  const down = ns.summaryModel({ status: 'unavailable', reason: 'token' }, items, t);
  assert.equal(down.kind, 'summary');
  assert.equal(down.text, 'Game Vault unavailable');
  assert.match(down.title, /access token/);
});
```

`extension/tests/ownership-bundles.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { labelText, makeT, ns, pageWindow, sample, settle } from './helpers.mjs';

const t = makeT('en');
const owned = (title) => ({
  status: 'owned',
  game: { title, url: 'http://127.0.0.1:8093/#/game/g1', copies: [{ platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false }] },
  candidates: [],
});
const possible = (title) => ({ status: 'possible', game: null, candidates: [{ title, url: 'http://127.0.0.1:8093/#/game/g2', copies: [] }] });
const notOwned = () => ({ status: 'not_owned', game: null, candidates: [] });

/** Follows window's page as the content script does; url.current is the address, send answers lookups. */
function follow(window, url, send) {
  const sent = [];
  const ctl = ns.start({
    doc: window.document,
    url: () => url.current,
    send: async (items) => { sent.push(items); return send(items); },
    t,
    MutationObserver: window.MutationObserver,
    setTimer: (fn, ms) => setTimeout(fn, ms),
    clearTimer: (h) => clearTimeout(h),
    debounceMs: 5,
  });
  return { sent, ctl };
}

const done = (tt, window, ctl) => tt.after(async () => { ctl.stop(); await window.happyDOM.close(); });
const summaryText = (window) => labelText(window.document.querySelector('[data-gamevault="summary"]'));
const count = (window, kind) => window.document.querySelectorAll(`[data-gamevault="${kind}"]`).length;

test('a Humble bundle gets one mark per game and one summary', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-games-bundle' };
  const window = pageWindow(sample('humble-bundle.html'), url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({
    status: 'ok',
    results: items.map((it) => (it.title === 'Lantern Keep' ? owned(it.title) : it.title === 'Harbor Lights' ? possible(it.title) : notOwned())),
  }));
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 1);
  assert.equal(sent[0].length, 5);
  assert.equal(count(window, 'owned'), 2, 'the game listed in two tiers is marked in both');
  assert.equal(count(window, 'possible'), 1);
  assert.equal(count(window, 'summary'), 1);
  assert.equal(summaryText(window), 'You own 1 of 4 · 1 possible');
  for (const card of window.document.querySelectorAll('.tier-item-view')) {
    assert.equal([...card.children].filter((c) => c.hasAttribute('data-gamevault')).length, 1, 'exactly one mark, visible or not');
  }
});

test('games a bundle page adds later are marked too, and the summary counts them', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/pick-and-mix/invented-build-your-own' };
  const window = pageWindow(sample('fanatical-bundle.html'), url.current);
  const mine = new Set(['Echo Valley', 'Paper Comet']);
  const { sent, ctl } = follow(window, url, async (items) => ({ status: 'ok', results: items.map((it) => (mine.has(it.title) ? owned(it.title) : notOwned())) }));
  done(tt, window, ctl);
  await settle();
  assert.equal(summaryText(window), 'You own 1 of 3');
  window.document.querySelector('.bundle-games').insertAdjacentHTML('beforeend',
    '<div class="bundle-game-card"><h3 class="bundle-game-card__title">Paper Comet</h3></div>'
    + '<div class="bundle-game-card"><h3 class="bundle-game-card__title">Night Ferry</h3></div>');
  await settle();
  assert.equal(sent.length, 2);
  assert.equal(sent[1].length, 5, 'the summary counts the whole bundle');
  assert.equal(summaryText(window), 'You own 2 of 5');
  assert.equal(count(window, 'summary'), 1);
  assert.equal(count(window, 'owned'), 2);
});

test('a bundle of 450 games gets a mark on each and one summary, from one message', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-mega-bundle' };
  const cards = Array.from({ length: 450 }, (_, i) => `<div class="tier-item-view"><span class="item-title">Invented Game ${i + 1}</span></div>`).join('');
  const window = pageWindow(`<!doctype html><html><body><h1>Invented Mega Bundle</h1>${cards}</body></html>`, url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({
    status: 'ok',
    results: items.map((it) => (it.title === 'Invented Game 450' ? owned(it.title) : notOwned())),
  }));
  done(tt, window, ctl);
  await settle(300);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].length, 450);
  const marked = [...window.document.querySelectorAll('.tier-item-view')].filter((c) => ns.hasLabel(c)).length;
  assert.equal(marked, 450);
  assert.equal(count(window, 'owned'), 1);
  assert.equal(summaryText(window), 'You own 1 of 450');
});

test('when Game Vault cannot answer, a bundle says so in its summary and marks nothing as not owned', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-games-bundle' };
  const window = pageWindow(sample('humble-bundle.html'), url.current);
  const { ctl } = follow(window, url, async () => ({ status: 'unavailable', reason: 'unreachable' }));
  done(tt, window, ctl);
  await settle();
  assert.equal(summaryText(window), 'Game Vault unavailable');
  assert.equal(count(window, 'owned') + count(window, 'possible'), 0);
  assert.equal(count(window, 'none'), 5, 'checked, invisibly: not asked again on every change');
});
```

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/ownership-pages.test.mjs extension/tests/ownership-labels.test.mjs extension/tests/ownership-bundles.test.mjs`
Expected: FAIL (`ns.pageFor(...)` is null for bundle addresses; `ns.markModel is not a function`).

- [ ] **Step 3: Texts.** Add to `extension/_locales/en/messages.json`:

```json
  "markOwned": { "message": "Owned on $SYSTEMS$", "placeholders": { "systems": { "content": "$1", "example": "PC, PS4" } } },
  "markPendingKey": { "message": "Key not redeemed ($SYSTEMS$)", "placeholders": { "systems": { "content": "$1", "example": "PC" } } },
  "markPossible": { "message": "Maybe owned" },
  "summaryOwned": {
    "message": "You own $OWNED$ of $TOTAL$",
    "placeholders": { "owned": { "content": "$1", "example": "4" }, "total": { "content": "$2", "example": "9" } }
  },
  "summaryPossible": { "message": "$COUNT$ possible", "placeholders": { "count": { "content": "$1", "example": "2" } } }
```

and to `extension/_locales/es/messages.json`:

```json
  "markOwned": { "message": "Lo tienes en $SYSTEMS$", "placeholders": { "systems": { "content": "$1", "example": "PC, PS4" } } },
  "markPendingKey": { "message": "Clave sin canjear ($SYSTEMS$)", "placeholders": { "systems": { "content": "$1", "example": "PC" } } },
  "markPossible": { "message": "Puede que lo tengas" },
  "summaryOwned": {
    "message": "Tienes $OWNED$ de $TOTAL$",
    "placeholders": { "owned": { "content": "$1", "example": "4" }, "total": { "content": "$2", "example": "9" } }
  },
  "summaryPossible": { "message": "$COUNT$ posibles", "placeholders": { "count": { "content": "$1", "example": "2" } } }
```

- [ ] **Step 4: Bundle pages as data** (`extension/ownership/stores.js`)
  - Humble's `pages`, after its product page:

    ```js
        {
          kind: 'bundle',
          path: '^/games/[^/?#]+',
          items: { selectors: ['.tier-item-view'], title: ['.item-title', '.item-title-text'] },
          summary: ['.bundle-logo-text', '.heading-medium', 'h1'],
        },
    ```
  - Fanatical's `pages`, after its product page:

    ```js
        {
          kind: 'bundle',
          path: '^/[a-z]{2}(?:-[a-z]{2})?/(?:bundle|pick-and-mix)/[^/?#]+',
          items: { selectors: ['.bundle-game-card', '.pnm-product-card'], title: ['.bundle-game-card__title', '.card-title', 'h3', 'h4'] },
          summary: ['main h1', 'h1'],
        },
    ```

- [ ] **Step 5: Reading a bundle** (`extension/ownership/read.js`): in `readPage`, replace the final `return null; // a kind of page this version does not read` with

```js
    if (page.kind === 'bundle') {
      const summary = ns.first(doc, page.summary);
      const items = [];
      for (const element of doc.querySelectorAll(page.items.selectors.join(', '))) {
        const title = ns.textOf(ns.first(element, page.items.title));
        if (title) items.push({ item: { title }, element });
      }
      return summary && items.length ? { kind: 'bundle', items, summary } : null;
    }
    return null; // a kind of page this version does not read
```

- [ ] **Step 6: Marks and the summary** (`extension/ownership/label.js`, before `ns.renderLabel`)

```js
  /**
   * The mark on one game of a bundle: "Owned on PC", "Key not redeemed (PC)", "Maybe owned"; an
   * invisible "checked" mark when it is not owned or Game Vault could not answer (the summary says
   * which), so the game is not asked about on every change. Marks carry no link: store cards are
   * often links themselves.
   */
  ns.markModel = (answer, t) => {
    const r = answer?.status === 'ok' ? answer.result : null;
    if (r?.status === 'owned' && r.game) {
      const copies = r.game.copies ?? [];
      const systems = [...new Set(copies.map((c) => c.system || c.platform))].join(', ');
      const pendingOnly = copies.length > 0 && copies.every((c) => c.pendingKey);
      return { kind: 'owned', tone: 'owned', block: true, text: t(pendingOnly ? 'markPendingKey' : 'markOwned', [systems]), title: r.game.title };
    }
    if (r?.status === 'possible') {
      return { kind: 'possible', tone: 'possible', block: true, text: t('markPossible'), title: (r.candidates ?? []).map((c) => c.title).join('\n') };
    }
    return { kind: 'none' };
  };

  /**
   * The summary next to a bundle's heading: "You own 4 of 9" over its different titles (a game in two
   * tiers counts once), possible ones apart; unavailable when Game Vault could not answer.
   */
  ns.summaryModel = (outcome, items, t) => {
    if (outcome?.status !== 'ok') return { ...ns.unavailableModel(outcome?.reason, t), kind: 'summary' };
    const rank = { owned: 2, possible: 1 };
    const best = new Map();
    items.forEach((item, i) => {
      const key = (item.title ?? '').toLowerCase();
      const status = outcome.results?.[i]?.status;
      if (!best.has(key) || (rank[status] ?? 0) > (rank[best.get(key)] ?? 0)) best.set(key, status);
    });
    const count = (s) => [...best.values()].filter((v) => v === s).length;
    const text = t('summaryOwned', [String(count('owned')), String(best.size)]);
    const possible = count('possible');
    return {
      kind: 'summary',
      tone: count('owned') ? 'owned' : 'muted',
      text: possible ? `${text} · ${t('summaryPossible', [String(possible)])}` : text,
    };
  };
```

- [ ] **Step 7: The bundle flow** (`extension/ownership/content.js`)
  - Inside `ns.start`, before `async function scan()`:

    ```js
        // A bundle's summary counts every game on it, so all of them are asked about whenever new ones
        // appear (the background's cache answers the ones already asked).
        async function labelBundle(read, href) {
          const fresh = read.items.filter((e) => !ns.hasLabel(e.element));
          if (!fresh.length && ns.hasLabel(read.summary)) return;
          const items = read.items.map((e) => e.item);
          const outcome = await ask(items);
          if (url() !== href) return;
          read.items.forEach((e, i) => {
            if (!ns.hasLabel(e.element)) ns.renderLabel(doc, e.element, ns.markModel(answerAt(outcome, i), t));
          });
          for (const old of [...read.summary.children].filter((c) => c.getAttribute('data-gamevault') === 'summary')) old.remove();
          ns.renderLabel(doc, read.summary, ns.summaryModel(outcome, items, t));
        }
    ```
  - In `scan()`, right after `if (!read) return;`:

    ```js
            if (read.kind === 'bundle') {
              await labelBundle(read, href);
              return;
            }
    ```

- [ ] **Step 8: Run the extension tests**

Run: `docker run --rm -v "$PWD:/src" -w /src -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 extension/tests/*.test.mjs`
Expected: PASS.

- [ ] **Step 9: Test and commit**

Run: `task test` (expected: green).

```bash
git add extension
git commit -m "Connector: Humble and Fanatical bundle pages: a mark per owned game and \"You own N of M\""
```

---

### Task 10: Live checks: store selectors, the extension end to end

No new code unless a selector is wrong. The repository is public: nothing about the user's library, accounts or results goes into files, commits or the PR; results stay in the chat.

**Files:**
- Modify (only if a live page differs): `extension/ownership/stores.js` (selectors, first in each list), the matching `extension/tests/samples/*.html` (the real markup's shape, invented titles), and the header comment of `stores.js` (the check date).

**Interfaces:**
- Consumes: everything above. Produces: confirmed (or fixed) selector data.

- [ ] **Step 1: Steam, GOG and Humble selectors, read only and signed out, in the browser pane.** Navigate to each public page, then run the snippet with `javascript_tool` (it only reads):
  - Steam: `https://store.steampowered.com/app/620/` (any public app page; if an age gate shows, pick another app instead of entering a date):

    ```js
    ({ title: document.querySelector('#appHubAppName')?.textContent ?? null, byClass: document.querySelectorAll('.apphub_AppName').length })
    ```
    Expected: the game's title, `byClass` ≥ 1.
  - GOG: open `https://www.gog.com/en/games`, follow any game to its product page:

    ```js
    ({ id: document.querySelector('[card-product]')?.getAttribute('card-product') ?? null,
       title: document.querySelector('h1.productcard-basics__title')?.textContent.trim() ?? null })
    ```
    Expected: a numeric id and the title. If `id` is null, search the page for its product id (`document.documentElement.outerHTML.match(/card-product="(\d+)"|"productId":\s*"?(\d+)/)`) and point `id.selectors` / `id.attr` at the element that holds it; if no element does, delete the GOG `id` entry (title only) and change the GOG case of `ownership-pages.test.mjs` to expect `{ title: 'Echo Valley' }`.
  - Humble store: open `https://www.humblebundle.com/store`, follow any game:

    ```js
    ({ title: document.querySelector('h1.human_name-view')?.textContent.trim() ?? null,
       header: document.querySelector('.product-header-view h1')?.textContent.trim() ?? null })
    ```
  - Humble bundle: open `https://www.humblebundle.com/games`, follow any current games bundle:

    ```js
    ({ items: document.querySelectorAll('.tier-item-view').length,
       first: document.querySelector('.tier-item-view .item-title, .tier-item-view .item-title-text')?.textContent.trim() ?? null,
       summary: ['.bundle-logo-text', '.heading-medium', 'h1'].map((s) => [s, Boolean(document.querySelector(s))]) })
    ```
    Expected: items > 0, a title, at least one summary selector true. Scroll to the bottom first: tiers may load late.
  - Wherever a value is null: use `find` / `read_page` on the title or an item to see its element, pick a stable class or attribute (not a generated hash), put it first in that list in `stores.js`, and update the sample page to that shape.

- [ ] **Step 2: Fanatical, checked by the user.** Fanatical's terms forbid automated access, so the agent does not open it. Ask the user, in chat, to open a Fanatical game page and a bundle (or pick-and-mix) page in their own browser, open the developer console and run:

```js
({ product: ['h1.product-name', 'main h1'].map((s) => [s, document.querySelector(s)?.textContent.trim() ?? null]),
   cards: ['.bundle-game-card', '.pnm-product-card'].map((s) => [s, document.querySelectorAll(s).length]),
   cardTitle: ['.bundle-game-card__title', '.card-title', 'h3', 'h4'].map((s) => [s, document.querySelector('.bundle-game-card, .pnm-product-card')?.querySelector(s)?.textContent.trim() ?? null]) })
```

and paste the output. Adjust `stores.js` and `fanatical-*.html` as in Step 1 (if neither card selector matches, ask the user for the outer HTML of one card, with its title replaced by an invented one).

- [ ] **Step 3: Record the check.** In the header comment of `extension/ownership/stores.js`, replace "The selectors were checked on the live sites when this feature was built (see docs/technical.md)." with "Checked on the live sites on <today's date, YYYY-MM-DD>." Run `task test`.

- [ ] **Step 4: The extension end to end, in the user's Chrome.** `task test-server`. Ask the user to:
  1. reload the unpacked extension (`chrome://extensions` → Game Vault Connector → reload), open http://127.0.0.1:8093 and enable it there from the extension's icon if it is not yet;
  2. in Game Vault, System → Security → Access tokens, create "Extension, test"; in the extension's menu choose `http://127.0.0.1:8093`, paste the token, turn on **Show what I own on store pages** (the browser asks for the four sites);
  3. open a Steam, a GOG, a Humble product page and a Humble bundle, and a Fanatical product and bundle page, for a game in the test copy's library and one not in it; expected: "In your collection: …" (opens the sheet in a new tab), "Not in your collection", bundle marks and "You own N of M";
  4. stop the test server (`task test-server:stop`) and reload a store page: "Game Vault unavailable", with the hint on hover; start it again, revoke the token in System → Security, wait a minute and reload: "Game Vault unavailable" with the token hint;
  5. turn the option off: the labels are gone after a reload, and `chrome://extensions` → Details → Site access no longer lists the store sites (Humble stays if a sign-in uses it);
  6. optional: with the browser in Spanish, the labels and the menu are in Spanish.
  Fix what the user reports, with a test first where the behaviour is testable. Then `task test-server:stop`.

- [ ] **Step 5: Commit** (only if Steps 1–3 changed files)

```bash
git add extension
git commit -m "Connector: store selectors checked on the live sites"
```

---

### Task 11: Documentation, memory and the final check

**Files:**
- Modify: `docs/technical.md`, `README.md`, `extension/README.md`, `extension/PRIVACY.md`, `.claude/docs/ui.md`, `.claude/memory/connector.md`, `.claude/memory/security-model.md`, `.claude/MEMORY.md`

**Interfaces:**
- Consumes: everything above. Produces: documentation only.

- [ ] **Step 1: `docs/technical.md`**
  - **Repository layout**: `auth/` under `domain/` becomes "Access control: request identity, principals, settings, users, sessions, access tokens"; under `application/` add `ownership/        "Do I own it?": store links, exact titles, current copies (read only)`; after the `web/` block add `extension/            Browser extension (Game Vault Connector): sign-in recipes (lib/), store labels (ownership/), tests with a fake chrome`.
  - **Access control**, a new bullet after the session bullet: **Access tokens** (System → Security → Access tokens) let a program, the browser extension, look up what you own from a browser outside the trusted networks. A token is `gvt_` and 32 random bytes in base64url; only its SHA-256 is stored, and it is shown once. It is sent as `Authorization: Bearer gvt_…` and reaches only `OwnershipService/CheckOwnership`: any other request that carries one is refused as unauthenticated, even from a trusted network. The token alone identifies the caller (the session cookie and the network's trust are not used with it), so it never makes a request trusted, and the proxy and host-name rules are unchanged. Other `Authorization` headers, such as an authenticating proxy's, are ignored. Unknown tokens count towards the sign-in throttling (10 failures in 15 minutes, shared with passwords; while it lasts, even a valid token waits, HTTP 429); the last use is written at most once a minute; revoking takes effect on the next request (tokens are read from the database on every request).
  - **Security**: "session tokens as SHA-256 hashes" becomes "session and access tokens as SHA-256 hashes".
  - A new section after **Connecting sources with the browser extension**:

    ```markdown
    ## Do I own it?

    One lookup answers "do I already have it?" for store pages, bundles and pasted lists.

    - **Lookup.** `OwnershipService/CheckOwnership` (`internal/application/ownership`) takes up to 200
      items, each `{store, id}` (a link key such as `steam` or `gog` and the game's id there), a
      `{title}`, or both, and answers each, in order: **owned** (one game), **possible** (up to five
      candidates) or **not owned**. Matching: a store link equal to `{store, id}` (automatic links
      included), then the exact title (`game.SameTitle`, indexed by `game.TitleKey`), then the title
      without edition and store tags (`game.MatchKey`), which only ever says possible. Only current
      copies count: sold, given away and expired ones are left out, and a game with no other copy is
      not owned. An owned game comes with its copies (platform, system, kind, pending key) and
      `#/game/<id>`, the address that opens its sheet in the web UI. The lookup only reads (no
      provider or network calls, nothing saved); a request is at most 256 KiB.
    - **Bundle checker.** Library → **Do I own it?** cleans a pasted list (`web/src/lib/ownership.ts`:
      one title per line; bullets, numbering, prices, discounts and currency signs removed; lines
      marked as DLC or soundtrack skipped and counted; blank, overlong and duplicate lines dropped),
      checks it 200 titles at a time and lists every title as owned (badges, pending key, a link to
      the sheet), possible (its candidates) or not owned, with "You own N of M" and **Copy the ones I
      don't own** (possible ones are not copied). Nothing is saved; on phones the dialog is full
      screen, the list above the results.
    - **Store labels (browser extension).** With **Show what I own on store pages** on, the extension
      asks the browser for `store.steampowered.com`, `www.gog.com`, `www.humblebundle.com` and
      `www.fanatical.com`, and registers one content script per allowed site (`extension/ownership/`);
      turning it off unregisters them and gives back the sites no sign-in uses. What each page shows
      is data in `extension/ownership/stores.js`: Steam product pages give the AppID from
      `/app/<id>` and the title, GOG product pages the product id (`card-product`) and the title,
      Humble and Fanatical product pages the title, and Humble (`/games/…`) and Fanatical
      (`/bundle/…`, `/pick-and-mix/…`) bundle pages every game's title. A debounced MutationObserver
      follows single-page navigation and content drawn late. Product pages get a label after the
      title ("In your collection: PS4 · disc, Steam · key not redeemed", a link to the sheet in a new
      tab; "Possible: …"; a quieter "Not in your collection"); bundle pages a mark on each game owned
      ("Owned on PC", "Key not redeemed (PC)", "Maybe owned") and "You own N of M" by the heading,
      counting different titles. When Game Vault cannot answer (not running, an error, the token
      refused, not set up) a label says **Game Vault unavailable** with a hint, never "not in your
      collection". Labels live in closed shadow roots with their own styles. Only the background talks
      to Game Vault: it sends `{store, id, title}` to the chosen address (one of the enabled ones),
      with the access token and without cookies, 200 items per request, and reuses answers for a
      minute (changed settings or a refused token clear them). Texts follow the browser language
      (en, es). The selectors were checked on the live sites (date in `stores.js`); when a store
      changes its pages, fix `stores.js` and the matching sample in `extension/tests/samples/`.
    ```
  - **Browsing the library**: add one sentence: a `#/game/<id>` address opens that game's sheet; closing it returns to `#/library`.

- [ ] **Step 2: `README.md`.** After the "Keys are tracked" bullet, one bullet (no implementation details):

```markdown
- **Do I already own it?** Paste a bundle's games, or any list of titles, to see which ones you have.
  With the browser extension, Steam, GOG, Humble Bundle and Fanatical pages show whether a game is in
  your collection, and bundle pages how many of their games you own.
```

- [ ] **Step 3: `extension/README.md`.** After **First use**, a section:

```markdown
## What you own, on store pages

1. Only if this browser is not on one of Game Vault's trusted networks: in Game Vault, open
   **System → Security → Access tokens** and create one (it is shown once).
2. In the extension's menu, under **What you own, on store pages**, choose your Game Vault address,
   paste the token, and turn on **Show what I own on store pages**. The browser asks for Steam, GOG,
   Humble Bundle and Fanatical.
3. Open a game on one of those stores: a label next to its title says whether it is in your
   collection (click it to open the game in Game Vault). Humble and Fanatical bundles mark each game
   you own and say how many you have.

"Game Vault unavailable" means Game Vault did not answer or refused the token: hover over it to see
why. Turning the option off removes the labels and gives the store sites' access back.
```

In **First use**, step 1, "press **Enable on …**" stays (it is the button's English text); add "(in other languages the button is translated)".

- [ ] **Step 4: `extension/PRIVACY.md`.** Add after the first bullet:

```markdown
- **On store pages** (only with "Show what I own on store pages" on, and only on the Steam, GOG,
  Humble Bundle and Fanatical sites the browser allowed): it reads the page you are viewing, namely
  the game's id in its address or page and the game titles it shows, and sends only those, with your
  access token, to the Game Vault address you chose. It never sends anything to the store, never
  reads your account there, and keeps the answers in memory for a minute only.
```

and in **What it stores** add "the Game Vault address used for store pages and its access token (in the extension's local storage)".

- [ ] **Step 5: `.claude/docs/ui.md`.** In **Structure**: `features/library/`: add `OwnershipDialog` ("Do I own it?": paste box, rows, full screen on phones) and "`LibraryPage` opens the sheet a `#/game/<id>` address names"; add `features/system/AccessTokens` (create once-shown, list, revoke); `lib/ownership.ts` (paste cleaning, request chunks, rows: pure, Node-tested) and `lib/gameRoute.ts` (`#/game/<id>`); `components/ui.tsx`: `Modal` `phoneFull`.

- [ ] **Step 6: Project memory**
  - `.claude/memory/connector.md`, a new bullet: **Store labels (2026-10-12):** what store pages show is data in `extension/ownership/stores.js` (Steam AppID from `/app/<id>`, GOG `card-product`, Humble and Fanatical titles; bundles on Humble `/games/…`, Fanatical `/bundle/…` and `/pick-and-mix/…`); one content script per allowed store, registered dynamically and resynced on permission changes; labels in closed shadow roots; only the background calls `OwnershipService/CheckOwnership` (token, no cookies, 200 items per request, one-minute cache); "unavailable" is never "not owned". Fanatical's selectors are checked by the user only (its terms forbid automated access). The extension's DOM tests use happy-dom from `web/node_modules`. Link [[security-model]].
  - `.claude/memory/security-model.md`, a new bullet: **Access tokens (2026-10-12):** `gvt_` + 32 random bytes, only SHA-256 stored, shown once; `Authorization: Bearer gvt_…` reaches only `OwnershipService/CheckOwnership`; a token request ignores the session cookie and the trusted network; other `Authorization` headers are ignored (authenticating proxies forward their own); unknown tokens count towards the sign-in throttling; last use written at most once a minute; no cached validity, so revoking is immediate. A token never makes a request trusted.
  - `.claude/MEMORY.md`: the Connector line becomes "… spike 10/10 stores; when-conditions, per-host confirmation, not published; store labels and bundle marks from selector data"; the Security model line becomes "… never trust proxied or host-name requests; access tokens reach only the ownership lookup".

- [ ] **Step 7: The whole branch**

Run: `task lint`, `task test`, and the CI freshness check: `task generate && git status --porcelain proto internal/gen web/src/gen` (expected: nothing listed).

- [ ] **Step 8: Final browser pass.** `task test-server`, http://127.0.0.1:8093/?v=<a new number>, desktop then the `mobile` preset (reload): System → Security → Access tokens (create, copy, revoke), Library → **Do I own it?** (paste, results, copy, a link to a sheet, `#/game/<id>`). Reset the viewport with preset `desktop`, then `task test-server:stop`.

- [ ] **Step 9: Commit**

```bash
git add docs README.md extension/README.md extension/PRIVACY.md .claude
git commit -m "Docs: ownership lookup, access tokens, store labels; project memory"
```

---

## Self-review

- **Spec coverage.** Access tokens (create with a name, shown once, list with created and last used, revoke; hash only; ≥32 random bytes; `gvt_`) → Tasks 1, 4, 5. Scope (any other RPC refused as unauthenticated), Bearer transport, proxy and host-name rules unchanged, brute force counted with sign-ins, last used at most once a minute → Task 4. `OwnershipService/CheckOwnership`, items `{store, id}` / `{title}`, 200 items, link then automatic link then `SameTitle`, owned / possible (5 candidates) / not owned, copies summary with platform, system, kind and pending key, `#/game/<id>`, no side effects → Tasks 2, 3 (and `#/game/<id>` opening the sheet → Task 6). Extension popup token field and the store toggle with host permissions → Task 8. One content script per store registered only for granted hosts → Task 8. Steam AppID, GOG id or title, Humble and Fanatical titles, as data → Tasks 7, 9. MutationObserver, debounced → Tasks 7, 9. Background lookup with the token, cached per page for a short time → Task 8. Product label states and the click to the sheet in a new tab → Task 7. Bundle marks and summary → Task 9. "Game Vault unavailable", never "not in your collection" → Tasks 7, 8, 9. Shadow root isolation → Task 7. en/es → Tasks 7, 8, 9. Privacy file → Task 11. Bundle checker (paste cleaning helper with Node tests, rows, summary, copy the ones not owned, nothing saved, full screen on phones, en/es) → Task 6. Tests: Go (Tasks 1–4), extension with fake chrome and sample pages (Tasks 7–9), web Node (Task 6), browser (Tasks 5, 6, 10, 11). Delivery docs, README line, memory → Task 11.
- **Placeholders.** None in code. Task 10 is a check whose edits depend on what the live pages show; it says exactly what to run, what to expect and where each fix goes. The only value filled at execution time is the check date in a comment.
- **Names used across tasks.** Go: `auth.TokenPrefix`, `auth.TokenBytes`, `auth.AccessToken`, `auth.NewAccessToken`, `auth.HashToken`, `auth.IsAccessToken`, `auth.MethodToken`, `Principal.TokenID`, repository `AccessTokens` / `AccessTokenByHash` / `SaveAccessToken` / `TouchAccessToken` / `DeleteAccessToken`; `game.TitleKey`; `ownership.MaxItems`, `ErrTooManyItems`, `Status*`, `Item`, `Copy`, `Match`, `Result`, `Catalog`, `NewService`, `Check`; `appauth` `CreateAccessToken`, `AccessTokens`, `RevokeAccessToken`, `AuthenticateToken`; rpc `NewOwnershipHandler`, `Handlers.Ownership`, `tokenProcedures`, `accessToken`, `serveWithToken`, `denyThrottled`. Proto: `OwnershipService.CheckOwnership`, `OwnershipItem`, `OwnedCopy`, `OwnedGame`, `OwnershipResult`; `AuthService.ListAccessTokens` / `CreateAccessToken` / `RevokeAccessToken`, `AccessToken`. Web: `ownershipClient`, `cleanTitles`, `chunk`, `toRows`, `summarize`, `notOwnedText`, `MAX_CHECK_ITEMS`, `gameIdFromHash`, `OwnershipDialog`, `AccessTokens`, `Modal.phoneFull`. Extension: `gvOwnership.STORES`, `storeForUrl`, `pageFor`, `first`, `textOf`, `hasLabel`, `readPage`, `copiesText`, `unavailableModel`, `labelModel`, `markModel`, `summaryModel`, `renderLabel`, `rootOf`, `start`; `lib/ownership.js` `MAX_ITEMS`, `MAX_PAGE_ITEMS`, `CACHE_MS`, `storeOrigin`, `STORE_ORIGINS`, `scriptId`, `scriptFor`, `grantedStores`, `removableOrigins`, `syncStoreScripts`, `createLookup`; storage `ownership: { enabled, origin, token }`; message `{ op: 'ownership', items }`.
- **Review Focus.** Each of the five lines has its test in the owning task (Tasks 4, 6, 7, 8, 9), named in the line.
