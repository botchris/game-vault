package media

import (
	"log/slog"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
)

// CoverOption configures NewCovers.
type CoverOption interface {
	// applyCovers applies the option to the service being built.
	applyCovers(c *Covers)
}

// DetailsOption configures NewDetails.
type DetailsOption interface {
	// applyDetails applies the option to the service being built.
	applyDetails(d *Details)
}

// AssetsOption configures NewAssets.
type AssetsOption interface {
	// applyAssets applies the option to the service being built.
	applyAssets(a *Assets)
}

// IdentifierOption configures NewIdentifier.
type IdentifierOption interface {
	// applyIdentifier applies the option to the service being built.
	applyIdentifier(i *Identifier)
}

// PhotosOption configures NewPhotos.
type PhotosOption interface {
	// applyPhotos applies the option to the service being built.
	applyPhotos(p *Photos)
}

// AutoLinkerOption configures NewAutoLinker.
type AutoLinkerOption interface {
	// applyAutoLinker applies the option to the service being built.
	applyAutoLinker(a *AutoLinker)
}

// Option configures New: it is any option of the services, handed to each service it applies to.
type Option interface {
	// applyNew records what New itself reads from the option, if anything.
	applyNew(c *config)
}

// config is what New itself reads from its options: the ports it passes as required arguments.
type config struct {
	photos  PhotoStore
	details DetailsStore
}

// ClockOption sets the clock of the services that date things (time.Now by default).
type ClockOption struct{ now port.Clock }

// WithClock sets the clock (time.Now by default).
func WithClock(now port.Clock) ClockOption { return ClockOption{now} }

func (o ClockOption) applyCovers(c *Covers)         { c.now = o.now }
func (o ClockOption) applyDetails(d *Details)       { d.now = o.now }
func (o ClockOption) applyPhotos(p *Photos)         { p.now = o.now }
func (o ClockOption) applyAutoLinker(a *AutoLinker) { a.now = o.now }
func (ClockOption) applyNew(*config)                {}

// LoggerOption sets the logger of every service (discarded by default).
type LoggerOption struct{ log *slog.Logger }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) LoggerOption { return LoggerOption{log} }

func (o LoggerOption) applyCovers(c *Covers)         { c.log = o.log }
func (o LoggerOption) applyDetails(d *Details)       { d.log = o.log }
func (o LoggerOption) applyAssets(a *Assets)         { a.log = o.log }
func (o LoggerOption) applyIdentifier(i *Identifier) { i.log = o.log }
func (o LoggerOption) applyPhotos(p *Photos)         { p.log = o.log }
func (o LoggerOption) applyAutoLinker(a *AutoLinker) { a.log = o.log }
func (LoggerOption) applyNew(*config)                {}

// PhotoStoreOption sets the store of the users' copy photos: a chosen cover may be one, and New
// builds the Photos service on it (none by default).
type PhotoStoreOption struct{ store PhotoStore }

// WithPhotoStore sets the store of the users' copy photos (none by default).
func WithPhotoStore(store PhotoStore) PhotoStoreOption { return PhotoStoreOption{store} }

func (o PhotoStoreOption) applyCovers(c *Covers) { c.photos = o.store }
func (o PhotoStoreOption) applyNew(c *config)    { c.photos = o.store }

// DetailsStoreOption sets the cache of game sheets: dropped with a game's other media (Covers) or
// when links are found (AutoLinker), and the one New builds the Details service on (none by default:
// no sheet is cached).
type DetailsStoreOption struct{ store DetailsStore }

// WithDetailsStore sets the cache of game sheets (none by default).
func WithDetailsStore(store DetailsStore) DetailsStoreOption { return DetailsStoreOption{store} }

func (o DetailsStoreOption) applyCovers(c *Covers)         { c.details = o.store }
func (o DetailsStoreOption) applyAutoLinker(a *AutoLinker) { a.details = o.store }
func (o DetailsStoreOption) applyNew(c *config)            { c.details = o.store }

// RhythmOption sets the random source and the sleep of the background work (the details scan, the
// auto-linker), so tests control its timing (real randomness and sleeps by default).
type RhythmOption struct{ rhythm unattended.Rhythm }

// WithRhythm sets the background work's random source and sleep.
func WithRhythm(r unattended.Rhythm) RhythmOption { return RhythmOption{r} }

func (o RhythmOption) applyDetails(d *Details)       { d.rhythm = o.rhythm }
func (o RhythmOption) applyAutoLinker(a *AutoLinker) { a.rhythm = o.rhythm }
func (RhythmOption) applyNew(*config)                {}

// The services each shared option applies to, checked by the compiler.
var (
	_ interface {
		CoverOption
		DetailsOption
		PhotosOption
		AutoLinkerOption
		Option
	} = ClockOption{}
	_ interface {
		CoverOption
		DetailsOption
		AssetsOption
		IdentifierOption
		PhotosOption
		AutoLinkerOption
		Option
	} = LoggerOption{}
)

// The wiring New does between the services, which no caller sets.
type (
	coverWiring      func(c *Covers)
	detailsWiring    func(d *Details)
	autoLinkerWiring func(a *AutoLinker)
)

func (f coverWiring) applyCovers(c *Covers)              { f(c) }
func (f detailsWiring) applyDetails(d *Details)          { f(d) }
func (f autoLinkerWiring) applyAutoLinker(a *AutoLinker) { f(a) }

// optionsFor returns the options of opts that apply to a service whose option type is T, in order.
func optionsFor[T any](opts []Option) []T {
	out := make([]T, 0, len(opts))

	for _, o := range opts {
		if t, ok := o.(T); ok {
			out = append(out, t)
		}
	}

	return out
}

// Defaults every service starts from, before its options.
var (
	defaultClock  port.Clock = time.Now
	defaultLogger            = slog.New(slog.DiscardHandler)
)
