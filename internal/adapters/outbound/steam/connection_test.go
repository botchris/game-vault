package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

func TestStoreTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a store browse API that knows Portal 2", func(t *testing.T) {
		var gotInput string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotInput = r.URL.Query().Get("input_json")

			w.Write([]byte(`{"response":{"store_items":[{"appid":620,"assets":{"asset_url_format":"steam/apps/620/${FILENAME}?t=1","library_capsule":"abc/library_600x900.jpg","header":"def/header.jpg"}}]}}`))
		}))
		defer srv.Close()

		s := &Store{APIURL: srv.URL, AssetsURL: "https://assets.test/", Client: srv.Client()}

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := s.Test(ctx, schema.Settings{})

			t.Run("THEN it passes after asking for app 620", func(t *testing.T) {
				require.NoError(t, err)
				assert.Contains(t, gotInput, `"appid":620`)
			})
		})
	})

	t.Run("GIVEN a store browse API that fails with HTTP 503", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		s := &Store{APIURL: srv.URL, Client: srv.Client()}

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := s.Test(ctx, schema.Settings{})

			t.Run("THEN it fails and says what to do", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "HTTP 503")
				assert.Contains(t, err.Error(), "try again")
			})
		})
	})
}

func TestDetailsTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a store that answers for Portal 2", func(t *testing.T) {
		var gotAppIDs string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAppIDs = r.URL.Query().Get("appids")

			w.Write([]byte(`{"620":{"success":true,"data":{"name":"Portal 2","short_description":"Test chambers."}}}`))
		}))
		defer srv.Close()

		d := NewDetails(&Store{StoreURL: srv.URL, Client: srv.Client()})

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := d.Test(ctx, schema.Settings{})

			t.Run("THEN it passes after asking for app 620", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "620", gotAppIDs)
			})
		})
	})

	t.Run("GIVEN a store that rate-limits with HTTP 429", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		d := NewDetails(&Store{StoreURL: srv.URL, Client: srv.Client()})

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := d.Test(ctx, schema.Settings{})

			t.Run("THEN it fails with the rate limit message", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "too many requests")
			})
		})
	})
}
