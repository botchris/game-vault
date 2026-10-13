package rpc

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/catalog"
	"gamevault/internal/application/fields"
	"gamevault/internal/application/logs"
	"gamevault/internal/application/media"
	"gamevault/internal/application/ownership"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/source"
)

// sentinels lists every exported sentinel error of the domain and application packages, by
// "<package path under internal/>.<name>", with the code the API answers it with.
// TestSentinels_haveACode checks that the list is complete, so a new sentinel cannot reach the
// API as an internal error without someone deciding it should.
var sentinels = map[string]struct {
	err  error
	code connect.Code
}{
	"domain/auth.ErrUnauthenticated": {auth.ErrUnauthenticated, connect.CodeUnauthenticated},
	"domain/auth.ErrBadCredentials":  {auth.ErrBadCredentials, connect.CodeUnauthenticated},
	"domain/auth.ErrNotFound":        {auth.ErrNotFound, connect.CodeNotFound},
	"domain/auth.ErrSetupDone":       {auth.ErrSetupDone, connect.CodeAlreadyExists},
	"domain/auth.ErrSetupNotTrusted": {auth.ErrSetupNotTrusted, connect.CodePermissionDenied},
	"domain/auth.ErrTooManyAttempts": {auth.ErrTooManyAttempts, connect.CodeResourceExhausted},
	"domain/auth.ErrLockout":         {auth.ErrLockout, connect.CodeFailedPrecondition},
	"domain/field.ErrNotFound":       {field.ErrNotFound, connect.CodeNotFound},
	"domain/game.ErrGameNotFound":    {game.ErrGameNotFound, connect.CodeNotFound},
	"domain/game.ErrCopyNotFound":    {game.ErrCopyNotFound, connect.CodeNotFound},
	"domain/game.ErrPhotoNotFound":   {game.ErrPhotoNotFound, connect.CodeNotFound},
	"domain/game.ErrEditionNotFound": {game.ErrEditionNotFound, connect.CodeNotFound},
	"domain/game.ErrInvalidBarcode":  {game.ErrInvalidBarcode, connect.CodeInvalidArgument},
	"domain/provider.ErrNotFound":    {provider.ErrNotFound, connect.CodeNotFound},
	"domain/provider.ErrRateLimited": {provider.ErrRateLimited, connect.CodeUnavailable},
	"domain/provider.ErrQuotaExhausted": {
		provider.ErrQuotaExhausted, connect.CodeUnavailable,
	},
	"domain/provider.ErrUnavailable":              {provider.ErrUnavailable, connect.CodeUnavailable},
	"domain/provider.ErrBadSettings":              {provider.ErrBadSettings, connect.CodeFailedPrecondition},
	"domain/source.ErrNotFound":                   {source.ErrNotFound, connect.CodeNotFound},
	"domain/source.ErrUnknownType":                {source.ErrUnknownType, connect.CodeNotFound},
	"domain/source.ErrSignedOut":                  {source.ErrSignedOut, connect.CodeFailedPrecondition},
	"application/catalog.ErrPhotoNotUploaded":     {catalog.ErrPhotoNotUploaded, connect.CodeFailedPrecondition},
	"application/catalog.ErrInvalidScannedCopies": {catalog.ErrInvalidScannedCopies, connect.CodeInvalidArgument},
	"application/catalog.ErrMergeIntoItself":      {catalog.ErrMergeIntoItself, connect.CodeInvalidArgument},
	"application/catalog.ErrCopyAlreadyThere":     {catalog.ErrCopyAlreadyThere, connect.CodeInvalidArgument},
	"application/catalog.ErrScannedGameGone":      {catalog.ErrScannedGameGone, connect.CodeNotFound},
	"application/fields.ErrValuesDoNotFit":        {fields.ErrValuesDoNotFit, connect.CodeFailedPrecondition},
	"application/logs.ErrInvalidName":             {logs.ErrInvalidName, connect.CodeInvalidArgument},
	"application/logs.ErrNotFound":                {logs.ErrNotFound, connect.CodeNotFound},
	"application/media.ErrNoEdition":              {media.ErrNoEdition, connect.CodeNotFound},
	"application/media.ErrUnknownProvider":        {media.ErrUnknownProvider, connect.CodeNotFound},
	"application/media.ErrUnknownStore":           {media.ErrUnknownStore, connect.CodeNotFound},
	"application/media.ErrInvalidPhoto":           {media.ErrInvalidPhoto, connect.CodeInvalidArgument},
	"application/ownership.ErrTooManyItems":       {ownership.ErrTooManyItems, connect.CodeInvalidArgument},
	"application/sync.ErrNotImported":             {sync.ErrNotImported, connect.CodeFailedPrecondition},
	"application/sync.ErrNotExcluded":             {sync.ErrNotExcluded, connect.CodeNotFound},
	"application/valuation.ErrNotValuable":        {valuation.ErrNotValuable, connect.CodeFailedPrecondition},
	"application/valuation.ErrNoProviders":        {valuation.ErrNoProviders, connect.CodeFailedPrecondition},
	"application/valuation.ErrChanged":            {valuation.ErrChanged, connect.CodeAborted},
}

