package media_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/media"
)

func TestDownloads_withoutFetcher(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN downloads without a fetcher", func(t *testing.T) {
		d := media.NewDownloads(nil)

		t.Run("WHEN an image is fetched", func(t *testing.T) {
			_, err := d.Fetch(ctx, "https://img.test/cover.jpg")

			t.Run("THEN it fails cleanly and frees its slot", func(t *testing.T) {
				assert.Error(t, err)
				assert.False(t, d.Busy())
			})
		})
	})
}
