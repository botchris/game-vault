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

func TestKinds(t *testing.T) {
	t.Run("GIVEN every kind of provider", func(t *testing.T) {
		t.Run("THEN the Providers page lists the home page's deals and releases after the others", func(t *testing.T) {
			assert.Equal(t, []Kind{KindCover, KindBarcode, KindMetadata, KindValuation, KindDeals, KindReleases}, Kinds())
		})

		t.Run("AND only prices, deals and releases are asked on a schedule", func(t *testing.T) {
			want := map[Kind]bool{
				KindCover:     false,
				KindBarcode:   false,
				KindMetadata:  false,
				KindValuation: true,
				KindDeals:     true,
				KindReleases:  true,
			}

			for _, k := range Kinds() {
				assert.Equal(t, want[k], k.Scheduled(), k)
			}
		})
	})
}
