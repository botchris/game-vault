package notifyerr_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/adapters/outbound/notifyerr"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/errkind"
)

func TestPermanent(t *testing.T) {
	t.Run("GIVEN a permanent failure, wrapped", func(t *testing.T) {
		err := fmt.Errorf("sending: %w", notifyerr.Permanent("the service rejected the token: paste a new one"))

		t.Run("THEN it is notify.ErrPermanent, an Invalid error, with its message", func(t *testing.T) {
			assert.ErrorIs(t, err, notify.ErrPermanent)
			assert.Equal(t, errkind.Invalid, errkind.Of(err))
			assert.Equal(t, "sending: the service rejected the token: paste a new one", err.Error())
		})
	})

	t.Run("GIVEN a connector's own error type embedding PermanentError", func(t *testing.T) {
		type mediaError struct{ notifyerr.PermanentError }

		var err error = &mediaError{notifyerr.PermanentError("the image was refused")}

		t.Run("THEN it is permanent and Invalid too", func(t *testing.T) {
			assert.ErrorIs(t, err, notify.ErrPermanent)
			assert.Equal(t, errkind.Invalid, errkind.Of(err))
		})
	})
}

func TestRateLimited(t *testing.T) {
	t.Run("GIVEN a rate limit asking to wait 30 seconds", func(t *testing.T) {
		err := fmt.Errorf("sending: %w", notifyerr.RateLimited("the service asks to slow down", 30*time.Second))

		t.Run("THEN it says how long to wait and is not permanent", func(t *testing.T) {
			var limited interface{ RetryAfter() time.Duration }
			assert.True(t, errors.As(err, &limited))
			assert.Equal(t, 30*time.Second, limited.RetryAfter())
			assert.NotErrorIs(t, err, notify.ErrPermanent)
			assert.Equal(t, "sending: the service asks to slow down", err.Error())
		})
	})
}
