package unattended_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/unattended"
)

func TestRhythm_draws(t *testing.T) {
	tests := []struct {
		name    string
		random  float64
		vary    time.Duration
		between time.Duration
	}{
		{"lowest draw", 0, 54 * time.Minute, 2 * time.Second},
		{"middle draw", 0.5, time.Hour, 3 * time.Second},
		{"high draw", 0.75, 63 * time.Minute, 3500 * time.Millisecond},
	}

	for _, tt := range tests {
		r := unattended.Rhythm{Random: func() float64 { return tt.random }}

		assert.Equal(t, tt.vary, r.Vary(time.Hour, 0.1), tt.name)
		assert.Equal(t, tt.between, r.Between(2*time.Second, 4*time.Second), tt.name)
	}
}

func TestRhythm_zeroValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the zero rhythm", func(t *testing.T) {
		var r unattended.Rhythm

		t.Run("WHEN it draws many varied durations", func(t *testing.T) {
			t.Run("THEN each stays within the range", func(t *testing.T) {
				for range 1000 {
					d := r.Vary(time.Hour, 0.1)
					assert.GreaterOrEqual(t, d, 54*time.Minute)
					assert.LessOrEqual(t, d, 66*time.Minute)
				}
			})
		})

		t.Run("WHEN it waits", func(t *testing.T) {
			t.Run("THEN a short wait completes", func(t *testing.T) {
				assert.True(t, r.Wait(ctx, time.Millisecond))
			})

			t.Run("OR a wait ends early, reporting false, when the context ends", func(t *testing.T) {
				done, stop := context.WithCancel(ctx)
				stop()

				assert.False(t, r.Wait(done, time.Hour))
			})
		})
	})
}
