// Package app wires Game Vault together: the repositories, the application services and the HTTP
// handler that serves them. It is the composition root shared by the server (cmd/gamevault) and the
// end-to-end tests of the API, so both build the same graph and a new dependency is added once.
//
// What differs between them comes in as options: the plugins (real stores or fakes), the image
// fetcher, the password hasher's cost, the certificate policy, the disk check and whether
// notifications are sent. Starting the background jobs and listening stay with the server.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"gamevault/internal/adapters/inbound/rpc"
	"gamevault/internal/adapters/outbound/backupdir"
	"gamevault/internal/adapters/outbound/csvfile"
	"gamevault/internal/adapters/outbound/diskspace"
	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/imagefetch"
	"gamevault/internal/adapters/outbound/logfile"
	"gamevault/internal/adapters/outbound/passwordhash"
	"gamevault/internal/adapters/outbound/photostore"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/auth"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/fields"
	"gamevault/internal/application/health"
	"gamevault/internal/application/home"
	"gamevault/internal/application/logs"
	"gamevault/internal/application/media"
	"gamevault/internal/application/notify"
	"gamevault/internal/application/ownership"
	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/system"
	"gamevault/internal/application/transfer"
	"gamevault/internal/application/valuation"
	"gamevault/internal/config"
	domainauth "gamevault/internal/domain/auth"
)

// Plugins are the integrations the services run: store sources, media providers, price providers
// and notification connectors (plugin.Registry in the server, fakes in tests).
type Plugins interface {
	// Sources are the store integrations, one per source type.
	Sources() []sync.Provider

	// Providers are the cover, barcode, metadata and price providers, in default chain order.
	Providers() []providers.Impl

	// Valuations are the price providers the valuation service asks.
	Valuations() []valuation.Provider

	// Deals are the home page's deals providers.
	Deals() []home.DealsProvider

	// Releases are the home page's upcoming releases providers.
	Releases() []home.ReleasesProvider

	// Connectors are the notification connector types, in the order the UI lists them.
	Connectors() []notify.Connector
}

// App is Game Vault built: the HTTP handler, and the services the server still needs to start the
// background jobs.
type App struct {
	// Handler serves the API, the media endpoints and the web UI.
	Handler http.Handler

	// Sync scans the sources and keeps their sessions alive.
	Sync *sync.Service

	// Valuation estimates the physical copies' prices.
	Valuation *valuation.Service

	// Media are the cover, details, photo and link services.
	Media *media.Services

	// System makes the backups.
	System *system.Service

	// Home gathers the home page's deals and releases.
	Home *home.Service

	// Notify keeps the notification connections and delivers what is queued for them.
	Notify *notify.Service

	// NotifyWatcher turns Health's problems into notifications.
	NotifyWatcher *notify.Watcher

	// NotifyDaily tells the keys whose deadline comes near and the releases out today.
	NotifyDaily *notify.Daily
}

// Option configures Build.
type Option func(*options)

type options struct {
	plugins Plugins
	images  media.ImageFetcher
	hasher  auth.Hasher
	certs   auth.CertificatePolicy
	disk    health.DiskSpace
	now     port.Clock
	version string
	sending bool
}

// WithPlugins sets the integrations (none by default).
func WithPlugins(p Plugins) Option { return func(o *options) { o.plugins = p } }

// WithImageFetcher sets how images are downloaded (over HTTP by default).
func WithImageFetcher(f media.ImageFetcher) Option { return func(o *options) { o.images = f } }

// WithHasher sets the password hasher (bcrypt at its default cost by default; tests lower it).
func WithHasher(h auth.Hasher) Option { return func(o *options) { o.hasher = h } }

// WithCertificatePolicy sets what applies the certificate validation setting to outgoing
// connections. Without it the setting is saved but not applied, so certificates stay checked: the
// server passes the policy installed on the shared transport.
func WithCertificatePolicy(c auth.CertificatePolicy) Option {
	return func(o *options) { o.certs = c }
}

// WithDiskSpace sets how the health reads the free space of the config folder (the filesystem's
// by default); nil turns the disk check off.
func WithDiskSpace(d health.DiskSpace) Option { return func(o *options) { o.disk = d } }

// WithClock sets the clock every service reads (time.Now by default).
func WithClock(now port.Clock) Option { return func(o *options) { o.now = now } }

