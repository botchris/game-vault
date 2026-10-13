package apiclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		status      int
		limited     bool
		unavailable bool
	}{
		{http.StatusTooManyRequests, true, false},
		{http.StatusInternalServerError, true, true},
		{http.StatusBadGateway, true, true},
		{http.StatusServiceUnavailable, true, true},
		{http.StatusNotFound, false, false},
		{http.StatusBadRequest, false, false},
		{http.StatusUnauthorized, false, false},
		{http.StatusForbidden, false, false},
	}
	for _, c := range cases {
		err := ClassifyStatus(c.status)
		require.Error(t, err, c.status)
		assert.Equal(t, c.limited, errors.Is(err, provider.ErrRateLimited), c.status)
		assert.Equal(t, c.unavailable, errors.Is(err, provider.ErrUnavailable), c.status)
		assert.False(t, errors.Is(err, provider.ErrBadSettings), c.status)
		assert.Contains(t, err.Error(), fmt.Sprintf("HTTP %d", c.status))
	}
}

func TestRejected(t *testing.T) {
	for _, s := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		assert.True(t, Rejected(s), s)
	}

	for _, s := range []int{http.StatusNotFound, http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError} {
		assert.False(t, Rejected(s), s)
	}
}

func TestClassifyError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an answer with an HTTP status", func(t *testing.T) {
		statusErr := func(status int) error {
			return &StatusError{
				Method: http.MethodGet,
				URL:    "https://api.example.com/v1/games",
				Status: status,
			}
		}

		t.Run("THEN a 429 is a rate limit", func(t *testing.T) {
			err := ClassifyError(ctx, statusErr(http.StatusTooManyRequests))
			require.ErrorIs(t, err, provider.ErrRateLimited)
			assert.NotErrorIs(t, err, provider.ErrUnavailable)
		})

		t.Run("AND a 5xx is an outage", func(t *testing.T) {
			require.ErrorIs(t, ClassifyError(ctx, statusErr(http.StatusBadGateway)), provider.ErrUnavailable)
		})

		t.Run("AND a 404 or a 401 is left as it is", func(t *testing.T) {
			for _, s := range []int{http.StatusNotFound, http.StatusUnauthorized} {
				err := ClassifyError(ctx, statusErr(s))
				assert.NotErrorIs(t, err, provider.ErrRateLimited)
				assert.True(t, IsStatus(err, s))
			}
		})
	})

	t.Run("GIVEN no answer", func(t *testing.T) {
		raw := &url.Error{
			Op:  "Get",
			URL: "https://api.example.com/api?token=s3cret&op=lookup",
			Err: errors.New("connection reset by peer"),
		}

		t.Run("WHEN it is classified", func(t *testing.T) {
			err := ClassifyError(ctx, raw)

			t.Run("THEN it is an outage that does not show the query", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrUnavailable)
				assert.NotContains(t, err.Error(), "s3cret")
				assert.Contains(t, err.Error(), "connection reset by peer")
			})
		})

		t.Run("WHEN the caller gave up", func(t *testing.T) {
			done, stop := context.WithCancel(ctx)
			stop()

			err := ClassifyError(done, raw)

			t.Run("THEN it is not an outage", func(t *testing.T) {
				assert.NotErrorIs(t, err, provider.ErrRateLimited)
				assert.NotContains(t, err.Error(), "s3cret")
			})
		})
	})

	t.Run("GIVEN a web page instead of JSON", func(t *testing.T) {
		require.ErrorIs(t, ClassifyError(ctx, ErrNotJSON), provider.ErrUnavailable)
	})

	t.Run("GIVEN an error already classified or none", func(t *testing.T) {
		bad := provider.BadSettings("the key was rejected")
		assert.Equal(t, bad, ClassifyError(ctx, bad))
		require.NoError(t, ClassifyError(ctx, nil))
	})
}
