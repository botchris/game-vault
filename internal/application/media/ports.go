package media

import (
	"context"
	"time"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ErrNoCover means no image could be found for the game.
var ErrNoCover = errkind.New(errkind.NotFound, "no cover available")

// ErrNoEdition means the game has no copy on the system asked for.
var ErrNoEdition = errkind.New(errkind.NotFound, "the game has no copy on that system: reload the page")

// Image is an encoded image.
type Image struct {
	Data        []byte
	ContentType string
}

// AssetStore is the port that keeps every downloaded image of a game in one place (one folder per
// game, Plex-style): its cover, the "no cover found" marker, and the images of its details sheet,
// with a record of where each one came from so it can be downloaded again if it goes missing.
type AssetStore interface {
	// GetCover returns the stored cover of a game's edition, and whether there is one.
	GetCover(id game.ID, system string) (Image, bool, error)

	// PutCover stores the cover of a game's edition, replacing the previous one.
	PutCover(g GameRef, system string, img Image) error

	// MarkCoverMissing remembers that no cover was found for an edition, so the lookup is not
	// repeated on every request.
	MarkCoverMissing(g GameRef, system string, at time.Time) error

	// CoverMissingSince returns when no cover was last found for an edition, and whether that was
	// recorded.
	CoverMissingSince(id game.ID, system string) (time.Time, bool)

	// ClearCoverMissing forgets that no cover was found for an edition, so it is looked up again;
	// stored covers stay.
	ClearCoverMissing(id game.ID, system string) error

	// PruneCoverMarkers removes the "no cover found" markers of the editions not on systems, and the
	// one from before editions, and reports how many it removed. Stored covers stay.
	PruneCoverMarkers(id game.ID, systems []string) (int, error)

	// DeleteCover removes an edition's stored cover and "no cover found" marker; other editions'
	// covers and the cover stored before editions stay.
	DeleteCover(id game.ID, system string) error

	// DeleteLegacyCover removes the cover stored before editions and its "no cover found" marker.
	DeleteLegacyCover(id game.ID) error

	// HasLegacyCover reports whether the cover stored before editions, or its "no cover found"
	// marker, is still there.
	HasLegacyCover(id game.ID) bool

	// LegacyCoverIDs returns the games whose cover stored before editions, or its "no cover found"
	// marker, is still there.
	LegacyCoverIDs() []game.ID

	// DeleteCovers removes the stored covers and markers of every edition of a game.
	DeleteCovers(id game.ID) error

	// AdoptLegacyCover makes the cover stored before editions the cover of the edition on system,
	// and reports whether there was one.
	AdoptLegacyCover(g GameRef, system string) (Image, bool, error)

	// GetAsset returns a stored sheet image of a game, and whether there is one.
	GetAsset(id game.ID, name string) (Image, bool, error)

	// PutAsset stores a sheet image of a game under the given name.
	PutAsset(g GameRef, name string, img Image) error

	// SetAssetSources records name → remote URL for the game's sheet images, replacing the previous
	// set; images no longer referenced are deleted.
	SetAssetSources(g GameRef, sources map[string]string) error

	// AssetSource returns the remote URL a sheet image was downloaded from, and whether it is known.
	AssetSource(id game.ID, name string) (string, bool)

	// DeleteAll removes everything stored for the game.
	DeleteAll(id game.ID) error
}

// ImageFetcher is the port that downloads an image from a URL.
type ImageFetcher interface {
	// Fetch downloads the image at url.
	Fetch(ctx context.Context, url string) (Image, error)
}

// CoverCandidate is one image a provider proposes.
type CoverCandidate struct {
	URL      string
	ThumbURL string
	Label    string

	// Title is the provider's canonical title for the game the image belongs to, when it knows it.
	Title    string
	Provider provider.ID
}

// CoverProvider is the port each cover source implements (TheGamesDB, Steam...).
type CoverProvider interface {
	providers.Impl

	// Applies reports, without any network call, whether the provider can have a cover for q.
	// It keeps quota-limited providers away from games they cannot help with.
	Applies(q GameQuery) bool

	// Covers returns candidate images, best first. No candidates is not an error.
	Covers(ctx context.Context, q GameQuery, settings schema.Settings) ([]CoverCandidate, error)
}

// BarcodeMatch is what a barcode database knows about a product.
type BarcodeMatch struct {
	// Raw is the product name as the database has it, e.g. "Assassin's Creed Iii Ed. Special Ps3(sp)".
	Raw string

	// Title, Platform and Edition are extracted from Raw (see game.ParseProductName).
	Title    string
	Platform string
	Edition  string
	ImageURL string
	Provider provider.ID
}

// BarcodeProvider is the port each barcode database implements (UPCitemdb, EAN-Search...).
type BarcodeProvider interface {
	providers.Impl

	// Lookup returns the products registered under the code, best first. No match is not an error.
	Lookup(ctx context.Context, code game.Barcode, settings schema.Settings) ([]BarcodeMatch, error)
}

// ImageHoster is an optional port for providers whose images the browser may load through the
// image proxy. Many CDNs refuse hotlinked images, so the UI never loads them directly.
type ImageHoster interface {
	// ImageHosts returns the hosts whose images the proxy may fetch for this provider.
	ImageHosts() []string
}

// ErrImageNotAllowed means the proxy was asked for an image outside the providers' hosts.
var ErrImageNotAllowed = errkind.New(errkind.PermissionDenied, "image host not allowed")

// LinkStore is a store games can be linked to, as the media providers know it.
type LinkStore struct {
	game.Store

	// Searchable reports that a provider can search the store's catalog (SearchLinks).
	Searchable bool
}

// LinkMatch is a game found in a store's catalog.
type LinkMatch struct {
	// ID is the game's id in the store, the value of its link.
	ID       string
	Name     string
	ImageURL string
}

// StoreLinker is an optional port for providers that find a game by its link to a store.
type StoreLinker interface {
	// LinkStore describes the store whose links the provider reads.
	LinkStore() game.Store
}

// LinkFinder is an optional port for providers that can name, without doubt, the store ids of the
// game a query describes (IGDB's external games of a game it matched, Steam's only app with exactly
// that title). Their answers become automatic links (see game.Game.AutoLinks).
type LinkFinder interface {
	// FindLinks returns the store links of the game q describes, from a match the provider trusts;
	// none when it cannot tell. Errors are those of the provider's other calls (rate limits…).
	FindLinks(ctx context.Context, q GameQuery, settings schema.Settings) (game.Links, error)
}

// LinkSearcher is an optional port for providers that can also search their store's catalog by
// title, so the user (and the add-on cover lookup) can link a game to it.
type LinkSearcher interface {
	StoreLinker

	// SearchLinks returns the store's games whose title matches the query, best first.
	SearchLinks(ctx context.Context, query string) ([]LinkMatch, error)
}

// ErrUnknownStore means a search named a store no provider can search.
var ErrUnknownStore = errkind.New(errkind.NotFound, "no provider can search that store")

// GameRef points to a game already in the catalog.
type GameRef struct {
	ID    game.ID
	Title string
}
