package media

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"gamevault/internal/application/providers"
)

// ImageProxy fetches providers' images for the browser, which never loads them from their hosts:
// many CDNs refuse hotlinked images.
type ImageProxy struct {
	reg   *providers.Registry
	fetch ImageFetcher
}

// NewImageProxy builds the proxy of the images of the providers in reg.
func NewImageProxy(reg *providers.Registry, fetch ImageFetcher) *ImageProxy {
	return &ImageProxy{
		reg:   reg,
		fetch: fetch,
	}
}

// Fetch downloads an image for the browser. Only https URLs on hosts declared by a provider
// (ImageHoster) are allowed, so the proxy cannot be used to reach arbitrary or internal addresses.
func (p *ImageProxy) Fetch(ctx context.Context, rawURL string) (Image, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !p.imageHostAllowed(u.Hostname()) {
		return Image{}, ErrImageNotAllowed
	}

	return p.fetch.Fetch(ctx, u.String())
}

// imageHostAllowed reports whether a provider declares host. Disabled providers count too, on
// purpose: the store-link search (media.LinkSearcher) shows thumbnails from stores whose cover
// provider may be off, and a cover already proposed must keep showing while the chain changes. The
// list is fixed at build time (a few image CDNs), so this opens no other address; covers the user
// pastes as a URL are downloaded by the server, not through this proxy.
func (p *ImageProxy) imageHostAllowed(host string) bool {
	for _, impl := range p.reg.All() {
		if h, ok := impl.(ImageHoster); ok && slices.Contains(h.ImageHosts(), strings.ToLower(host)) {
			return true
		}
	}

	return false
}
