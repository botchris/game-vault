# Game Vault Connector — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect a source in one click: a generic browser extension runs a declarative recipe that each source declares, captures the credential from the store's own sign-in and hands it to the Game Vault tab, which fills the field, tests and saves.

**Architecture:** Recipes are data (`schema.SignInRecipe`, JSON) attached to credential fields in Go and sent to the page with the source descriptions. The extension (`extension/`, plain JS, Manifest V3) validates a recipe and runs it with five capture primitives and an optional readiness condition. Its engine is a pure module tested in Node with a fake browser. A bridge content script, registered only on Game Vault addresses the user enabled, relays page messages over a port. The source dialog detects the extension and offers Connect. Every credential field also accepts whatever a user would naturally paste.

**Tech Stack:** Go 1.26, ConnectRPC/buf, React 19 + TypeScript, plain JavaScript (ES modules) for the extension, Node 24 test runner, testify.

**Spec:** `docs/superpowers/specs/2026-10-10-connector-extension-design.md`

## Decisions made while planning (record them in the spec in Task 7)

- **Readiness (`when`).** Recipes gain an optional readiness condition: `urlPrefix` (the tab is on that address), `contains` (the captured value contains it) and `fetch` (a JSON field is present when fetched with the session). Cookies and storage can exist before the user signs in; without a condition the extension would capture anonymous values and close the tab before sign-in.
- **Enabling from the popup.** "Enable on this address" lives in the extension's popup. A web page cannot make the extension ask the browser for a permission. The dialog explains where to click.
- **Timeout in JSON.** The recipe's timeout is `timeoutSeconds` (int) in its JSON contract.
- **Ports instead of one-shot messages.** The bridge talks to the background over a port (`runtime.connect`), so a long sign-in keeps the service worker alive. If the port drops, the page gets an error instead of hanging.

## Global Constraints

- Everything runs in the toolchain container through Task (`task lint`, `task test`, `task generate`, `task go -- test ./pkg/ -run X -v`); never Go or Node on the host.
- Load the `write-go` skill before writing Go. `task lint` enforces one struct field per line, blank lines between documented members and docs on interface methods, golangci-lint.
- Recipes are **data, never code**. The extension never evaluates anything it receives (no `eval`, `new Function`, or code `scripting.executeScript` strings from the page).
- Every recipe address is `https://`. A capture may only read the opened host or hosts the recipe lists in `hosts`, and both Go and the extension check it.
- The extension has **no host permissions at install** (`optional_host_permissions` only). Each Game Vault address is enabled from the popup. Each store host is confirmed by the user in the extension's own window, and the browser grants it then.
- Captured values go only to the requesting tab, in the reply. The extension never stores or logs them.
- Recipe version `1`. The engine limits a sign-in to `timeoutSeconds` (default 300, maximum 600) and allows one sign-in at a time per Game Vault tab.
- Every UI text through `t()` with keys in `en.json` and `es.json`; repository text in English.
- Real-store checks: on a **copy** of the config only Humble and Steam (rotating credentials); the rest on the user's real server after merging.
- Branch `feature/connector-extension`, one PR to `main`, nothing published to extension stores, no tags.

## Review Focus

- A user not yet signed in must never have anonymous cookies or storage captured. Every cookie, cookies and storage recipe needs a readiness condition, or a capture that only exists once signed in (test in Task 2 and Task 4).
- A message posted by any other page or origin must be refused by the bridge and the background (tests in Task 4; manual check in Task 5).
- A recipe asking to capture from a host it does not open or list must be rejected by both Go and the extension (tests in Task 1 and Task 4).
- Closing the store's tab or the confirmation window, or the service worker stopping, must end in a clear error in the dialog, never a hang (tests in Task 4; page timeout in Task 6).
- A recipe newer than the installed extension must say "update the extension", not fail obscurely (tests in Task 4 and Task 6).

---

## File structure

| File | Responsibility |
| --- | --- |
| `internal/domain/schema/signin.go`, `signin_test.go` (create) | `SignInRecipe`, `Capture`, `When`, `Validate` |
| `internal/domain/schema/schema.go` (modify) | `Field.SignIn` |
| `internal/adapters/outbound/{humble,battlenet,eaapp,playstation,ubisoft,fanatical,epic,gog,xbox,amazon}/provider.go` (modify) | Each credential field's recipe |
| `internal/application/plugin/plugintest/plugintest.go`, `plugintest_test.go` (modify) | Recipes validated for every source |
| `proto/gamevault/v1/source.proto` (modify) + generated, `internal/adapters/inbound/rpc/mapper.go`, `mapper_internal_test.go` (create) | `SettingField.sign_in` (recipe JSON, `open` filled from the help link) |
| `internal/adapters/outbound/humble/paste_test.go`, `playstation/paste_test.go`, … (create/modify) | "Paste anything" per source |
| `extension/manifest.json`, `background.js`, `bridge.js`, `popup.html`, `popup.js`, `confirm.html`, `confirm.js`, `package.json`, `README.md`, `PRIVACY.md` (create) | The extension |
| `extension/lib/recipe.js`, `capture.js`, `engine.js` (create), `extension/tests/*.test.mjs` (create) | Pure logic and its Node tests |
| `Taskfile.yml` (modify) | Extension tests in `task test`, `task extension:pack` |
| `web/src/lib/connector.ts` (create), `web/src/features/sources/SourceDialog.tsx`, `web/src/styles.css`, `web/src/i18n/locales/{en,es}.json` (modify) | Page side |
| `docs/technical.md`, `docs/plugins.md`, `README.md`, `.claude/memory/connector.md`, `.claude/MEMORY.md`, the spec (modify/create) | Docs |

---

### Task 1: Recipe types and validation (Go)

**Files:**
- Create: `internal/domain/schema/signin.go`, `internal/domain/schema/signin_test.go`
- Modify: `internal/domain/schema/schema.go`

**Interfaces:**
- Produces:
  - `const schema.RecipeVersion = 1`
  - `type SignInRecipe struct { Version int; Open string; Private bool; Hosts []string; TimeoutSeconds int; When *When; Capture Capture }` with JSON tags `version, open, private, hosts, timeoutSeconds, when, capture` (empty ones omitted)
  - `type When struct { URLPrefix string; Contains string; Fetch *FetchCapture }` (JSON `urlPrefix, contains, fetch`)
  - `type Capture struct { Cookie *CookieCapture; Cookies *CookiesCapture; Storage *StorageCapture; Redirect *RedirectCapture; Fetch *FetchCapture }` (JSON `cookie, cookies, storage, redirect, fetch`)
  - `CookieCapture{URL, Name}`, `CookiesCapture{URL}`, `StorageCapture{Origin, Key, Path}`, `RedirectCapture{Prefix, Param}`, `FetchCapture{URL, Field}` (JSON camelCase: `url, name, origin, key, path, prefix, param, field`)
  - `func (r SignInRecipe) Validate(helpURL string) error` (an empty `Open` uses `helpURL`)
  - `func (r SignInRecipe) WithOpen(helpURL string) SignInRecipe` (fills an empty `Open`)
  - `Field.SignIn *SignInRecipe`

- [ ] **Step 1: Write the failing tests** — `signin_test.go`:

```go
package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func humbleRecipe() SignInRecipe {
	return SignInRecipe{
		Version: 1,
		Open:    "https://www.humblebundle.com/home/keys",
		When:    &When{URLPrefix: "https://www.humblebundle.com/home/keys"},
		Capture: Capture{Cookie: &CookieCapture{URL: "https://www.humblebundle.com", Name: "_simpleauth_sess"}},
	}
}

func TestSignInRecipe_validate(t *testing.T) {
	t.Run("GIVEN well-formed recipes of every kind", func(t *testing.T) {
		ok := []SignInRecipe{
			humbleRecipe(),
			{Version: 1, Open: "https://account.battle.net/overview", When: &When{URLPrefix: "https://account.battle.net/overview"},
				Capture: Capture{Cookies: &CookiesCapture{URL: "https://account.battle.net/overview"}}},
			{Version: 1, Open: "https://www.ea.com/", Hosts: []string{"accounts.ea.com"},
				When:    &When{Fetch: &FetchCapture{URL: "https://accounts.ea.com/connect/auth?x=1", Field: "access_token"}},
				Capture: Capture{Cookies: &CookiesCapture{URL: "https://accounts.ea.com/connect/auth?x=1"}}},
			{Version: 1, Open: "https://connect.ubisoft.com/login?appId=x", Private: true,
				Capture: Capture{Storage: &StorageCapture{Origin: "https://connect.ubisoft.com", Key: "PRODrememberMe", Path: "/ready"}}},
			{Version: 1, Open: "https://auth.gog.com/auth?x=1", Hosts: []string{"embed.gog.com"},
				Capture: Capture{Redirect: &RedirectCapture{Prefix: "https://embed.gog.com/on_login_success", Param: "code"}}},
			{Version: 1, Open: "https://www.epicgames.com/id/login?x=1",
				Capture: Capture{Fetch: &FetchCapture{URL: "https://www.epicgames.com/id/api/redirect?x=1", Field: "authorizationCode"}}},
		}

		t.Run("THEN each is valid", func(t *testing.T) {
			for _, r := range ok {
				assert.NoError(t, r.Validate(""), r.Open)
			}
		})
	})

	t.Run("GIVEN a recipe whose address comes from the field's help link (Amazon)", func(t *testing.T) {
		r := SignInRecipe{Version: 1, Capture: Capture{Redirect: &RedirectCapture{Prefix: "https://www.amazon.com/", Param: "openid.oa2.authorization_code"}}}

		t.Run("THEN it is valid with an https help link, and WithOpen fills it", func(t *testing.T) {
			assert.NoError(t, r.Validate("https://www.amazon.com/ap/signin?x=1"))
			assert.Error(t, r.Validate(""))
			assert.Equal(t, "https://www.amazon.com/ap/signin?x=1", r.WithOpen("https://www.amazon.com/ap/signin?x=1").Open)
		})
	})

	t.Run("GIVEN broken recipes", func(t *testing.T) {
		bad := map[string]func(*SignInRecipe){
			"a newer version":              func(r *SignInRecipe) { r.Version = 2 },
			"no version":                   func(r *SignInRecipe) { r.Version = 0 },
			"an http address":              func(r *SignInRecipe) { r.Open = "http://www.humblebundle.com/" },
			"no capture":                   func(r *SignInRecipe) { r.Capture = Capture{} },
			"two captures":                 func(r *SignInRecipe) { r.Capture.Fetch = &FetchCapture{URL: "https://www.humblebundle.com/x", Field: "a"} },
			"a capture on another host":    func(r *SignInRecipe) { r.Capture.Cookie.URL = "https://evil.example" },
			"a condition on another host":  func(r *SignInRecipe) { r.When.URLPrefix = "https://evil.example/" },
			"a cookie without a name":      func(r *SignInRecipe) { r.Capture.Cookie.Name = "" },
			"a malformed host":             func(r *SignInRecipe) { r.Hosts = []string{"https://x.com/"} },
			"a timeout over ten minutes":   func(r *SignInRecipe) { r.TimeoutSeconds = 601 },
			"a negative timeout":           func(r *SignInRecipe) { r.TimeoutSeconds = -1 },
		}

		t.Run("THEN each is refused", func(t *testing.T) {
			for name, change := range bad {
				r := humbleRecipe()
				change(&r)
				assert.Error(t, r.Validate(""), name)
			}
		})
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/domain/schema/ -run TestSignInRecipe`
Expected: FAIL (build: `SignInRecipe` undefined).