// WithVersion sets the version the status reports ("dev" by default).
func WithVersion(v string) Option { return func(o *options) { o.version = v } }

// WithSending turns on queueing notifications for the connections. It is off by default, so tests
// and a server on a copy of the config (whose connections hold real secrets) never send; only a
// connection's Test sends then.
func WithSending(on bool) Option { return func(o *options) { o.sending = on } }

// Build wires Game Vault over an open database, in the folders of cfg, logging to log and to
// logFiles. It applies the saved log and security settings (and -reset-auth) before returning, so
// background jobs started afterwards already follow them.
func Build(ctx context.Context, cfg config.Config, db *sqlite.DB, logFiles *logfile.Rotator, log *slog.Logger, opts ...Option) (*App, error) {
	o := options{
		plugins: noPlugins{},
		images:  imagefetch.New(),
		hasher:  passwordhash.New(),
		certs:   keepValidating{},
		disk:    diskspace.Statfs{},
		now:     time.Now,
		version: "dev",
	}
	for _, opt := range opts {
		opt(&o)
	}

	// One clock for every service: a scan's report, the copies the catalog dates for it and the
	// covers' and prices' dates must never disagree because two services read different clocks.
	now := o.now

	// Outbound adapters
	games := sqlite.NewGameRepository(db)
	sources := sqlite.NewSourceRepository(db)
	settingsRepo := sqlite.NewSettingsRepository(db)

	if len(cfg.TrustedNetworks) > 0 {
		trusted, err := domainauth.Settings{TrustedNetworks: cfg.TrustedNetworks}.Normalize()
		if err != nil {
			return nil, fmt.Errorf("-trusted-networks / GAMEVAULT_TRUSTED_NETWORKS: %w", err)
		}

		settingsRepo.DefaultTrustedNetworks = trusted.TrustedNetworks
	}

	assets, err := gamedata.Open(cfg.GameDataDir())
	if err != nil {
		return nil, err
	}

	photos, err := photostore.Open(cfg.PhotosDir())
	if err != nil {
		return nil, err
	}

	backupPhotos, err := photostore.Open(filepath.Join(cfg.BackupDir(), "photos"))
	if err != nil {
		return nil, err
	}

	// Application services
	logsSvc := logs.NewService(settingsRepo, logFiles, logFiles)
	if err := logsSvc.Init(ctx); err != nil {
		return nil, fmt.Errorf("applying log settings: %w", err)
	}

	// The providers' configuration is read once, on first use, and then kept in memory.
	registry := providers.NewRegistry(sqlite.NewProviderRepository(db), o.plugins.Providers(),
		providers.WithClock(now), providers.WithLogger(log))
	mediaSvcs := media.New(games, registry, assets, o.images,
		media.WithPhotoStore(photos), media.WithDetailsStore(sqlite.NewDetailsStore(db)), media.WithClock(now), media.WithLogger(log))

	// The notification service is built before the use cases that publish to it. It reads covers
	// from the game data only: rendering a message must never make a request.
	notifyStore := sqlite.NewNotifyStore(db)
	notifySvc := notify.NewService(sqlite.NewConnectionRepository(db), notifyStore, notifyStore, games, assets,
		notify.WithConnectors(o.plugins.Connectors()...), notify.WithClock(now), notify.WithLogger(log),
		notify.WithSending(o.sending))

	valuationSvc := valuation.NewService(games, db, registry, settingsRepo, now, log, o.plugins.Valuations()...)
	fieldsSvc := fields.NewService(settingsRepo, games, db, now)
	catalogSvc := catalog.NewService(games, db, catalog.WithClock(now), catalog.WithCoverCache(mediaSvcs.Covers),
		catalog.WithPhotoFiles(photos), catalog.WithFields(settingsRepo), catalog.WithEvents(notifySvc))
	ownershipSvc := ownership.NewService(games)
	homeSvc := home.NewService(registry, games, ownershipSvc, sqlite.NewHomeSnapshots(db), settingsRepo,
		home.WithDeals(o.plugins.Deals()...), home.WithReleases(o.plugins.Releases()...),
		home.WithClock(now), home.WithLogger(log), home.WithEvents(notifySvc))

	mediaSvcs.AutoLinks.SetGameLinker(catalogSvc) // automatic links are saved by the catalog, in its transactions
	syncSvc := sync.NewService(sources, games, db, catalogSvc, o.plugins.Sources(),
		sync.WithClock(now), sync.WithLogger(log), sync.WithEvents(notifySvc))
	linkStores := media.NewStores(registry, syncSvc)
	transferSvc := transfer.NewService(games, catalogSvc, settingsRepo, csvfile.Codec{}, transfer.WithClock(now),
		transfer.WithEvents(notifySvc))
	systemSvc := system.NewService(games, db, backupdir.New(cfg.BackupDir()), settingsRepo,
		system.WithPhotoArchive(photostore.NewArchive(photos, backupPhotos)), system.WithClock(now), system.WithLogger(log),
		system.WithStatus(system.Status{
			Version:      o.version,
			ConfigDir:    cfg.ConfigDir,
			DatabasePath: cfg.DatabasePath(),
			StartedAt:    now(),
		}), system.WithKeep(cfg.BackupKeep))

	healthSvc := health.NewService(registry, syncSvc, systemSvc, health.DiskCheck{
		Path:  cfg.ConfigDir,
		Space: o.disk,
	}, notifySvc)

	notifyWatcher := notify.NewWatcher(healthSvc, notifyStore, notifySvc,
		notify.WithWatcherClock(now), notify.WithWatcherLogger(log))
	notifyDaily := notify.NewDaily(games, notifyStore, homeSvc, notifySvc,
		notify.WithDailyClock(now), notify.WithDailyLogger(log))

	// The saved certificate setting must be applied before any background job starts: until then
	// requests would ignore it.
	authSvc := auth.NewService(sqlite.NewAuthRepository(db), settingsRepo, o.hasher, o.certs, now, log,
		auth.WithEvents(notifySvc))
	if cfg.ResetAuth {
		if err := authSvc.ResetAuthentication(ctx); err != nil {
			return nil, fmt.Errorf("resetting authentication: %w", err)
		}
	}

	if err := authSvc.Init(ctx); err != nil {
		return nil, fmt.Errorf("applying security settings: %w", err)
	}

	// Inbound adapter
	handler := rpc.NewHTTPHandler(rpc.Handlers{
		Auth:        rpc.NewAuthHandler(authSvc),
		AuthService: authSvc,
		Games:       rpc.NewGameHandler(catalogSvc, mediaSvcs.Details, linkStores, syncSvc),
		Sources:     rpc.NewSourceHandler(syncSvc),
		System:      rpc.NewSystemHandler(systemSvc, transferSvc),
		Logs:        rpc.NewLogHandler(logsSvc),
		MediaRPC:    rpc.NewMediaHandler(registry, mediaSvcs.Covers, mediaSvcs.Details, mediaSvcs.Identifier),
		Covers:      mediaSvcs.Covers,
		Assets:      mediaSvcs.Assets,
		Photos:      mediaSvcs.Photos,
		Proxy:       mediaSvcs.Proxy,
		Valuation:   rpc.NewValuationHandler(valuationSvc),
		Fields:      rpc.NewFieldHandler(fieldsSvc),
		Health:      rpc.NewHealthHandler(healthSvc),
		Ownership:   rpc.NewOwnershipHandler(ownershipSvc),
		Home:        rpc.NewHomeHandler(homeSvc),
	}, rpc.Options{
		UIDir:       cfg.UIDir,
		CORSOrigins: cfg.CORSOrigins,
		Log:         log,
	})

	return &App{
		Handler:       handler,
		Sync:          syncSvc,
		Valuation:     valuationSvc,
		Media:         mediaSvcs,
		System:        systemSvc,
		Home:          homeSvc,
		Notify:        notifySvc,
		NotifyWatcher: notifyWatcher,
		NotifyDaily:   notifyDaily,
	}, nil
}

// noPlugins is the default: no integrations at all.
type noPlugins struct{}

func (noPlugins) Sources() []sync.Provider { return nil }

func (noPlugins) Providers() []providers.Impl { return nil }

func (noPlugins) Valuations() []valuation.Provider { return nil }

func (noPlugins) Deals() []home.DealsProvider { return nil }

func (noPlugins) Releases() []home.ReleasesProvider { return nil }

func (noPlugins) Connectors() []notify.Connector { return nil }

// keepValidating is the default certificate policy: it applies nothing, so Go's default (every
// certificate checked) stays.
type keepValidating struct{}

func (keepValidating) SetCertificateValidation(domainauth.CertificateValidation) {}
