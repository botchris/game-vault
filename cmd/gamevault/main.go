// Command gamevault runs the Game Vault server: a ConnectRPC API over a SQLite catalog of your
// games and every copy you own of them (keys, store libraries, physical discs).
//
// The wiring itself is internal/app, shared with the API's end-to-end tests; this file loads the
// configuration, opens the database and the log, picks the real plugins, starts the background
// jobs and serves.
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

	"gamevault/internal/adapters/outbound/logfile"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/adapters/outbound/tlspolicy"
	"gamevault/internal/app"
	"gamevault/internal/config"
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

	plugins, err := newPlugins(log)
	if err != nil {
		return err
	}

	// Certificate validation applies to every outgoing HTTPS request (they all use the default
	// transport). It must be installed, and the saved setting applied by Build, before any
	// background job starts: Install writes to the shared transport, which would race with a job
	// already using it, and until the setting is applied requests would ignore it.
	certs := tlspolicy.Install(http.DefaultTransport.(*http.Transport))

	vault, err := app.Build(ctx, cfg, db, logFiles, log,
		app.WithPlugins(plugins), app.WithCertificatePolicy(certs), app.WithVersion(version))
	if err != nil {
		return err
	}

	// Background jobs (only after Build applied the TLS policy and the security settings).
	// Every job that makes requests nobody asked for at that moment is off with -no-unattended. The
	// auto-linker stays on: without the details scanner it only searches games a person opened.
	if cfg.NoUnattended {
		log.Warn("scheduled scans, keep-alives, price estimates, the background details scan and the home page's deals and releases are off (-no-unattended)")
	} else {
		go vault.Sync.RunScheduler(ctx, time.Minute, 30*time.Minute)
		go vault.Sync.RunKeepAlive(ctx, time.Minute, 10*time.Minute)
		go vault.Valuation.RunScheduler(ctx)
		go vault.Media.Details.RunScanner(ctx, 2*time.Second) // 2-4 s apart: gentle enough for the strictest store API (Steam: ~200 requests / 5 min)
		go vault.Home.Run(ctx)                                // deals every 6 h, releases every 24 h, ±10 %
	}

	go vault.Media.AutoLinks.Run(ctx, 2*time.Second) // one game at a time, jittered, after cover downloads
	go vault.Media.Photos.RunCleanup(ctx, time.Hour, 24*time.Hour)

	if cfg.BackupInterval > 0 {
		go vault.System.RunScheduledBackups(ctx, cfg.BackupInterval)
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s (is another Game Vault already running? use -addr to pick another port): %w", cfg.Addr, err)
	}

	srv := &http.Server{
		Handler:           vault.Handler,
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