// internalOnly lists the sentinels that never reach the API, and why: they need no code.
var internalOnly = map[string]string{
	"application/health.ErrUnsupported":    "a disk check answers it as a health reason, never as an RPC error",
	"application/plugin.ErrDuplicate":      "a programming error found when the server starts",
	"application/media.ErrNoCover":         "HTTP only: the cover endpoint answers it with a 404",
	"application/media.ErrImageNotAllowed": "HTTP only: the image proxy answers it with a 403",
	"application/media.ErrNoAsset":         "HTTP only: the image endpoints answer it with a 404",
	"application/media.ErrNoPhoto":         "HTTP only: the photo endpoint answers it with a 404",
	"application/valuation.ErrNotListed": "a price provider's answer that the valuation service turns " +
		"into \"no estimate\"",
}

// eachFile parses the non-test Go files under the given directories of internal/ and calls fn with
// each file and its package path under internal/ ("adapters/outbound/cex").
func eachFile(t *testing.T, dirs []string, fn func(pkg string, f *ast.File)) {
	t.Helper()

	root := filepath.Join("..", "..", "..")

	for _, dir := range dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}

			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}

			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}

			fn(filepath.ToSlash(rel), f)

			return nil
		})
		require.NoError(t, err)
	}
}

// eachSentinel calls fn with every exported package-level variable whose name starts with Err, by
// "<package>.<name>", and the expression it is set to (nil when it has none).
func eachSentinel(t *testing.T, dirs []string, fn func(name string, value ast.Expr)) {
	t.Helper()

	eachFile(t, dirs, func(pkg string, f *ast.File) {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}

			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if !strings.HasPrefix(n.Name, "Err") || !n.IsExported() {
						continue
					}

					var value ast.Expr
					if i < len(vs.Values) {
						value = vs.Values[i]
					}

					fn(pkg+"."+n.Name, value)
				}
			}
		}
	})
}

// declaredSentinels returns every exported sentinel error of internal/domain and
// internal/application.
func declaredSentinels(t *testing.T) []string {
	t.Helper()

	var names []string

	eachSentinel(t, []string{"domain", "application"}, func(name string, _ ast.Expr) {
		names = append(names, name)
	})

	return names
}

