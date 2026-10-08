// Package imagefetch downloads images over HTTP (implements media.ImageFetcher).
package imagefetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gamevault/internal/application/media"
)

// maxBytes bounds a cover download.
const maxBytes = 8 << 20

// Fetcher implements media.ImageFetcher.
type Fetcher struct{ Client *http.Client }

var _ media.ImageFetcher = (*Fetcher)(nil)

func New() *Fetcher { return &Fetcher{Client: &http.Client{Timeout: 20 * time.Second}} }

func (f *Fetcher) Fetch(ctx context.Context, url string) (media.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return media.Image{}, err
	}
	req.Header.Set("User-Agent", "GameVault/1.0 (+self-hosted game inventory)")
	res, err := f.Client.Do(req)
	if err != nil {
		return media.Image{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return media.Image{}, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return media.Image{}, err
	}
	if len(data) > maxBytes {
		return media.Image{}, fmt.Errorf("image larger than %d MB", maxBytes>>20)
	}
	// Trust the bytes, not the header: servers often send octet-stream.
	ct := http.DetectContentType(data)
	if !strings.HasPrefix(ct, "image/") {
		return media.Image{}, fmt.Errorf("not an image (%s)", ct)
	}
	return media.Image{Data: data, ContentType: ct}, nil
}