- [ ] **Step 3: Implement** — `internal/domain/schema/signin.go`:

```go
package schema

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// RecipeVersion is the version of the recipe format and of the primitives it uses. The browser
// extension refuses recipes newer than the ones it knows (the user then updates it).
const RecipeVersion = 1

// maxTimeoutSeconds bounds how long the extension waits for the user to sign in.
const maxTimeoutSeconds = 600

var reHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// SignInRecipe tells the Game Vault Connector browser extension how to collect a setting's value
// from the store's own sign-in: what to open, when the user counts as signed in, and what to
// capture then. It is data the extension interprets, never code.
type SignInRecipe struct {
	Version int    `json:"version"`
	Open    string `json:"open,omitempty"` // empty: the field's HelpURL (a per-run link such as Amazon's)

	// Private prefers a private window when the user allowed the extension in one (Ubisoft: its
	// ticket rotates when Game Vault uses it, which would sign the normal browser out).
	Private bool `json:"private,omitempty"`

	// Hosts are other hosts the capture or condition may read (EA's accounts.ea.com); the user
	// confirms each one.
	Hosts          []string `json:"hosts,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"` // 0: five minutes

	// When is the readiness condition; nil: the capture itself is the condition (a redirect, a
	// fetch whose field only exists once signed in).
	When    *When   `json:"when,omitempty"`
	Capture Capture `json:"capture"`
}

// When tells the extension that the user is signed in. Every condition set must hold.
type When struct {
	URLPrefix string        `json:"urlPrefix,omitempty"` // the tab is on an address starting so
	Contains  string        `json:"contains,omitempty"`  // the captured value contains this text
	Fetch     *FetchCapture `json:"fetch,omitempty"`     // this field is present when fetched with the session
}

// Capture is what the extension reads once the user is signed in: exactly one member is set.
type Capture struct {
	Cookie   *CookieCapture   `json:"cookie,omitempty"`
	Cookies  *CookiesCapture  `json:"cookies,omitempty"`
	Storage  *StorageCapture  `json:"storage,omitempty"`
	Redirect *RedirectCapture `json:"redirect,omitempty"`
	Fetch    *FetchCapture    `json:"fetch,omitempty"`
}

// CookieCapture reads one cookie's value (HttpOnly ones included).
type CookieCapture struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// CookiesCapture reads every cookie sent to an address, as a Cookie header ("a=1; b=2").
type CookiesCapture struct {
	URL string `json:"url"`
}

// StorageCapture reads a localStorage value from the tab when it is on Origin (and Path, if set).
type StorageCapture struct {
	Origin string `json:"origin"`
	Key    string `json:"key"`
	Path   string `json:"path,omitempty"`
}

// RedirectCapture reads a query parameter of the address the store redirects to.
type RedirectCapture struct {
	Prefix string `json:"prefix"`
	Param  string `json:"param"`
}

// FetchCapture reads a field of the JSON an address answers when fetched with the user's session.
type FetchCapture struct {
	URL   string `json:"url"`
	Field string `json:"field"`
}

// WithOpen returns the recipe with an empty Open replaced by helpURL.
func (r SignInRecipe) WithOpen(helpURL string) SignInRecipe {
	if r.Open == "" {
		r.Open = helpURL
	}

	return r
}

// Validate checks the recipe the way the extension does: a known version, https addresses only,
// exactly one capture, and every address on the opened host or a listed one.
func (r SignInRecipe) Validate(helpURL string) error {
	r = r.WithOpen(helpURL)
	if r.Version < 1 || r.Version > RecipeVersion {
		return fmt.Errorf("sign-in recipe version %d is not supported", r.Version)
	}

	if r.TimeoutSeconds < 0 || r.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("sign-in recipe timeout must be between 0 and %d seconds", maxTimeoutSeconds)
	}

	open, err := httpsHost(r.Open)
	if err != nil {
		return err
	}

	allowed := map[string]bool{open: true}

	for _, h := range r.Hosts {
		if !reHost.MatchString(h) {
			return fmt.Errorf("sign-in recipe host %q is not a host name", h)
		}

		allowed[h] = true
	}

	addresses, err := r.Capture.addresses()
	if err != nil {
		return err
	}

	if r.When != nil {
		if r.When.URLPrefix != "" {
			addresses = append(addresses, r.When.URLPrefix)
		}

		if f := r.When.Fetch; f != nil {
			if f.Field == "" {
				return errors.New("sign-in recipe condition needs a field")
			}

			addresses = append(addresses, f.URL)
		}
	}

	for _, a := range addresses {
		h, err := httpsHost(a)
		if err != nil {
			return err
		}

		if !allowed[h] {
			return fmt.Errorf("sign-in recipe reads %s, which it neither opens nor lists", h)
		}
	}

	return nil
}

// addresses returns the capture's addresses, checking that exactly one capture is set and complete.
func (c Capture) addresses() ([]string, error) {
	var (
		set  int
		out  []string
		miss bool
	)

	if v := c.Cookie; v != nil {
		set++
		out = append(out, v.URL)
		miss = v.Name == ""
	}

	if v := c.Cookies; v != nil {
		set++
		out = append(out, v.URL)
	}

	if v := c.Storage; v != nil {
		set++
		out = append(out, v.Origin)
		miss = miss || v.Key == ""
	}

	if v := c.Redirect; v != nil {
		set++
		out = append(out, v.Prefix)
		miss = miss || v.Param == ""
	}

	if v := c.Fetch; v != nil {
		set++
		out = append(out, v.URL)
		miss = miss || v.Field == ""
	}

	switch {
	case set != 1:
		return nil, errors.New("a sign-in recipe needs exactly one capture")
	case miss:
		return nil, errors.New("the sign-in recipe's capture is incomplete")
	}

	return out, nil
}

func httpsHost(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("sign-in recipe address %q must be an https address", address)
	}

	return strings.ToLower(u.Hostname()), nil
}
```

In `schema.go`, `Field` gains (after `HelpURL`, documented):

```go
	// SignIn lets the browser extension fill this setting from the store's own sign-in; nil when
	// the value cannot be captured that way.
	SignIn *SignInRecipe
```

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/domain/schema/`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
git add internal/domain/schema
git commit -m "Schema: sign-in recipes the browser extension runs to fill a setting"
```

---

### Task 2: Each source declares its recipe; recipes reach the page

**Files:**
- Modify: the ten providers' `Descriptor()` (`internal/adapters/outbound/{humble,battlenet,eaapp,playstation,ubisoft,fanatical,epic,gog,xbox,amazon}/provider.go`), `internal/application/plugin/plugintest/plugintest.go`, `plugintest_test.go`, `proto/gamevault/v1/source.proto` (+ generated), `internal/adapters/inbound/rpc/mapper.go`
- Create: `internal/adapters/inbound/rpc/mapper_internal_test.go`