// withoutKind reports whether a sentinel's value is made without a kind: errors.New, or
// fmt.Errorf that wraps nothing.
func withoutKind(value ast.Expr) bool {
	call, ok := value.(*ast.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	switch {
	case pkg.Name == "errors" && sel.Sel.Name == "New":
		return true
	case pkg.Name == "fmt" && sel.Sel.Name == "Errorf" && len(call.Args) > 0:
		lit, ok := call.Args[0].(*ast.BasicLit)
		return ok && !strings.Contains(lit.Value, "%w")
	default:
		return false
	}
}

// adapterSentinelsWithoutKind lists the adapters' sentinels that may stay without a kind, and why.
var adapterSentinelsWithoutKind = map[string]string{
	"adapters/outbound/apiclient.ErrNotJSON": "plumbing: each adapter classifies it (a bot check page, " +
		"a changed API) before it leaves the adapter",
	"adapters/outbound/apiclient.ErrTokenRejected": "plumbing: WithRetry renews the token and turns a " +
		"second refusal into a sign-out",
	"adapters/outbound/apiclient.ErrTooLarge": "plumbing: an answer over the size limit is a failure " +
		"of the integration, not of the request",
}

func TestSentinels_haveACode(t *testing.T) {
	t.Run("GIVEN every exported sentinel error of the domain and application packages", func(t *testing.T) {
		declared := declaredSentinels(t)
		require.NotEmpty(t, declared)

		t.Run("THEN each one is listed, with a code or as never reaching the API", func(t *testing.T) {
			for _, name := range declared {
				_, listed := sentinels[name]
				_, internal := internalOnly[name]
				assert.True(t, listed || internal, "%s has no code: add it to sentinels in this file", name)
			}
		})

		t.Run("AND each listed one maps to its code, never to Internal", func(t *testing.T) {
			for name, s := range sentinels {
				code := connect.CodeOf(toConnectError(s.err))
				assert.NotEqual(t, connect.CodeInternal, code, name)
				assert.Equal(t, s.code, code, name)
			}
		})
	})
}

func TestToConnectError_byKind(t *testing.T) {
	errDisk := errors.New("disk I/O error")

	tests := []struct {
		name string
		err  error
		want connect.Code
	}{
		{"invalid input", errkind.Invalidf("the title is required"), connect.CodeInvalidArgument},
		{"not found", errkind.New(errkind.NotFound, "x"), connect.CodeNotFound},
		{"already exists", errkind.New(errkind.AlreadyExists, "x"), connect.CodeAlreadyExists},
		{"conflict", errkind.New(errkind.Conflict, "x"), connect.CodeAborted},
		{"unauthenticated", errkind.New(errkind.Unauthenticated, "x"), connect.CodeUnauthenticated},
		{"permission denied", errkind.New(errkind.PermissionDenied, "x"), connect.CodePermissionDenied},
		{"precondition", errkind.New(errkind.Precondition, "x"), connect.CodeFailedPrecondition},
		{"exhausted", errkind.New(errkind.Exhausted, "x"), connect.CodeResourceExhausted},
		{"unavailable", errkind.New(errkind.Unavailable, "x"), connect.CodeUnavailable},
		{"a kind wrapped in context", fmt.Errorf("saving: %w", errkind.New(errkind.NotFound, "x")), connect.CodeNotFound},
		{"a kind given by Wrap", errkind.Wrap(errkind.Invalid, errDisk), connect.CodeInvalidArgument},
		{"an error without a kind", errDisk, connect.CodeInternal},
		{"a store that signed the user out", source.SignedOut("sign in again"), connect.CodeFailedPrecondition},
		{"a connect error as it is", connect.NewError(connect.CodeCanceled, errDisk), connect.CodeCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, connect.CodeOf(toConnectError(tt.err)))
		})
	}

	t.Run("no error is no error", func(t *testing.T) {
		assert.NoError(t, toConnectError(nil))
	})
}

func TestKindCodes_coverEveryKind(t *testing.T) {
	for _, k := range errkind.All() {
		_, ok := kindCodes[k]
		assert.True(t, ok, "kind %q has no Connect code", k)
	}
}

func TestAdapterSentinels_haveAKind(t *testing.T) {
	t.Run("GIVEN every exported sentinel error of the outbound adapters", func(t *testing.T) {
		var seen int

		eachSentinel(t, []string{"adapters/outbound"}, func(name string, value ast.Expr) {
			seen++

			t.Run("THEN "+name+" is made with a kind, or allowed without one for a reason", func(t *testing.T) {
				_, allowed := adapterSentinelsWithoutKind[name]
				assert.False(t, withoutKind(value) && !allowed,
					"%s is a plain errors.New: make it with errkind.New (or wrap a sentinel that has a kind)", name)
			})
		})

		require.NotZero(t, seen)
	})
}

func TestErrorTypesWithIs_haveAKind(t *testing.T) {
	t.Run("GIVEN every error type with its own Is method", func(t *testing.T) {
		methods := map[string]map[string]bool{} // "<package>.<type>" → its method names

		eachFile(t, []string{"domain", "application", "adapters/outbound"}, func(pkg string, f *ast.File) {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
					continue
				}

				recv := fd.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}

				id, ok := recv.(*ast.Ident)
				if !ok {
					continue
				}

				key := pkg + "." + id.Name
				if methods[key] == nil {
					methods[key] = map[string]bool{}
				}

				methods[key][fd.Name.Name] = true
			}
		})

		t.Run("THEN each one also says its kind, since errors.Is matching does not carry it", func(t *testing.T) {
			for typ, m := range methods {
				if m["Is"] && m["Error"] {
					assert.True(t, m["Kind"], "%s has Is but no Kind method", typ)
				}
			}
		})
	})
}
