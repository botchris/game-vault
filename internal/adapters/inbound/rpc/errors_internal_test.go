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
	"domain/auth.ErrLockout":         {auth.ErrLockout, connect.CodeInvalidArgument},
	"domain/field.ErrNotFound":       {field.ErrNotFound, connect.CodeNotFound},
	"domain/game.ErrGameNotFound":    {game.ErrGameNotFound, connect.CodeNotFound},
	"domain/game.ErrCopyNotFound":    {game.ErrCopyNotFound, connect.CodeNotFound},
	"domain/game.ErrPhotoNotFound":   {game.ErrPhotoNotFound, connect.CodeNotFound},
	"domain/game.ErrInvalidBarcode":  {game.ErrInvalidBarcode, connect.CodeInvalidArgument},
	"domain/provider.ErrNotFound":    {provider.ErrNotFound, connect.CodeNotFound},
	"domain/provider.ErrRateLimited": {provider.ErrRateLimited, connect.CodeUnavailable},
	"domain/provider.ErrQuotaExhausted": {
		provider.ErrQuotaExhausted, connect.CodeUnavailable,
	},
	"domain/provider.ErrUnavailable":              {provider.ErrUnavailable, connect.CodeUnavailable},
	"domain/provider.ErrBadSettings":              {provider.ErrBadSettings, connect.CodeFailedPrecondition},
	"domain/source.ErrNotFound":                   {source.ErrNotFound, connect.CodeNotFound},
	"domain/source.ErrUnknownType":                {source.ErrUnknownType, connect.CodeInvalidArgument},
	"domain/source.ErrSignedOut":                  {source.ErrSignedOut, connect.CodeFailedPrecondition},
	"application/catalog.ErrPhotoNotUploaded":     {catalog.ErrPhotoNotUploaded, connect.CodeFailedPrecondition},
	"application/catalog.ErrInvalidScannedCopies": {catalog.ErrInvalidScannedCopies, connect.CodeInvalidArgument},
	"application/catalog.ErrMergeIntoItself":      {catalog.ErrMergeIntoItself, connect.CodeInvalidArgument},
	"application/catalog.ErrCopyAlreadyThere":     {catalog.ErrCopyAlreadyThere, connect.CodeInvalidArgument},
	"application/catalog.ErrScannedGameGone":      {catalog.ErrScannedGameGone, connect.CodeNotFound},
	"application/fields.ErrValuesConflict":        {fields.ErrValuesConflict, connect.CodeFailedPrecondition},
	"application/logs.ErrInvalidName":             {logs.ErrInvalidName, connect.CodeInvalidArgument},
	"application/logs.ErrNotFound":                {logs.ErrNotFound, connect.CodeNotFound},
	"application/media.ErrNoCover":                {media.ErrNoCover, connect.CodeNotFound},
	"application/media.ErrNoEdition":              {media.ErrNoEdition, connect.CodeInvalidArgument},
	"application/media.ErrUnknownProvider":        {media.ErrUnknownProvider, connect.CodeNotFound},
	"application/media.ErrImageNotAllowed":        {media.ErrImageNotAllowed, connect.CodePermissionDenied},
	"application/media.ErrUnknownStore":           {media.ErrUnknownStore, connect.CodeNotFound},
	"application/media.ErrNoAsset":                {media.ErrNoAsset, connect.CodeNotFound},
	"application/media.ErrNoPhoto":                {media.ErrNoPhoto, connect.CodeNotFound},
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
	"application/health.ErrUnsupported": "a disk check answers it as a health reason, never as an RPC error",
	"application/plugin.ErrDuplicate":   "a programming error found when the server starts",
	"application/valuation.ErrNotListed": "a price provider's answer that the valuation service turns " +
		"into \"no estimate\"",
}

// declaredSentinels parses the Go files under internal/domain and internal/application and returns
// every exported package-level variable whose name starts with Err.
func declaredSentinels(t *testing.T) []string {
	t.Helper()

	root := filepath.Join("..", "..", "..")

	var names []string

	for _, dir := range []string{"domain", "application"} {
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

			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}

				for _, spec := range gd.Specs {
					for _, n := range spec.(*ast.ValueSpec).Names {
						if strings.HasPrefix(n.Name, "Err") && n.IsExported() {
							names = append(names, filepath.ToSlash(rel)+"."+n.Name)
						}
					}
				}
			}

			return nil
		})
		require.NoError(t, err)
	}

	return names
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
	for k := errkind.Unknown + 1; k.String() != "unknown"; k++ {
		_, ok := kindCodes[k]
		assert.True(t, ok, "kind %q has no Connect code", k)
	}
}