**Interfaces:**
- Consumes: `schema.SignInRecipe`, `Validate`, `WithOpen` (Task 1).
- Produces: `SettingField.sign_in` (string, the recipe's JSON with `open` filled), field 7; recipes on every credential field.

- [ ] **Step 1: Write the failing tests** — `mapper_internal_test.go`:

```go
package rpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

func TestFieldsToPB_signIn(t *testing.T) {
	t.Run("GIVEN a field whose recipe opens the field's own help link", func(t *testing.T) {
		fields := schema.Fields{{
			Key:     "code",
			Kind:    schema.FieldSecret,
			HelpURL: "https://www.amazon.com/ap/signin?run=42",
			SignIn: &schema.SignInRecipe{
				Version: 1,
				Capture: schema.Capture{Redirect: &schema.RedirectCapture{Prefix: "https://www.amazon.com/", Param: "openid.oa2.authorization_code"}},
			},
		}, {Key: "plain", Kind: schema.FieldText}}

		t.Run("WHEN it is sent to the page", func(t *testing.T) {
			out := fieldsToPB(fields)

			t.Run("THEN the recipe travels as JSON with today's link, and plain fields have none", func(t *testing.T) {
				var r map[string]any
				require.NoError(t, json.Unmarshal([]byte(out[0].SignIn), &r))
				assert.Equal(t, "https://www.amazon.com/ap/signin?run=42", r["open"])
				assert.InDelta(t, 1, r["version"], 0)
				assert.Empty(t, out[1].SignIn)
			})
		})
	})
}
```

and in `plugintest_test.go`, a source whose credential field has a recipe capturing from another host is reported, and a cookie recipe without a `When` is reported (anonymous cookies would be captured):

```go
type recipeSource struct{ recipe schema.SignInRecipe }

func (r recipeSource) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           "recipe",
		Name:           "Recipe",
		DescriptionKey: "sources.recipe.description",
		Fields: source.Fields{{
			Key:      "session",
			LabelKey: "sources.recipe.session",
			Kind:     schema.FieldSecret,
			Required: true,
			SignIn:   &r.recipe,
		}},
	}
}
func (recipeSource) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) { return nil, nil, nil }
func (recipeSource) Test(context.Context, source.Settings) error                                 { return nil }

func TestProblems_recipes(t *testing.T) {
	tr := Translations{"en": {"sources.recipe.description": true, "sources.recipe.session": true}}
	check := func(r schema.SignInRecipe) []string {
		return Problems(plugin.Plugin{ID: "recipe", Name: "Recipe", Sources: []sync.Provider{recipeSource{r}}}, tr)
	}

	t.Run("GIVEN a recipe reading another host THEN it is reported", func(t *testing.T) {
		r := schema.SignInRecipe{Version: 1, Open: "https://a.example.com/", When: &schema.When{URLPrefix: "https://a.example.com/"},
			Capture: schema.Capture{Cookie: &schema.CookieCapture{URL: "https://b.example.com/", Name: "s"}}}
		assert.NotEmpty(t, check(r))
	})

	t.Run("GIVEN a cookie recipe with no readiness condition THEN it is reported", func(t *testing.T) {
		r := schema.SignInRecipe{Version: 1, Open: "https://a.example.com/",
			Capture: schema.Capture{Cookie: &schema.CookieCapture{URL: "https://a.example.com/", Name: "s"}}}
		assert.NotEmpty(t, check(r))
	})

	t.Run("GIVEN a sound recipe THEN nothing is reported", func(t *testing.T) {
		r := schema.SignInRecipe{Version: 1, Open: "https://a.example.com/", When: &schema.When{URLPrefix: "https://a.example.com/"},
			Capture: schema.Capture{Cookie: &schema.CookieCapture{URL: "https://a.example.com/", Name: "s"}}}
		assert.Empty(t, check(r))
	})
}
```

(Add the imports the file lacks: `gamevault/internal/application/sync`, `gamevault/internal/domain/source`. If `source.Fields` is an alias of `schema.Fields`, as `source.Field = schema.Field` suggests, the literal compiles; otherwise use `schema.Fields`.)

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/adapters/inbound/rpc/ -run TestFieldsToPB_signIn` and `task go -- test ./internal/application/plugin/...`
Expected: FAIL (build: `SignIn` field on `pb.SettingField` undefined; recipe problems not reported).

- [ ] **Step 3: Proto and mapper** — `source.proto`, in `SettingField`:

```proto
  // How the Game Vault Connector browser extension fills this field from the store's own
  // sign-in, as JSON (schema.SignInRecipe); empty when it cannot.
  string sign_in = 7;
```

Run `task generate`. In `fieldsToPB`, per field:

```go
		var signIn string
		if f.SignIn != nil {
			if b, err := json.Marshal(f.SignIn.WithOpen(f.HelpURL)); err == nil {
				signIn = string(b)
			}
		}
```

and `SignIn: signIn,` in the literal (import `encoding/json`).

- [ ] **Step 4: plugintest** — in `checker.fields` (or where source fields are checked), for each public field with a recipe:

```go
		if r := f.SignIn; r != nil {
			if err := r.Validate(f.HelpURL); err != nil {
				c.add("%s, field %s: %v", where, f.Key, err)
			}

			// Cookies and storage can exist before the user signs in: without a condition the
			// extension would capture anonymous values.
			anonymous := r.Capture.Cookie != nil || r.Capture.Cookies != nil || r.Capture.Storage != nil
			if anonymous && r.When == nil && (r.Capture.Storage == nil || r.Capture.Storage.Path == "") {
				c.add("%s, field %s: a cookie or storage recipe needs a readiness condition (when)", where, f.Key)
			}
		}
```

(A storage capture with a `Path` that only exists after sign-in, like Ubisoft's `/ready`, counts as its own condition.)

- [ ] **Step 5: The recipes** — add `SignIn` to each credential field in the providers' descriptors. Use the providers' existing constants (`LoginURL`, `NPSSOURL`, `LoginCheckURL`, …); the values are the spike's (spec table):

| Provider (field) | Recipe |
|---|---|
| humble (`_simpleauth_sess` field) | `Open: "https://www.humblebundle.com/home/keys"`, `When: &schema.When{URLPrefix: "https://www.humblebundle.com/home/keys"}`, `Capture: Cookie{URL: "https://www.humblebundle.com", Name: "_simpleauth_sess"}` |
| battlenet (`cookies`) | `Open: defaultAccountURL + "/overview"`, `When: URLPrefix defaultAccountURL + "/overview"`, `Capture: Cookies{URL: defaultAccountURL + "/overview"}` |
| eaapp (`cookies`) | `Open: "https://www.ea.com/"`, `Hosts: []string{"accounts.ea.com"}`, `When: Fetch{URL: LoginCheckURL, Field: "access_token"}`, `Capture: Cookies{URL: LoginCheckURL}` |
| playstation (`npsso`) | `Open: "https://www.playstation.com/"`, `Hosts: []string{"ca.account.sony.com"}`, `Capture: Fetch{URL: NPSSOURL, Field: "npsso"}` |
| ubisoft (login data) | `Open: LoginURL`, `Private: true`, `Capture: Storage{Origin: "https://connect.ubisoft.com", Key: "PRODrememberMe", Path: "/ready"}` |
| fanatical (`bsauth` field) | `Open: defaultBaseURL + "/en/"`, `When: &schema.When{Contains: "\"authenticated\":true"}`, `Capture: Storage{Origin: defaultBaseURL, Key: "bsauth"}` |
| epic (`authCode`) | `Open: LoginURL`, `Capture: Fetch{URL: "https://www.epicgames.com/id/api/redirect?clientId=" + clientID + "&responseType=code", Field: "authorizationCode"}` |
| gog (`authCode`) | `Open: LoginURL`, `Hosts: []string{"embed.gog.com"}`, `Capture: Redirect{Prefix: "https://embed.gog.com/on_login_success", Param: "code"}` |
| xbox (`code`) | `Open: LoginURL`, `Capture: Redirect{Prefix: redirectURI, Param: "code"}` |
| amazon (`code`) | no `Open` (the per-run link in `HelpURL`), `Capture: Redirect{Prefix: "https://www.amazon.com/", Param: "openid.oa2.authorization_code"}` |

All with `Version: schema.RecipeVersion`. Check that `defaultBaseURL` (Fanatical) is `https://www.fanatical.com`; if its value differs, use the literal origin. If the capture host differs from the opened host in a way the table misses, add it to `Hosts` (plugintest reports it).

- [ ] **Step 6: Run the tests**

Run: `task go -- test ./...`
Expected: PASS, including `TestPlugins` over every source's recipe.

- [ ] **Step 7: Lint, test, commit**

Run: `task lint && task test`

```bash
git add internal proto web/src/gen
git commit -m "Sources declare how the browser extension can fill their credentials; recipes reach the page"
```

---

### Task 3: "Paste anything"

**Files:**
- Modify: `internal/adapters/outbound/humble/provider.go`, `internal/adapters/outbound/playstation/provider.go`
- Create or modify: an internal `paste_test.go` in each of `humble`, `epic`, `gog`, `xbox`, `playstation`, `amazon`, `fanatical`, `ubisoft`

**Interfaces:**
- Consumes: existing cleaners `humble.cleanCookie`, `epic.cleanCode`, `gog.cleanCode`, `xbox.cleanCode`, `playstation.cleanNPSSO`, `amazon.cleanCode`, `fanatical.token`, `ubisoft.parseLoginData`, and `browsersession.Parse`.
- Produces: Humble accepts a whole `Cookie` header; PlayStation accepts `npsso=…` (a cookie pair or header).

- [ ] **Step 1: Write the failing tests** — table tests of what a user might paste. For each package, an internal test file (`package humble`, …). Humble and PlayStation get new formats; the others pin what already works. Each format gets one case.

```go
// internal/adapters/outbound/humble/paste_test.go
package humble

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanCookie_pasted(t *testing.T) {
	for pasted, want := range map[string]string{
		"abc123":                                     "abc123",
		`"abc123"`:                                   "abc123",
		"_simpleauth_sess=abc123;":                   "abc123",
		" abc123 \n":                                 "abc123",
		"csrf_cookie=x; _simpleauth_sess=abc123; y=2": "abc123",
		"Cookie: a=1; _simpleauth_sess=abc123":        "abc123",
	} {
		assert.Equal(t, want, cleanCookie(pasted), pasted)
	}
}
```

```go
// internal/adapters/outbound/playstation/paste_test.go
package playstation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanNPSSO_pasted(t *testing.T) {
	const v = "Fx1aB2cD3eF4gH5iJ6kL7mN8oP9qR0sT1uV2wX3yZ4aB5cD6eF7gH8iJ9kL0mN1o"
	for _, pasted := range []string{
		v,
		`"` + v + `"`,
		`{"npsso":"` + v + `"}`,
		"npsso=" + v,
		"Cookie: a=1; npsso=" + v + "; b=2",
	} {
		assert.Equal(t, v, cleanNPSSO(pasted), pasted)
	}
}
```

For `epic` (bare code, JSON with `authorizationCode`, quotes), `gog` and `xbox` (full address, address with other parameters, bare code), `amazon` (full amazon.com address with `openid.oa2.authorization_code`, bare code), `fanatical` (whole `bsauth` JSON, bare token, token in quotes) and `ubisoft` (bare ticket, ticket in quotes, the JSON the existing tests use): read each cleaner and its existing tests first, and add only the formats not already covered, each as a table row. The expected value is what the source needs.

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/humble/ ./internal/adapters/outbound/playstation/ -run 'pasted'`
Expected: FAIL on the Cookie-header rows (Humble) and the `npsso=` rows (PlayStation); the pinned rows of the other packages pass already (that is fine: they pin behavior).

- [ ] **Step 3: Implement** — Humble:

```go
// cleanCookie accepts the value as copied from the browser: the bare value, "_simpleauth_sess=…",
// a whole Cookie header, surrounding quotes and a trailing ";".
func cleanCookie(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "=") {
		if s, ok := browsersession.Parse(v)["_simpleauth_sess"]; ok {
			v = s
		}
	}

	v = strings.TrimSuffix(v, ";")

	return strings.Trim(strings.TrimSpace(v), `"`)
}
```

(import `gamevault/internal/adapters/outbound/browsersession`; if `Parse` drops quotes or keeps them, the final `Trim` handles both.) PlayStation:

```go
func cleanNPSSO(v string) string {
	v = strings.TrimSpace(v)
	if m := reNPSSO.FindStringSubmatch(v); m != nil {
		return m[1]
	}

	if strings.Contains(v, "npsso=") {
		if s, ok := browsersession.Parse(v)["npsso"]; ok {
			return strings.Trim(s, `"' `)
		}
	}

	return strings.Trim(v, `"' `)
}
```

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/...`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
git add internal/adapters/outbound
git commit -m "Credential fields accept whatever is naturally copied: whole Cookie headers included"
```

---

### Task 4: Extension logic (pure modules, Node tests)

**Files:**
- Create: `extension/package.json`, `extension/lib/recipe.js`, `extension/lib/capture.js`, `extension/lib/engine.js`, `extension/tests/recipe.test.mjs`, `extension/tests/engine.test.mjs`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces (ES modules):
  - `recipe.js`: `RECIPE_VERSION = 1`; `class RecipeError extends Error` with `code` (`'invalid'` | `'unsupported'`); `validate(recipe) → { kind, hosts, timeoutMs }`.
  - `capture.js`: `redirectValue(url, prefix, param) → string`; `cookieHeader([{name, value}]) → string`; `jsonField(text, field) → string`; `originAllowed(origin, enabled[]) → bool`; `hostsGranted(hosts[], remembered[]) → bool`.
  - `engine.js`: `startRun(recipe, api) → { result: Promise<string>, cancel() }`; errors carry `code`: `'cancelled'` | `'timeout'` | `'failed'`. `api` = `{ openTab(url, private) → Promise<tabId>, watchTab(tabId, handler) → unsubscribe, tabURL(tabId) → Promise<string>, getCookie(url, name, tabId), getCookies(url, tabId) → Promise<[{name, value}]>, readStorage(tabId, key), fetchText(url), closeTab(tabId), setTimer(ms, fn) → cancel }`; `handler({ kind: 'loaded' | 'committed' | 'removed', url })`.

- [ ] **Step 1: Write the failing tests** — `extension/package.json`:

```json
{
  "private": true,
  "type": "module",
  "description": "Lets Node run the extension's ES modules in tests; not part of the packed extension."
}
```

`extension/tests/recipe.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { validate } from '../lib/recipe.js';
import { cookieHeader, hostsGranted, jsonField, originAllowed, redirectValue } from '../lib/capture.js';

