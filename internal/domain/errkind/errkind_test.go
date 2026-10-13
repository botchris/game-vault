package errkind_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/errkind"
)

// errBare is an error without a kind.
var errBare = errors.New("disk full")

// errSentinel is a sentinel with a kind.
var errSentinel = errkind.New(errkind.NotFound, "game not found")

// ownKind is an error type that states its kind itself, as source.SignedOut's does.
type ownKind struct{}

func (ownKind) Error() string { return "signed out" }

func (ownKind) Kind() errkind.Kind { return errkind.Precondition }

func TestOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errkind.Kind
	}{
		{"nil", nil, errkind.Unknown},
		{"an error without a kind", errBare, errkind.Unknown},
		{"a sentinel", errSentinel, errkind.NotFound},
		{"a wrapped sentinel", fmt.Errorf("loading: %w", errSentinel), errkind.NotFound},
		{"a validation error", errkind.Invalidf("title is %s", "required"), errkind.Invalid},
		{"an error type with a Kind method", fmt.Errorf("scan: %w", ownKind{}), errkind.Precondition},
		{"an error given a kind by Wrap", errkind.Wrap(errkind.Invalid, errBare), errkind.Invalid},
		{"the outermost kind wins", errkind.Wrap(errkind.Unavailable, errSentinel), errkind.Unavailable},
		{"Wrap with Unknown hides the kinds inside", errkind.Wrap(errkind.Unknown, errSentinel), errkind.Unknown},
		{"Wrap leaves a canceled request alone", errkind.Wrap(errkind.Unavailable, fmt.Errorf("search: %w", context.Canceled)), errkind.Unknown},
		{"Wrap leaves a deadline alone", errkind.Wrap(errkind.Unavailable, context.DeadlineExceeded), errkind.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errkind.Of(tt.err))
		})
	}
}

func TestWrap_keepsTheError(t *testing.T) {
	t.Run("GIVEN an error wrapped with a kind", func(t *testing.T) {
		err := errkind.Wrap(errkind.Invalid, errSentinel)

		t.Run("THEN its message and identity are the wrapped error's", func(t *testing.T) {
			assert.Equal(t, "game not found", err.Error())
			assert.ErrorIs(t, err, errSentinel)
		})
	})

	t.Run("GIVEN no error", func(t *testing.T) {
		t.Run("THEN Wrap returns nil", func(t *testing.T) {
			assert.NoError(t, errkind.Wrap(errkind.Invalid, nil))
		})
	})
}

func TestNew_sentinelsAreDistinct(t *testing.T) {
	a := errkind.New(errkind.NotFound, "not found")
	b := errkind.New(errkind.NotFound, "not found")

	assert.NotErrorIs(t, a, b)
	assert.ErrorIs(t, fmt.Errorf("x: %w", a), a)
}

func TestValidationError_matchesWithErrorsAs(t *testing.T) {
	var v *errkind.ValidationError

	assert.ErrorAs(t, fmt.Errorf("saving: %w", errkind.Invalidf("bad")), &v)
	assert.Equal(t, "bad", v.Error())
}

func TestAll_namesEveryKind(t *testing.T) {
	all := errkind.All()

	assert.NotContains(t, all, errkind.Unknown)
	assert.Contains(t, all, errkind.Invalid)
	assert.Contains(t, all, errkind.Unavailable)

	for _, k := range all {
		assert.NotEqual(t, "unknown", k.String(), "kind %d has no name", k)
	}
}
