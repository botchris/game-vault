package apiclient

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/provider"
)

func TestWithoutQuery_keepsTheKind(t *testing.T) {
	t.Run("GIVEN a classified transport error whose URL holds a key", func(t *testing.T) {
		transport := &url.Error{
			Op:  "Get",
			URL: "https://api.example.test/v1/Games?apikey=secret",
			Err: errors.New("connection refused"),
		}
		err := fmt.Errorf("not answering (%w): %w", transport, provider.ErrUnavailable)

		t.Run("WHEN the query is dropped", func(t *testing.T) {
			got := withoutQuery(err)

			t.Run("THEN it is still Unavailable", func(t *testing.T) {
				var q *queryless
				require.ErrorAs(t, got, &q)
				assert.NotContains(t, got.Error(), "secret")
				assert.Equal(t, errkind.Unavailable, errkind.Of(got))
			})
		})
	})
}