const humble = () => ({
  version: 1,
  open: 'https://www.humblebundle.com/home/keys',
  when: { urlPrefix: 'https://www.humblebundle.com/home/keys' },
  capture: { cookie: { url: 'https://www.humblebundle.com', name: '_simpleauth_sess' } },
});

test('a sound recipe gives its capture kind, hosts and timeout', () => {
  const ea = { version: 1, open: 'https://www.ea.com/', hosts: ['accounts.ea.com'],
    when: { fetch: { url: 'https://accounts.ea.com/connect/auth?x=1', field: 'access_token' } },
    capture: { cookies: { url: 'https://accounts.ea.com/connect/auth?x=1' } }, timeoutSeconds: 120 };
  assert.deepEqual(validate(humble()), { kind: 'cookie', hosts: ['www.humblebundle.com'], timeoutMs: 300000 });
  assert.deepEqual(validate(ea), { kind: 'cookies', hosts: ['www.ea.com', 'accounts.ea.com'], timeoutMs: 120000 });
});

test('broken or hostile recipes are refused, newer ones as unsupported', () => {
  const cases = {
    invalid: [
      (r) => { r.open = 'http://www.humblebundle.com/'; },
      (r) => { r.capture = {}; },
      (r) => { r.capture.fetch = { url: 'https://www.humblebundle.com/x', field: 'a' }; },
      (r) => { r.capture.cookie.url = 'https://evil.example/'; },
      (r) => { r.when.urlPrefix = 'https://evil.example/'; },
      (r) => { r.hosts = ['https://x.com/']; },
      (r) => { r.timeoutSeconds = 601; },
      (r) => { r.capture.cookie.name = ''; },
    ],
    unsupported: [(r) => { r.version = 2; }],
  };
  for (const [code, changes] of Object.entries(cases)) {
    for (const change of changes) {
      const r = humble();
      change(r);
      assert.throws(() => validate(r), (e) => e.code === code, String(change));
    }
  }
  assert.throws(() => validate(null), (e) => e.code === 'invalid');
});

test('capture helpers', () => {
  assert.equal(redirectValue('https://embed.gog.com/on_login_success?origin=client&code=ab%2Fc', 'https://embed.gog.com/on_login_success', 'code'), 'ab/c');
  assert.equal(redirectValue('https://embed.gog.com/other?code=x', 'https://embed.gog.com/on_login_success', 'code'), '');
  assert.equal(cookieHeader([{ name: 'a', value: '1' }, { name: 'b', value: '2' }]), 'a=1; b=2');
  assert.equal(jsonField('{"authorizationCode":"abc","x":1}', 'authorizationCode'), 'abc');
  assert.equal(jsonField('{"authorizationCode":null}', 'authorizationCode'), '');
  assert.equal(jsonField('access_token=tok&expires=1', 'access_token'), 'tok');
  assert.equal(jsonField('<html>', 'npsso'), '');
  assert.equal(originAllowed('http://192.168.1.10:8080', ['http://192.168.1.10:8080']), true);
  assert.equal(originAllowed('http://192.168.1.10:8081', ['http://192.168.1.10:8080']), false);
  assert.equal(originAllowed('https://evil.example', []), false);
  assert.equal(hostsGranted(['a.com', 'b.com'], ['b.com', 'a.com']), true);
  assert.equal(hostsGranted(['a.com', 'c.com'], ['a.com']), false);
});
```

`extension/tests/engine.test.mjs`:

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { startRun } from '../lib/engine.js';

const tick = () => new Promise((r) => setTimeout(r, 0));

// fakeBrowser stands in for the extension APIs: tests change its cookies, storage and fetches,
// then fire the tab events a real browser would.
function fakeBrowser({ url = 'about:blank' } = {}) {
  const b = {
    cookies: {}, storage: {}, fetches: {}, url, opened: [], closed: [], handler: null, timeout: null,
    openTab: async (u, priv) => { b.opened.push([u, priv]); return 7; },
    watchTab: (id, h) => { b.handler = h; return () => { b.handler = null; }; },
    tabURL: async () => b.url,
    getCookie: async (u, name) => b.cookies[name] ?? '',
    getCookies: async () => Object.entries(b.cookies).map(([name, value]) => ({ name, value })),
    readStorage: async (id, key) => b.storage[key] ?? '',
    fetchText: async (u) => b.fetches[u] ?? '{}',
    closeTab: async (id) => { b.closed.push(id); },
    setTimer: (ms, fn) => { b.timeout = fn; return () => { b.timeout = null; }; },
    fire: async (kind, u) => { b.url = u ?? b.url; b.handler?.({ kind, url: u }); await tick(); await tick(); },
  };
  return b;
}

const humble = {
  version: 1, open: 'https://www.humblebundle.com/home/keys',
  when: { urlPrefix: 'https://www.humblebundle.com/home/keys' },
  capture: { cookie: { url: 'https://www.humblebundle.com', name: '_simpleauth_sess' } },
};

test('a cookie is captured only once the tab reaches the signed-in page, then the tab closes', async () => {
  const b = fakeBrowser();
  const run = startRun(humble, b);
  await tick();
  b.cookies._simpleauth_sess = 'anonymous';
  await b.fire('loaded', 'https://www.humblebundle.com/login?goto=/home/keys');
  assert.equal(b.closed.length, 0, 'not signed in yet');
  b.cookies._simpleauth_sess = 'signed-in';
  await b.fire('loaded', 'https://www.humblebundle.com/home/keys');
  assert.equal(await run.result, 'signed-in');
  assert.deepEqual(b.closed, [7]);
  assert.deepEqual(b.opened, [['https://www.humblebundle.com/home/keys', false]]);
});

test('a user already signed in is captured right after opening', async () => {
  const b = fakeBrowser({ url: 'https://www.humblebundle.com/home/keys' });
  b.cookies._simpleauth_sess = 'sess';
  assert.equal(await startRun(humble, b).result, 'sess');
});

test('a redirect code is taken when the store redirects', async () => {
  const b = fakeBrowser();
  const run = startRun({ version: 1, open: 'https://login.live.com/oauth20_authorize.srf?x=1',
    capture: { redirect: { prefix: 'https://login.live.com/oauth20_desktop.srf', param: 'code' } } }, b);
  await tick();
  await b.fire('committed', 'https://login.live.com/login.srf');
  await b.fire('committed', 'https://login.live.com/oauth20_desktop.srf?code=M.C1_abc&lc=1033');
  assert.equal(await run.result, 'M.C1_abc');
});

test('storage is read only on its page, and a value must contain the condition', async () => {
  const b = fakeBrowser();
  const run = startRun({ version: 1, open: 'https://www.fanatical.com/en/', when: { contains: '"authenticated":true' },
    capture: { storage: { origin: 'https://www.fanatical.com', key: 'bsauth' } } }, b);
  await tick();
  b.storage.bsauth = '{"authenticated":false}';
  await b.fire('loaded', 'https://www.fanatical.com/en/');
  assert.equal(b.closed.length, 0);
  b.storage.bsauth = '{"authenticated":true,"token":"t"}';
  await b.fire('loaded', 'https://www.fanatical.com/en/account');
  assert.equal(await run.result, '{"authenticated":true,"token":"t"}');

  const u = fakeBrowser();
  const ubi = startRun({ version: 1, open: 'https://connect.ubisoft.com/login?x=1', private: true,
    capture: { storage: { origin: 'https://connect.ubisoft.com', key: 'PRODrememberMe', path: '/ready' } } }, u);
  await tick();
  u.storage.PRODrememberMe = 'ticket';
  await u.fire('loaded', 'https://connect.ubisoft.com/login?x=1');
  assert.equal(u.closed.length, 0, 'not on /ready yet');
  await u.fire('loaded', 'https://connect.ubisoft.com/ready');
  assert.equal(await ubi.result, 'ticket');
  assert.deepEqual(u.opened[0], ['https://connect.ubisoft.com/login?x=1', true]);
});

test('a fetched field and a fetch condition wait until the user is signed in', async () => {
  const b = fakeBrowser();
  const api = 'https://www.epicgames.com/id/api/redirect?clientId=x&responseType=code';
  const run = startRun({ version: 1, open: 'https://www.epicgames.com/id/login?x=1',
    capture: { fetch: { url: api, field: 'authorizationCode' } } }, b);
  await tick();
  b.fetches[api] = '{"authorizationCode":null}';
  await b.fire('loaded', 'https://www.epicgames.com/id/login');
  assert.equal(b.closed.length, 0);
  b.fetches[api] = '{"authorizationCode":"0123abcd"}';
  await b.fire('loaded', 'https://www.epicgames.com/account/personal');
  assert.equal(await run.result, '0123abcd');
});

test('closing the tab cancels without closing it again; the timeout and cancel close it', async () => {
  const b = fakeBrowser();
  const run = startRun(humble, b);
  await tick();
  await b.fire('removed');
  await assert.rejects(run.result, (e) => e.code === 'cancelled');
  assert.deepEqual(b.closed, []);

  const t = fakeBrowser();
  const timed = startRun(humble, t);
  await tick();
  t.timeout();
  await assert.rejects(timed.result, (e) => e.code === 'timeout');
  assert.deepEqual(t.closed, [7]);

  const c = fakeBrowser();
  const cancelled = startRun(humble, c);
  await tick();
  cancelled.cancel();
  await assert.rejects(cancelled.result, (e) => e.code === 'cancelled');
  assert.deepEqual(c.closed, [7]);
});

test('an invalid recipe is refused before anything opens', () => {
  const b = fakeBrowser();
  assert.throws(() => startRun({ ...humble, open: 'http://x.com/' }, b), (e) => e.code === 'invalid');
  assert.deepEqual(b.opened, []);
});
```

