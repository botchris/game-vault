package media

import "context"

// downloadSlots is how many images are downloaded at once (covers and sheet images together):
// enough to fill a page of covers quickly, few enough that a slow CDN does not pile up requests.
const downloadSlots = 6

// Downloads downloads images with a limit on how many run at once, shared by everything that
// downloads one for a person waiting (covers, sheet images). Only the download holds a slot: a
// provider lookup does not, so lookups queued behind a slow or throttled provider never keep other
// covers waiting.
type Downloads struct {
	fetch ImageFetcher
	slots chan struct{}
}

// NewDownloads returns the downloads made through fetch.
func NewDownloads(fetch ImageFetcher) *Downloads {
	return &Downloads{
		fetch: fetch,
		slots: make(chan struct{}, downloadSlots),
	}
}

// Fetch downloads the image at url once a slot is free.
func (d *Downloads) Fetch(ctx context.Context, url string) (Image, error) {
	d.slots <- struct{}{}
	defer func() { <-d.slots }()

	return d.fetch.Fetch(ctx, url)
}

// Busy reports whether an image is being downloaded: background work (the auto-linker) waits until
// none is.
func (d *Downloads) Busy() bool { return len(d.slots) > 0 }
