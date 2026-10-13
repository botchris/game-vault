package thegamesdb

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/errkind"
)

func TestErrors_kind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errkind.Kind
	}{
		{"an unknown key", ErrUnknownKey, errkind.Precondition},
		{"an unknown key answered with a 429", errUnknownKeyLimited, errkind.Precondition},
		{"a used-up allowance", ErrNoAllowance, errkind.Unavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errkind.Of(tt.err))
		})
	}
}