In `Taskfile.yml`, the `test` command gains the extension's tests and a syntax check of its non-module scripts. Change `node --test tests/*.test.mjs` to:

```
node --test tests/*.test.mjs ../extension/tests/*.test.mjs && for f in ../extension/*.js; do node --check $f || exit 1; done
```

(`background.js` and `popup.js` are modules too: `node --check` accepts their `import` lines only with the `type: module` package file, which is why `extension/package.json` exists. `bridge.js` and `confirm.js` are plain scripts.)

- [ ] **Step 2: Run them to see them fail**

Run: `task test`
Expected: FAIL at the extension tests (modules not found). If the `for` loop runs before the files of Task 5 exist, it finds nothing and passes; that is fine.

- [ ] **Step 3: Implement** — `extension/lib/recipe.js`:

```js
// Recipe validation. The extension trusts nothing it receives: every recipe is checked here
// before anything opens, with the same rules as Game Vault's schema.SignInRecipe.Validate.

export const RECIPE_VERSION = 1;

const KINDS = ['cookie', 'cookies', 'storage', 'redirect', 'fetch'];
const REQUIRED = { cookie: ['url', 'name'], cookies: ['url'], storage: ['origin', 'key'], redirect: ['prefix', 'param'], fetch: ['url', 'field'] };
const ADDRESS = { cookie: 'url', cookies: 'url', storage: 'origin', redirect: 'prefix', fetch: 'url' };
const HOST = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/;

/** A recipe the extension refuses; code is 'invalid' or 'unsupported' (newer than this extension). */
export class RecipeError extends Error {
  constructor(message, code = 'invalid') {
    super(message);
    this.name = 'RecipeError';
    this.code = code;
  }
}

function httpsHost(address) {
  let u;
  try { u = new URL(address); } catch { throw new RecipeError(`not an address: ${address}`); }
  if (u.protocol !== 'https:') throw new RecipeError(`not an https address: ${address}`);
  return u.hostname.toLowerCase();
}

/** Checks a recipe and returns what running it needs: its capture kind, its hosts, its timeout. */
export function validate(recipe) {
  if (!recipe || typeof recipe !== 'object') throw new RecipeError('no recipe');
  if (!Number.isInteger(recipe.version) || recipe.version < 1) throw new RecipeError('no recipe version');
  if (recipe.version > RECIPE_VERSION) throw new RecipeError('this recipe needs a newer extension', 'unsupported');
  const seconds = recipe.timeoutSeconds ?? 0;
  if (!Number.isInteger(seconds) || seconds < 0 || seconds > 600) throw new RecipeError('timeout out of range');

  const hosts = [httpsHost(recipe.open)];
  for (const h of recipe.hosts ?? []) {
    if (typeof h !== 'string' || !HOST.test(h)) throw new RecipeError(`not a host name: ${h}`);
    if (!hosts.includes(h)) hosts.push(h);
  }

  const kinds = KINDS.filter((k) => recipe.capture?.[k]);
  if (kinds.length !== 1) throw new RecipeError('a recipe needs exactly one capture');
  const kind = kinds[0];
  const c = recipe.capture[kind];
  for (const f of REQUIRED[kind]) if (typeof c[f] !== 'string' || !c[f]) throw new RecipeError(`the ${kind} capture needs ${f}`);

  const addresses = [c[ADDRESS[kind]]];
  if (recipe.when?.urlPrefix) addresses.push(recipe.when.urlPrefix);
  if (recipe.when?.fetch) {
    if (!recipe.when.fetch.field) throw new RecipeError('the condition needs a field');
    addresses.push(recipe.when.fetch.url);
  }
  for (const a of addresses) {
    const h = httpsHost(a);
    if (!hosts.includes(h)) throw new RecipeError(`the recipe reads ${h}, which it neither opens nor lists`);
  }

  return { kind, hosts, timeoutMs: (seconds || 300) * 1000 };
}
```

`extension/lib/capture.js`:

```js
// Small pure helpers the engine and the background use.

/** The value of param in url when url starts with prefix; '' otherwise. */
export function redirectValue(url, prefix, param) {
  if (typeof url !== 'string' || !url.startsWith(prefix)) return '';
  try { return new URL(url).searchParams.get(param) ?? ''; } catch { return ''; }
}

/** Cookies as a Cookie request header. */
export function cookieHeader(cookies) {
  return cookies.map((c) => `${c.name}=${c.value}`).join('; ');
}

/** A string field of a JSON answer, or of a form-encoded one ("a=1&b=2"); '' when missing or null. */
export function jsonField(text, field) {
  try {
    const v = JSON.parse(text)?.[field];
    return typeof v === 'string' ? v : '';
  } catch {
    for (const part of String(text).split(/[&\s]/)) {
      const [k, ...rest] = part.split('=');
      if (k === field && rest.length) return decodeURIComponent(rest.join('='));
    }
    return '';
  }
}

/** Whether a page's origin is one of the Game Vault addresses the user enabled (exact match, port included). */
export function originAllowed(origin, enabled) {
  return Array.isArray(enabled) && enabled.includes(origin);
}

/** Whether every host was confirmed before. */
export function hostsGranted(hosts, remembered) {
  return hosts.every((h) => remembered.includes(h));
}
```

`extension/lib/engine.js`:

```js
// The recipe engine: opens the store's sign-in, waits until the user is signed in, captures the
// value and closes the tab. The browser is behind `api` (see background.js), so Node tests drive
// it with a fake one.

import { validate } from './recipe.js';
import { cookieHeader, jsonField, redirectValue } from './capture.js';

function failure(code, message) {
  const e = new Error(message ?? code);
  e.code = code;
  return e;
}

/** Starts a recipe; result resolves with the captured value or rejects with a coded error. */
export function startRun(recipe, api) {
  const { kind, timeoutMs } = validate(recipe); // throws before anything opens
  const capture = recipe.capture[kind];
  let tabId;
  let done = false;
  let unsubscribe = () => {};
  let stopTimer = () => {};
  let resolve;
  let reject;
  const result = new Promise((res, rej) => { resolve = res; reject = rej; });

  const finish = (error, value, tabGone = false) => {
    if (done) return;
    done = true;
    unsubscribe();
    stopTimer();
    if (tabId !== undefined && !tabGone) api.closeTab(tabId);
    if (error) reject(error);
    else resolve(value);
  };

  const ready = async (url) => {
    const w = recipe.when;
    if (!w) return true;
    if (w.urlPrefix && !(url ?? '').startsWith(w.urlPrefix)) return false;
    if (w.fetch && !jsonField(await api.fetchText(w.fetch.url), w.fetch.field)) return false;
    return true;
  };

  const attempt = async (url) => {
    if (done || kind === 'redirect') return;
    try {
      if (!(await ready(url))) return;
      let value = '';
      if (kind === 'cookie') value = await api.getCookie(capture.url, capture.name, tabId);
      if (kind === 'cookies') value = cookieHeader(await api.getCookies(capture.url, tabId));
      if (kind === 'storage' && (url ?? '').startsWith(capture.origin + (capture.path ?? ''))) value = await api.readStorage(tabId, capture.key);
      if (kind === 'fetch') value = jsonField(await api.fetchText(capture.url), capture.field);
      if (value && (!recipe.when?.contains || value.includes(recipe.when.contains))) finish(null, value);
    } catch {
      // Not readable yet (a page still loading, a fetch refused): the next page load tries again.
    }
  };

  (async () => {
    try {
      tabId = await api.openTab(recipe.open, Boolean(recipe.private));
      unsubscribe = api.watchTab(tabId, (event) => {
        if (event.kind === 'removed') finish(failure('cancelled', 'the sign-in tab was closed'), undefined, true);
        else if (event.kind === 'committed' && kind === 'redirect') {
          const value = redirectValue(event.url, capture.prefix, capture.param);
          if (value) finish(null, value);
        } else if (event.kind === 'loaded') attempt(event.url);
      });
      stopTimer = api.setTimer(timeoutMs, () => finish(failure('timeout', 'the sign-in took too long')));
      await attempt(await api.tabURL(tabId)); // a user already signed in is captured at once
    } catch (e) {
      finish(failure('failed', e?.message));
    }
  })();

  return { result, cancel: () => finish(failure('cancelled', 'cancelled')) };
}
```

