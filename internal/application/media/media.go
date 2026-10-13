// Package media implements what Game Vault shows about a game besides the user's own records: its
// covers, its sheet (summary, screenshots…) and their images, the identification of a scanned
// barcode or a typed title, the photos users take of their copies, and the store links found
// automatically.
//
// Each part is its own service; New builds them and wires them to each other. They all run the
// providers configured in the providers registry (internal/application/providers): covers are
// resolved through an ordered chain of cover providers (Plex-agent style) and cached in the config
// directory, so the UI never hot-links third-party sites and quotas are spent once.
package media

import (
	"log/slog"
	"slices"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/game"
)

// defaultLanguage is the language sheets are gathered in when the UI names none: English is the
// fallback of every language-dependent feature.
const defaultLanguage = "en"

// Option configures a media service. The options are shared by every service of the package; each
// service reads the ones it uses and ignores the others.
type Option func(*options)

// options are the optional dependencies and settings of the services.
type options struct {
	now       port.Clock
	log       *slog.Logger
	photos    PhotoStore
	details   DetailsStore
	downloads *Downloads
	autoLinks linkQueue
	rhythm    unattended.Rhythm
	misses    *misses
}

// WithClock sets the clock (time.Now by default).
func WithClock(now port.Clock) Option { return func(o *options) { o.now = now } }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) Option { return func(o *options) { o.log = log } }

// WithPhotos sets the store of the users' copy photos, which a chosen cover may be (none by default).
func WithPhotos(photos PhotoStore) Option { return func(o *options) { o.photos = photos } }

// WithDetailsStore sets the cache of game sheets, dropped with a game's other media (none by
// default: no sheet is cached).
func WithDetailsStore(details DetailsStore) Option { return func(o *options) { o.details = details } }

// WithDownloads sets the image downloads the auto-linker yields to (none by default).
func WithDownloads(d *Downloads) Option { return func(o *options) { o.downloads = d } }

// WithAutoLinks sets the auto-linker that viewed games are queued for (none by default: no links
// are looked for).
func WithAutoLinks(a *AutoLinker) Option { return func(o *options) { o.autoLinks = a } }

// WithRhythm sets the random source and the sleep of the background work (the details scan, the
// auto-linker), so tests control its timing (real randomness and sleeps by default).
func WithRhythm(r unattended.Rhythm) Option { return func(o *options) { o.rhythm = r } }

// withMisses shares the covers' "no cover" answers between the covers and the auto-linker, whose
// links found make them stale.
func withMisses(m *misses) Option { return func(o *options) { o.misses = m } }

// newOptions applies opts over the defaults: no-op ports, the real clock, a silent logger.
func newOptions(opts []Option) options {
	o := options{
		now:       time.Now,
		log:       slog.New(slog.DiscardHandler),
		photos:    noPhotos{},
		details:   noDetails{},
		autoLinks: noAutoLinks{},
		misses:    &misses{},
	}
	for _, opt := range opts {
		opt(&o)
	}

	if o.downloads == nil {
		o.downloads = NewDownloads(nil)
	}

	return o
}

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

// New builds the media services over the providers in reg. games, store and fetch are required;
// the options give the optional ports (photos, sheet cache) and the clock and logger, shared by
// every service.
func New(games game.Repository, reg *providers.Registry, store AssetStore, fetch ImageFetcher, opts ...Option) *Services {
	o := newOptions(opts)
	downloads := NewDownloads(fetch)

	// Links found for a game give it the stores' art: the auto-linker makes the covers forget what
	// they missed for it.
	missed := &misses{}

	autoLinks := NewAutoLinker(games, reg, slices.Concat(opts, []Option{WithDownloads(downloads), withMisses(missed)})...)
	assets := NewAssets(games, store, downloads, opts...)
	shared := slices.Concat(opts, []Option{WithAutoLinks(autoLinks)})
	covers := NewCovers(games, reg, store, downloads, slices.Concat(shared, []Option{withMisses(missed)})...)

	reg.OnCoversChanged(covers.forgetMissing)

	return &Services{
		Registry:   reg,
		Covers:     covers,
		Details:    NewDetails(games, reg, o.details, assets, shared...),
		Assets:     assets,
		Identifier: NewIdentifier(games, reg, opts...),
		Photos:     NewPhotos(games, o.photos, opts...),
		AutoLinks:  autoLinks,
		Proxy:      NewImageProxy(reg, fetch),
	}
}
