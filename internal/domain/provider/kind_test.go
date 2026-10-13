package provider

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/errkind"
)

func TestErrorTypes_kind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errkind.Kind
	}{
		{"a used-up allowance", ErrQuotaExhausted, errkind.Unavailable},
		{"a service not answering", ErrUnavailable, errkind.Unavailable},
		{"settings the service rejects", BadSettings("check the key"), errkind.Precondition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errkind.Of(fmt.Errorf("covers: %w", tt.err)))
		})
	}
}