- [ ] **Step 4: Run the tests**

Run: `task test`
Expected: PASS (Go, TS, web and extension Node tests, translations).

- [ ] **Step 5: Commit**

```bash
git add extension Taskfile.yml
git commit -m "Extension: recipe validation and the capture engine, tested in Node with a fake browser"
```

---

### Task 5: The extension shell

**Files:**
- Create: `extension/manifest.json`, `extension/background.js`, `extension/bridge.js`, `extension/lib/bridges.js`, `extension/popup.html`, `extension/popup.js`, `extension/confirm.html`, `extension/confirm.js`, `extension/ui.css`, `extension/README.md`, `extension/PRIVACY.md`
- Modify: `Taskfile.yml` (`extension:pack`), `.gitignore` (`dist/` if not ignored)

**Interfaces:**
- Consumes: `startRun`, `validate`, `RECIPE_VERSION`, `originAllowed`, `hostsGranted` (Task 4).
- Produces: the page protocol. Page → bridge: `window.postMessage({ type: 'gamevault-connector', dir: 'request', op, id, source?, field?, recipe? }, origin)`. Bridge → page: `{ type: 'gamevault-connector', dir: 'response', id, op: 'hello' | 'result' | 'error' | 'cancelled', version?, recipeVersion?, value?, code?, message? }`. Codes: `denied`, `busy`, `invalid`, `unsupported`, `cancelled`, `timeout`, `failed`.

- [ ] **Step 1: `manifest.json`**

```json
{
  "manifest_version": 3,
  "name": "Game Vault Connector",
  "version": "1.0.0",
  "description": "Connects your stores to your own Game Vault in one click: you sign in on each store's page and the extension hands the session to your Game Vault tab.",
  "permissions": ["cookies", "webNavigation", "scripting", "tabs", "storage"],
  "optional_host_permissions": ["https://*/*", "http://*/*"],
  "background": { "service_worker": "background.js", "scripts": ["background.js"], "type": "module" },
  "action": { "default_popup": "popup.html", "default_title": "Game Vault Connector" },
  "browser_specific_settings": { "gecko": { "id": "connector@gamevault", "strict_min_version": "128.0" } }
}
```

- [ ] **Step 2: `bridge.js`** (a plain content script; it may be injected twice, so it guards itself):

```js
// Content script on the Game Vault addresses the user enabled: relays the page's requests to the
// extension over a port (a port keeps the service worker alive through a long sign-in) and the
// answers back to the page. Only messages from this very page and origin are relayed.
(() => {
  if (window.__gamevaultConnectorBridge) return;
  window.__gamevaultConnectorBridge = true;
  const TYPE = 'gamevault-connector';
  const reply = (id, body) => window.postMessage({ ...body, type: TYPE, dir: 'response', id }, location.origin);

  window.addEventListener('message', (event) => {
    if (event.source !== window || event.origin !== location.origin) return;
    const d = event.data;
    if (!d || d.type !== TYPE || d.dir !== 'request' || typeof d.id !== 'string') return;
    const port = chrome.runtime.connect({ name: 'gamevault' });
    let answered = false;
    port.onMessage.addListener((msg) => {
      answered = true;
      reply(d.id, msg);
      port.disconnect();
    });
    port.onDisconnect.addListener(() => {
      if (!answered) reply(d.id, { op: 'error', code: 'failed', message: 'the extension stopped; try again' });
    });
    port.postMessage({ op: d.op, id: d.id, cancelId: d.cancelId, source: d.source, field: d.field, recipe: d.recipe });
  });
})();
```

- [ ] **Step 3: `background.js`**:

```js
// Game Vault Connector: runs sign-in recipes for the Game Vault addresses the user enabled. It
// knows no store: recipes come from Game Vault as data and are validated before anything opens.
// Captured values go only to the tab that asked, in the reply; nothing is stored or logged.

import { startRun } from './lib/engine.js';
import { RECIPE_VERSION, validate } from './lib/recipe.js';
import { hostsGranted, originAllowed } from './lib/capture.js';
import { registerBridge } from './lib/bridges.js';

const running = new Map(); // request id → { cancel, tab }
const pending = new Map(); // confirmation id → { resolve, windowId }

const coded = (code, message) => Object.assign(new Error(message ?? code), { code });

async function enabledOrigins() {
  return (await chrome.storage.local.get('origins')).origins ?? [];
}

async function cookieStore(tabId) {
  const stores = await chrome.cookies.getAllCookieStores();
  return stores.find((s) => s.tabIds.includes(tabId))?.id;
}

const browserApi = {
  async openTab(url, privateWindow) {
    if (privateWindow && await chrome.extension.isAllowedIncognitoAccess()) {
      const w = await chrome.windows.create({ url, incognito: true });
      return w.tabs[0].id;
    }
    return (await chrome.tabs.create({ url })).id;
  },
  watchTab(tabId, handler) {
    const committed = (d) => d.tabId === tabId && d.frameId === 0 && handler({ kind: 'committed', url: d.url });
    const completed = (d) => d.tabId === tabId && d.frameId === 0 && handler({ kind: 'loaded', url: d.url });
    const removed = (id) => id === tabId && handler({ kind: 'removed' });
    chrome.webNavigation.onCommitted.addListener(committed);
    chrome.webNavigation.onCompleted.addListener(completed);
    chrome.tabs.onRemoved.addListener(removed);
    return () => {
      chrome.webNavigation.onCommitted.removeListener(committed);
      chrome.webNavigation.onCompleted.removeListener(completed);
      chrome.tabs.onRemoved.removeListener(removed);
    };
  },
  async tabURL(tabId) { return (await chrome.tabs.get(tabId)).url ?? ''; },
  async getCookie(url, name, tabId) {
    const storeId = await cookieStore(tabId);
    return (await chrome.cookies.get({ url, name, ...(storeId ? { storeId } : {}) }))?.value ?? '';
  },
  async getCookies(url, tabId) {
    const storeId = await cookieStore(tabId);
    return chrome.cookies.getAll({ url, ...(storeId ? { storeId } : {}) });
  },
  async readStorage(tabId, key) {
    const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: (k) => localStorage.getItem(k) ?? '', args: [key] });
    return result ?? '';
  },
  async fetchText(url) { return (await fetch(url, { credentials: 'include' })).text(); },
  async closeTab(tabId) { try { await chrome.tabs.remove(tabId); } catch { /* already closed */ } },
  setTimer(ms, fn) { const t = setTimeout(fn, ms); return () => clearTimeout(t); },
};

// Asks the user, in the extension's own window, to let this Game Vault address read sessions on
// these hosts; the browser's host permission is requested from that window (a user gesture).
async function ensureGranted(origin, hosts) {
  const grants = (await chrome.storage.local.get('grants')).grants ?? {};
  const known = grants[origin] ?? [];
  const origins = hosts.map((h) => `https://${h}/*`);
  if (hostsGranted(hosts, known) && await chrome.permissions.contains({ origins })) return;
  const id = crypto.randomUUID();
  const answer = await new Promise(async (resolve) => {
    const query = new URLSearchParams({ id, origin, hosts: hosts.join(',') });
    const w = await chrome.windows.create({ url: chrome.runtime.getURL(`confirm.html?${query}`), type: 'popup', width: 460, height: 300 });
    pending.set(id, { resolve, windowId: w.id });
  });
  if (!answer.allow) throw coded('denied', 'access was not allowed');
  if (answer.remember) {
    grants[origin] = [...new Set([...known, ...hosts])];
    await chrome.storage.local.set({ grants });
  }
}

chrome.windows.onRemoved.addListener((windowId) => {
  for (const [id, p] of pending) if (p.windowId === windowId) { pending.delete(id); p.resolve({ allow: false }); }
});

async function handle(msg, sender) {
  const origin = sender.origin ?? new URL(sender.url).origin;
  if (!sender.tab || !originAllowed(origin, await enabledOrigins())) throw coded('denied', 'this address is not enabled in the extension');
  switch (msg.op) {
    case 'hello':
      return { op: 'hello', version: chrome.runtime.getManifest().version, recipeVersion: RECIPE_VERSION };
    case 'cancel':
      running.get(msg.cancelId ?? msg.id)?.cancel();
      return { op: 'cancelled' };
    case 'connect': {
      if ([...running.values()].some((r) => r.tab === sender.tab.id)) throw coded('busy', 'a sign-in is already running in this tab');
      const { hosts } = validate(msg.recipe);
      await ensureGranted(origin, hosts);
      const run = startRun(msg.recipe, browserApi);
      running.set(msg.id, { cancel: run.cancel, tab: sender.tab.id });
      try {
        return { op: 'result', value: await run.result };
      } finally {
        running.delete(msg.id);
      }
    }
    default:
      throw coded('invalid', `unknown request ${msg.op}`);
  }
}

