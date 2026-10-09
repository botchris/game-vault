// Command gamevault runs the Game Vault server: a ConnectRPC API over a SQLite catalog of your
// games and every copy you own of them (keys, store libraries, physical discs).
//
// This file is the composition root: it is the only place that knows every concrete adapter.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gamevault/internal/adapters/inbound/rpc"
	"gamevault/internal/adapters/outbound/csvfile"
	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/imagefetch"
	"gamevault/internal/adapters/outbound/logfile"
	"gamevault/internal/adapters/outbound/passwordhash"
	"gamevault/internal/adapters/outbound/photostore"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/adapters/outbound/tlspolicy"
	"gamevault/internal/application/auth"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/logs"
	"gamevault/internal/application/media"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/system"
	"gamevault/internal/application/transfer"
	"gamevault/internal/config"
	domainauth "gamevault/internal/domain/auth"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/settings"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil && !errors.Is(err, flag.ErrHelp) {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		return err
	}

	// Logging: terminal + rotated files in config/logs. The level is a LevelVar so it can be
	// changed live from the UI; saved settings are applied once the database is open.
	level := new(slog.LevelVar)

	logFiles, err := logfile.Open(cfg.LogDir(), level, settings.DefaultLogging())
	if err != nil {
		return err
	}
	// Closing flushes the log; if that fails there is nowhere left to report it.
	defer func() { _ = logFiles.Close() }()

	log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFiles), &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sqlite.Open(ctx, cfg.DatabasePath(), cfg.BackupDir())
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("closing the database", "error", err)
		}
	}()

	// Outbound adapters
	games := sqlite.NewGameRepository(db)
	sources := sqlite.NewSourceRepository(db)
	settingsRepo := sqlite.NewSettingsRepository(db)

	if len(cfg.TrustedNetworks) > 0 {
		trusted, err := domainauth.Settings{TrustedNetworks: cfg.TrustedNetworks}.Normalize()
		if err != nil {
			return fmt.Errorf("-trusted-networks / GAMEVAULT_TRUSTED_NETWORKS: %w", err)
		}

		settingsRepo.DefaultTrustedNetworks = trusted.TrustedNetworks
	}

	assets, err := gamedata.Open(cfg.GameDataDir())
	if err != nil {
		return err
	}

	photos, err := photostore.Open(cfg.PhotosDir())
	if err != nil {
		return err
	}

	if err := migrateLegacyCovers(ctx, assets, games, cfg.LegacyCoverDir(), log); err != nil {
		return fmt.Errorf("migrating covers: %w", err)
	}

	plugins, err := newPlugins(log)
	if err != nil {
		return err
	}

	now := time.Now

	// Application services
	logsSvc := logs.NewService(settingsRepo, logFiles, logFiles)
	if err := logsSvc.Init(ctx); err != nil {
		return fmt.Errorf("applying log settings: %w", err)
	}

	mediaSvc := media.NewService(games, sqlite.NewProviderRepository(db), assets, photos, sqlite.NewDetailsStore(db), imagefetch.New(), now, log,
		plugins.Media())
	catalogSvc := catalog.NewService(games, db, now, mediaSvc)
	syncSvc := sync.NewService(sources, games, db, now, log, plugins.Sources()...)
	transferSvc := transfer.NewService(games, db, settingsRepo, now, csvfile.Codec{})
	systemSvc := system.NewService(games, db, settingsRepo, now, log, system.Status{
		Version:      version,
		ConfigDir:    cfg.ConfigDir,
		DatabasePath: cfg.DatabasePath(),
		StartedAt:    now(),
	}, cfg.BackupDir(), cfg.BackupKeep)

	// Background jobs
	if cfg.NoUnattended {
		log.Warn("scheduled scans and keep-alives are off (-no-unattended)")
	} else {
		go syncSvc.RunScheduler(ctx, time.Minute, 30*time.Minute)
		go syncSvc.RunKeepAlive(ctx, time.Minute, 10*time.Minute)
	}

	go mediaSvc.RunDetailsScanner(ctx, 2*time.Second) // gentle enough for the strictest store API (Steam: ~200 requests / 5 min)
	go mediaSvc.RunPhotoCleanup(ctx, time.Hour, 24*time.Hour)

	if cfg.BackupInterval > 0 {
		go systemSvc.RunScheduledBackups(ctx, cfg.BackupInterval)
	}

	// Certificate validation applies to every outgoing HTTPS request (they all use the default transport).
	certs := tlspolicy.Install(http.DefaultTransport.(*http.Transport))

	authSvc := auth.NewService(sqlite.NewAuthRepository(db), settingsRepo, passwordhash.New(), certs, now, log)
	if cfg.ResetAuth {
		if err := authSvc.ResetAuthentication(ctx); err != nil {
			return fmt.Errorf("resetting authentication: %w", err)
		}
	}

	if err := authSvc.Init(ctx); err != nil {
		return fmt.Errorf("applying security settings: %w", err)
	}

	// Inbound adapter
	handler := rpc.NewHTTPHandler(rpc.Handlers{
		Auth:        rpc.NewAuthHandler(authSvc),
		AuthService: authSvc,
		Games:       rpc.NewGameHandler(catalogSvc, mediaSvc, syncSvc),
		Sources:     rpc.NewSourceHandler(syncSvc),
		System:      rpc.NewSystemHandler(systemSvc, transferSvc),
		Logs:        rpc.NewLogHandler(logsSvc),
		MediaRPC:    rpc.NewMediaHandler(mediaSvc),
		Media:       mediaSvc,
	}, rpc.Options{
		UIDir:       cfg.UIDir,
		CORSOrigins: cfg.CORSOrigins,
		Log:         log,
	})

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s (is another Game Vault already running? use -addr to pick another port): %w", cfg.Addr, err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdown); err != nil {
			log.Warn("requests still running at shutdown were cut off", "error", err)
		}
	}()

	log.Info("game vault started", "version", version, "addr", "http://"+cfg.Addr, "config_dir", cfg.ConfigDir, "ui", cfg.UIDir != "")

	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	log.Info("game vault stopped")

	return nil
}

// migrateLegacyCovers moves covers from config/covers (flat, by id) into config/game-data (one
// folder per game). It is a no-op once the old directory is gone.
func migrateLegacyCovers(ctx context.Context, assets *gamedata.Store, games *sqlite.GameRepository, legacyDir string, log *slog.Logger) error {
	if _, err := os.Stat(legacyDir); os.IsNotExist(err) {
		return nil
	}

	list, err := games.List(ctx)
	if err != nil {
		return err
	}

	titles := make(map[game.ID]string, len(list))
	for _, g := range list {
		titles[g.ID()] = g.Title()
	}

	n, err := assets.MigrateLegacyCovers(legacyDir, titles)
	if n > 0 {
		log.Info("covers moved to game-data", "count", n)
	}

	return err
}
