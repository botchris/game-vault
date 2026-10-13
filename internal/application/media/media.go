// Package media implements what Game Vault shows about a game besides the user's own records: its
// covers, its sheet (summary, screenshots…) and their images, the identification of a scanned
// barcode or a typed title, the photos users take of their copies, and the store links found
// automatically.
//
// Each part is its own service; New builds them and wires them to each other. They all run the
// providers configured in the providers registry (internal/application/providers): covers are
// resolved through an ordered chain of cover providers (Plex-agent style) and cached in the config
// directory, so the UI never hot-links third-party sites and quotas are spent once.
//
// Options. Each service takes its own option type, so an option that would do nothing for it does not
// compile: NewCovers takes CoverOption values, NewDetails DetailsOption values, and so on. Each
// option type is an interface with one unexported method, which applies the option to that service.
//
// An option that several services understand is one concrete type implementing the option
// interface of each of them: ClockOption implements CoverOption, DetailsOption, PhotosOption and
// AutoLinkerOption, so WithClock(now) can be passed to any of those constructors, and only to them.
// Every option also implements Option, the option type of New, which hands each one to every
// service whose option interface it implements.
package media

import (
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
)

// defaultLanguage is the language sheets are gathered in when the UI names none: English is the
// fallback of every language-dependent feature.
const defaultLanguage = "en"

// Services are the media services, wired to each other.
type Services struct {
	// Registry is the providers registry the services run.
	Registry *providers.Registry

	// Covers resolves, caches and proposes the games' covers.
	Covers *Covers

	// Details gathers and caches the games' sheets, and scans for missing ones in the background.
	Details *Details

	// Assets serves the sheets' images.
	Assets *Assets

	// Identifier identifies scanned barcodes and typed titles.
	Identifier *Identifier

	// Photos keeps the photos users take of their copies.
	Photos *Photos

	// AutoLinks finds store links automatically.
	AutoLinks *AutoLinker

	// Proxy fetches the providers' images for the browser.
	Proxy *ImageProxy
}

// New builds the media services over the providers in reg and wires them to each other. games,
// store and fetch are required; opts are any of the services' options (WithPhotoStore,
// WithDetailsStore, WithRhythm, WithClock, WithLogger), each handed to every service it applies to.
func New(games game.Repository, reg *providers.Registry, store AssetStore, fetch ImageFetcher, opts ...Option) *Services {
	var cfg config
	for _, o := range opts {
		o.applyNew(&cfg)
	}

	if cfg.photos == nil {
		cfg.photos = noPhotos{}
	}

	if cfg.details == nil {
		cfg.details = noDetails{}
	}

	downloads := NewDownloads(fetch)

	// Links found for a game give it the stores' art: the auto-linker makes the covers forget what
	// they missed for it, so both get the same misses.
	missed := &misses{}

	autoLinks := NewAutoLinker(games, reg, append(optionsFor[AutoLinkerOption](opts), autoLinkerWiring(func(a *AutoLinker) {
		a.downloads, a.misses = downloads, missed
	}))...)
	assets := NewAssets(games, store, downloads, optionsFor[AssetsOption](opts)...)
	covers := NewCovers(games, reg, store, downloads, append(optionsFor[CoverOption](opts), coverWiring(func(c *Covers) {
		c.autoLinks, c.misses = autoLinks, missed
	}))...)
	details := NewDetails(games, reg, cfg.details, assets, append(optionsFor[DetailsOption](opts), detailsWiring(func(d *Details) {
		d.autoLinks = autoLinks
	}))...)

	reg.OnCoversChanged(covers.forgetMissing)

	return &Services{
		Registry:   reg,
		Covers:     covers,
		Details:    details,
		Assets:     assets,
		Identifier: NewIdentifier(games, reg, optionsFor[IdentifierOption](opts)...),
		Photos:     NewPhotos(games, cfg.photos, optionsFor[PhotosOption](opts)...),
		AutoLinks:  autoLinks,
		Proxy:      NewImageProxy(reg, fetch),
	}
}