chrome.runtime.onConnect.addListener((port) => {
  if (port.name !== 'gamevault') return;
  port.onMessage.addListener((msg) => {
    handle(msg, port.sender).then(
      (reply) => port.postMessage(reply),
      (e) => port.postMessage({ op: 'error', code: e.code ?? 'failed', message: e.message }),
    );
  });
});

// Answers of the confirmation window (an extension page).
chrome.runtime.onMessage.addListener((msg, sender) => {
  if (msg?.op !== 'confirm' || !sender.url?.startsWith(chrome.runtime.getURL('confirm.html'))) return;
  const p = pending.get(msg.id);
  if (!p) return;
  pending.delete(msg.id);
  p.resolve({ allow: Boolean(msg.allow), remember: Boolean(msg.remember) });
});

// Registered bridges survive restarts; re-register them in case the browser dropped them.
chrome.runtime.onStartup?.addListener(async () => {
  for (const origin of await enabledOrigins()) await registerBridge(origin).catch(() => {});
});
```

`extension/lib/bridges.js` (shared by the background and the popup; importing `background.js` from the popup would register its listeners twice):

```js
// The bridge content script registered per enabled Game Vault address.

/** The registration id of an address's bridge. */
export function bridgeId(origin) { return `bridge-${origin.replace(/[^a-z0-9]/gi, '_')}`; }

/** Registers the bridge on an address (a match pattern has no port: the background checks it). */
export async function registerBridge(origin) {
  const { protocol, hostname } = new URL(origin);
  const id = bridgeId(origin);
  const existing = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
  if (existing.length) return;
  await chrome.scripting.registerContentScripts([{ id, matches: [`${protocol}//${hostname}/*`], js: ['bridge.js'], runAt: 'document_start', persistAcrossSessions: true }]);
}
```

(The page's `cancel` request carries the id of the run to cancel as `cancelId`; see Task 6.) Note: a match pattern has no port, so the bridge runs on every port of that host; the background still checks the exact origin, port included, against the enabled list.

- [ ] **Step 4: `popup.html` / `popup.js`** — the popup enables the current tab's address, lists enabled addresses and confirmed hosts, and revokes them:

```html
<!doctype html>
<meta charset="utf-8">
<link rel="stylesheet" href="ui.css">
<main>
  <h1>Game Vault Connector</h1>
  <section id="current"></section>
  <h2>Enabled Game Vault addresses</h2>
  <ul id="origins"></ul>
  <p class="muted" id="version"></p>
</main>
<script type="module" src="popup.js"></script>
```

```js
import { registerBridge, bridgeId } from './lib/bridges.js';

const $ = (id) => document.getElementById(id);
const load = async (k, d) => (await chrome.storage.local.get(k))[k] ?? d;

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
    b.textContent = `Enable on ${here}`;
    b.onclick = async () => {
      const { protocol, hostname } = new URL(here);
      if (!await chrome.permissions.request({ origins: [`${protocol}//${hostname}/*`] })) return;
      await chrome.storage.local.set({ origins: [...origins, here] });
      await registerBridge(here);
      await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: ['bridge.js'] });
      render();
    };
    const p = document.createElement('p');
    p.className = 'muted';
    p.textContent = 'Only on your own Game Vault: this lets that address ask the extension to connect stores.';
    current.append(b, p);
  } else if (origins.includes(here)) {
    current.textContent = `Enabled on this address. Use Connect in a source's settings.`;
  }

  const list = $('origins');
  list.textContent = '';
  for (const origin of origins) {
    const li = document.createElement('li');
    const remove = document.createElement('button');
    remove.textContent = 'Remove';
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
        x.title = `Forget ${h}`;
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
  if (!origins.length) list.innerHTML = '<li class="muted">None yet: open your Game Vault and press Enable.</li>';
  $('version').textContent = `Version ${chrome.runtime.getManifest().version}`;
}

render();
```


- [ ] **Step 5: `confirm.html` / `confirm.js`**:

```html
<!doctype html>
<meta charset="utf-8">
<link rel="stylesheet" href="ui.css">
<main>
  <h1>Allow access?</h1>
  <p id="what"></p>
  <ul id="hosts"></ul>
  <label><input type="checkbox" id="remember" checked> Remember for this Game Vault</label>
  <p class="actions"><button id="deny">Deny</button> <button id="allow" class="primary">Allow</button></p>
</main>
<script src="confirm.js"></script>
```

```js
// Confirmation window: the user decides whether a Game Vault address may read their session on
// some store hosts; the browser's own permission prompt follows Allow (it needs this click).
const q = new URLSearchParams(location.search);
const id = q.get('id');
const origin = q.get('origin');
const hosts = (q.get('hosts') ?? '').split(',').filter(Boolean);
document.getElementById('what').textContent = `Game Vault at ${origin} wants your session on:`;
for (const h of hosts) {
  const li = document.createElement('li');
  li.textContent = h;
  document.getElementById('hosts').append(li);
}
const answer = async (allow) => {
  let granted = false;
  if (allow) granted = await chrome.permissions.request({ origins: hosts.map((h) => `https://${h}/*`) });
  await chrome.runtime.sendMessage({ op: 'confirm', id, allow: allow && granted, remember: document.getElementById('remember').checked });
  window.close();
};
document.getElementById('allow').onclick = () => answer(true);
document.getElementById('deny').onclick = () => answer(false);
```

Add `extension/ui.css` with minimal styles (system font, 13 px, padding 12 px, primary button amber `#FFB020` text `#181C2C`, muted grey), shared by both pages.

- [ ] **Step 6: Docs in the extension** — `extension/README.md`: what it does (one paragraph), install unpacked (Chrome: `chrome://extensions` → Developer mode → Load unpacked → this folder; Firefox: `about:debugging` → This Firefox → Load Temporary Add-on → `manifest.json`), first use (open Game Vault → extension icon → Enable on this address → in a source's settings, Connect → Allow), Ubisoft and incognito (allow the extension in incognito for a private window), revoking (popup), packing (`task extension:pack`). `extension/PRIVACY.md`: what it reads (only the cookies, storage values, redirect parameters or fetched fields a recipe names, on hosts the user confirmed), where it sends them (only to the Game Vault tab that asked, in the same browser), what it stores (enabled addresses and confirmed hosts; never credentials), no analytics, no remote code.

- [ ] **Step 7: Packing** — `Taskfile.yml`:

```yaml
  extension:pack:
    desc: Zip the browser extension for the Chrome Web Store / Firefox Add-ons into dist/
    deps: [toolchain]
    cmds:
      - '{{.IN_TOOLCHAIN}} sh -c "mkdir -p dist && cd extension && v=$(node -p \"require(\x27./manifest.json\x27).version\") && rm -f ../dist/game-vault-connector-$v.zip && zip -qr ../dist/game-vault-connector-$v.zip . -x tests/\* package.json && echo dist/game-vault-connector-$v.zip"'
```

Check `zip` exists in the toolchain (`task shell`, `which zip`). If not, add `zip` to the package install line of `build/toolchain.Dockerfile` (this rebuilds the image). Make sure `dist/` is git-ignored.

- [ ] **Step 8: Check** — `task test` (syntax of every `extension/*.js`, logic tests), `task extension:pack`; load `extension/` unpacked in Chrome with the test server running on a copy (Task 6 makes the page side). Here, check that the popup opens, that "Enable on http://127.0.0.1:8093" adds the address and that a page on another origin posting `{type:'gamevault-connector', dir:'request', op:'hello', id:'x'}` gets no answer (the bridge is not there).

- [ ] **Step 9: Commit**

```bash
git add extension Taskfile.yml .gitignore build/toolchain.Dockerfile
git commit -m "Extension: background engine, page bridge, confirmation and popup; packing task"
```

---

### Task 6: The source dialog connects with the extension

**Files:**
- Create: `web/src/lib/connector.ts`
- Modify: `web/src/features/sources/SourceDialog.tsx`, `web/src/styles.css`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/es.json`

**Interfaces:**
- Consumes: `SettingField.signIn` (Task 2), the page protocol (Task 5).
- Produces: `connector.ts`: `interface Recipe { version: number; open: string; private?: boolean; … }`; `detect(): Promise<{ version: string; recipeVersion: number } | null>`; `connect(source: string, field: string, recipe: Recipe): { result: Promise<string>; cancel(): void }`; `class ConnectorError extends Error { code: string }`.

- [ ] **Step 1: `connector.ts`**:

```ts
// Talks to the Game Vault Connector browser extension through its bridge on this page
// (window.postMessage): detect it, and run a source's sign-in recipe to get a credential.

const TYPE = 'gamevault-connector';
const CONNECT_TIMEOUT_MS = 11 * 60 * 1000; // a little over the extension's own maximum (10 min)

/** A sign-in recipe as Game Vault sends it (schema.SignInRecipe); the extension validates it. */
export interface Recipe {
  version: number;
  open: string;
  private?: boolean;
  [key: string]: unknown;
}

/** An error the extension or the page reported; code is denied, busy, invalid, unsupported, cancelled, timeout or failed. */
export class ConnectorError extends Error {
  code: string;

  constructor(code: string, message?: string) {
    super(message || code);
    this.code = code;
  }
}

interface Reply {
  op: string;
  id: string;
  version?: string;
  recipeVersion?: number;
  value?: string;
  code?: string;
  message?: string;
}

function request(body: Record<string, unknown>, timeoutMs: number): { reply: Promise<Reply>; id: string } {
  const id = crypto.randomUUID();
  const reply = new Promise<Reply>((resolve, reject) => {
    const onMessage = (e: MessageEvent) => {
      const d = e.data as Reply & { type?: string; dir?: string };
      if (e.source !== window || e.origin !== location.origin || d?.type !== TYPE || d.dir !== 'response' || d.id !== id) return;
      cleanup();
      resolve(d);
    };
    const timer = window.setTimeout(() => { cleanup(); reject(new ConnectorError('timeout')); }, timeoutMs);
    const cleanup = () => { window.removeEventListener('message', onMessage); window.clearTimeout(timer); };
    window.addEventListener('message', onMessage);
    window.postMessage({ ...body, type: TYPE, dir: 'request', id }, location.origin);
  });
  return { reply, id };
}

/** The extension's version when it is installed and enabled on this address; null otherwise. */
export async function detect(): Promise<{ version: string; recipeVersion: number } | null> {
  try {
    const r = await request({ op: 'hello' }, 600).reply;
    return r.op === 'hello' ? { version: r.version ?? '', recipeVersion: r.recipeVersion ?? 0 } : null;
  } catch {
    return null;
  }
}

/** Runs a recipe: the extension opens the store's sign-in and resolves with the captured value. */
export function connect(source: string, field: string, recipe: Recipe): { result: Promise<string>; cancel(): void } {
  const { reply, id } = request({ op: 'connect', source, field, recipe }, CONNECT_TIMEOUT_MS);
  const result = reply.then((r) => {
    if (r.op === 'result' && r.value) return r.value;
    throw new ConnectorError(r.code ?? 'failed', r.message);
  });
  return { result, cancel: () => { request({ op: 'cancel', cancelId: id }, 2000).reply.catch(() => {}); } };
}
```

- [ ] **Step 2: The dialog** — in `SourceDialog.tsx`:
- state: `const [connector, setConnector] = useState<{ version: string; recipeVersion: number } | null>(null);` with `useEffect(() => { detect().then(setConnector); }, []);`, and `const [connecting, setConnecting] = useState<{ key: string; cancel: () => void } | null>(null);`
- `const recipeOf = (f: SettingField): Recipe | null => { try { return f.signIn ? JSON.parse(f.signIn) as Recipe : null; } catch { return null; } };`
- refactor `test` to take the settings to test (`test(with = settings)`) and return whether it passed;
- `connectField(f)`:

```tsx
  const connectField = async (f: SettingField, recipe: Recipe) => {
    const run = connect(type.id, f.key, recipe);
    setConnecting({ key: f.key, cancel: run.cancel });
    setResult(null);
    try {
      const value = await run.result;
      const next = { ...settings, [f.key]: value };
      setSettings(next);
      if (await test(next)) await saveWith(next);
    } catch (e) {
      const code = e instanceof ConnectorError ? e.code : 'failed';
      setResult({ tone: 'error', text: t(`connector.error.${code}`, { defaultValue: errorMessage(e) }) });
    } finally {
      setConnecting(null);
    }
  };
```

(`saveWith(next)` is `save` taking the settings; keep `save(e)` for the form.)
- under each non-consent field with a recipe, before `FieldHelp`:

```tsx
              {(f.helpUrl || recipe) && (
                <div className="connector-row">
                  <a className="button small-button" href={recipe?.open || f.helpUrl} target="_blank" rel="noreferrer">
                    {t('connector.openSignIn')}<Icon name="external" size={14} />
                  </a>
                  {recipe && connector && recipe.version <= connector.recipeVersion && (
                    connecting?.key === f.key
                      ? <><span className="muted small">{t('connector.waiting')}</span><button type="button" className="small-button" onClick={connecting.cancel}>{t('common.cancel')}</button></>
                      : <button type="button" className="small-button primary" disabled={busy || !!connecting || consentMissing} onClick={() => connectField(f, recipe)}>{t('connector.connect')}</button>
                  )}
                  {recipe && connector && recipe.version > connector.recipeVersion && <span className="muted small">{t('connector.update')}</span>}
                  {recipe && !connector && <span className="muted small">{t('connector.install')} <a href="https://github.com/botchris/game-vault/tree/main/extension" target="_blank" rel="noreferrer">{t('connector.howTo')}</a></span>}
                </div>
              )}
              {recipe?.private && connector && <p className="muted small">{t('connector.private')}</p>}
```

(`recipe` is `recipeOf(f)` computed in the map callback; import `Icon`, `connect`, `detect`, `ConnectorError`, `type Recipe`, `type SettingField`.)

- [ ] **Step 3: Styles** — `.connector-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 6px 0; }`.

- [ ] **Step 4: Translations** — `en.json`, top-level `connector`: `openSignIn` "Open the sign-in page", `connect` "Connect", `waiting` "Sign in in the tab that opened…", `install` "One click with the Game Vault Connector browser extension.", `howTo` "How to install it", `update` "Update the Game Vault Connector extension to connect this store.", `private` "Ubisoft opens in a private window if you allowed the extension in incognito; otherwise Game Vault's use of the session will sign that browser out of Ubisoft.", `error.denied` "The extension was not allowed to read that store, or this address is not enabled: open the extension's icon and press Enable on this address.", `error.busy` "A sign-in is already running.", `error.invalid` "This store's sign-in recipe is not valid; update Game Vault.", `error.unsupported` "Update the Game Vault Connector extension.", `error.cancelled` "Sign-in cancelled.", `error.timeout` "The sign-in took too long; try again.", `error.failed` "The extension could not finish the sign-in; try again or paste the value by hand." `es.json`: `openSignIn` "Abrir la página de inicio de sesión", `connect` "Conectar", `waiting` "Inicia sesión en la pestaña que se ha abierto…", `install` "En un clic con la extensión Game Vault Connector.", `howTo` "Cómo instalarla", `update` "Actualiza la extensión Game Vault Connector para conectar esta tienda.", `private` "Ubisoft se abre en una ventana privada si permitiste la extensión en incógnito; si no, el uso que Game Vault hace de la sesión cerrará la sesión de Ubisoft en ese navegador.", `error.denied` "No se permitió a la extensión leer esa tienda, o esta dirección no está activada: abre el icono de la extensión y pulsa Activar en esta dirección.", `error.busy` "Ya hay un inicio de sesión en marcha.", `error.invalid` "La receta de inicio de sesión de esta tienda no es válida; actualiza Game Vault.", `error.unsupported` "Actualiza la extensión Game Vault Connector.", `error.cancelled` "Inicio de sesión cancelado.", `error.timeout` "El inicio de sesión ha tardado demasiado; inténtalo de nuevo.", `error.failed` "La extensión no pudo terminar el inicio de sesión; inténtalo de nuevo o pega el valor a mano." (The extension's own pages stay in English for now.)

- [ ] **Step 5: Check with a simulated extension** — `task test`, then `task test-server`. In the browser pane, before the extension exists, inject a stand-in that answers the protocol:

```js
window.addEventListener('message', (e) => {
  const d = e.data;
  if (d?.type !== 'gamevault-connector' || d.dir !== 'request') return;
  const reply = (b) => window.postMessage({ ...b, type: 'gamevault-connector', dir: 'response', id: d.id }, location.origin);
  if (d.op === 'hello') reply({ op: 'hello', version: '1.0.0', recipeVersion: 1 });
  if (d.op === 'connect') setTimeout(() => reply(window.__fakeAnswer ?? { op: 'result', value: 'fake-session' }), 800);
});
```

Then reopen a source dialog (detection runs on open) and check:
- the Connect button appears and "Open the sign-in page" points at the recipe's address;
- Connect shows "waiting" and Cancel;
- the value fills the field, Test connection runs (with `fake-session` it fails with the source's own error, and the error is shown and the value kept);
- `__fakeAnswer = {op:'error', code:'denied'}` shows the denied text;
- `{op:'hello', recipeVersion: 0}` shows "update";
- without the stand-in, the install hint shows;
- Fanatical's Connect stays disabled until the consent is ticked.

Desktop and mobile. Do not connect real rotating sources on the copy. `task test-server:stop`.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "Source dialog: connect with the browser extension, open the sign-in page, clear errors"
```

---

### Task 7: Docs, real check, review and PR

**Files:**
- Modify: `docs/technical.md`, `docs/plugins.md`, `README.md`, `docs/superpowers/specs/2026-10-10-connector-extension-design.md`, `.claude/MEMORY.md`
- Create: `.claude/memory/connector.md`

- [ ] **Step 1: Spec** — record the planning decisions (top of this plan): `when`, enabling from the popup, `timeoutSeconds`, ports.
- [ ] **Step 2: `docs/technical.md`** — a "Connecting sources with the browser extension" section: what the extension does and does not do, recipes (shape, primitives, `when`, host rule, version), the page protocol, permissions (enable from the popup, per-host confirmation, revoking), storage (no credentials), Ubisoft and incognito, the fallback (paste anything, Open the sign-in page).
- [ ] **Step 3: `docs/plugins.md`** — "Letting the extension fill a credential": add `SignIn` to the field, choose the capture, add a `when` for cookies and storage, list other hosts in `Hosts`, `plugintest` checks it, and new primitives need a new recipe version and an extension update.
- [ ] **Step 4: README** — one line: connect most stores in one click with the Game Vault Connector browser extension (`extension/`).
- [ ] **Step 5: Memory** — `.claude/memory/connector.md` (`type: project`): the spike results per store (spec table, dated 2026-10-10), Ubisoft's `/ready` page and why its home page fails, that cookie and storage captures need a readiness condition, the protocol and permission model, nothing published to stores yet (user's decision C). Add its line to `.claude/MEMORY.md`.
- [ ] **Step 6: Full verification** — `task lint`, `task test`, the Task 6 checks on the test server, `task extension:pack`.
- [ ] **Step 7: Real check (the user)** — with the extension loaded unpacked and the test server on a copy, connect **Humble only** (it does not rotate) end to end. The other stores are connected on the user's real server after merging.
- [ ] **Step 8: Commit the docs**

```bash
git add docs README.md .claude
git commit -m "Document the Game Vault Connector extension and sign-in recipes"
```

- [ ] **Step 9: Final review** — a fresh reviewer on the most capable model, against the spec, the Review Focus list and the security constraints. Fix what it finds in separate commits.
- [ ] **Step 10: PR** — push `feature/connector-extension`, open a PR to `main` titled "Game Vault Connector: connect sources in one click with a browser extension". Say what was verified: Go and Node tests, the dialog with a simulated extension, Humble end to end. Say what remains for the user on their real server: the nine other stores. Then `mcp__ccd_pr__get_status`. No tags, nothing published to extension stores.
