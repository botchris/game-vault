// Package config loads runtime configuration from flags and environment variables.
//
// Everything that changes at runtime (database, backups, logs, game images) lives in ConfigDir,
// so backing up that single directory is enough.
package config

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the server configuration.
type Config struct {
	Addr           string
	ConfigDir      string
	UIDir          string
	CORSOrigins    []string
	BackupInterval time.Duration
	BackupKeep     int

	// TrustedNetworks replace "this computer" as the trusted networks until the security
	// settings are saved from the UI. The Docker image sets them to the private networks, because
	// inside a container requests never come from 127.0.0.1.
	TrustedNetworks []string

	// ResetAuth makes authentication not required on trusted networks again, for when the
	// password is forgotten while it is required.
	ResetAuth bool

	// NoUnattended turns off scheduled scans and session keep-alives. A test server on a copy of
	// the config needs it: renewing a credential that rotates (Ubisoft, Epic, GOG…) in the copy
	// invalidates the one the real server holds.
	NoUnattended bool
}

// DatabasePath is the SQLite file inside ConfigDir.
func (c Config) DatabasePath() string { return filepath.Join(c.ConfigDir, "gamevault.db") }

// BackupDir is where backups are written.
func (c Config) BackupDir() string { return filepath.Join(c.ConfigDir, "backups") }

// LogDir holds the rotated log files.
func (c Config) LogDir() string { return filepath.Join(c.ConfigDir, "logs") }

// GameDataDir holds one folder per game with its cover and sheet images.
func (c Config) GameDataDir() string { return filepath.Join(c.ConfigDir, "game-data") }

// LegacyCoverDir is where versions before game-data kept covers; migrated on start.
func (c Config) LegacyCoverDir() string { return filepath.Join(c.ConfigDir, "covers") }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}

	return def
}

// Load parses flags (args, usually os.Args[1:]). Every flag can also be set through a
// GAMEVAULT_* environment variable; flags win.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("gamevault", flag.ContinueOnError)

	var (
		c       Config
		cors    string
		trusted string
	)

	keep, _ := strconv.Atoi(env("GAMEVAULT_BACKUP_KEEP", "14"))

	interval, err := time.ParseDuration(env("GAMEVAULT_BACKUP_INTERVAL", "24h"))
	if err != nil {
		return c, err
	}

	fs.StringVar(&c.Addr, "addr", env("GAMEVAULT_ADDR", "127.0.0.1:8080"), "listen address (env GAMEVAULT_ADDR)")
	fs.StringVar(&c.ConfigDir, "config-dir", env("GAMEVAULT_CONFIG_DIR", "config"), "data directory: database and backups (env GAMEVAULT_CONFIG_DIR)")
	fs.StringVar(&c.UIDir, "ui-dir", env("GAMEVAULT_UI_DIR", "web/dist"), "built web UI to serve; empty to disable (env GAMEVAULT_UI_DIR)")
	fs.StringVar(&cors, "cors-origins", env("GAMEVAULT_CORS_ORIGINS", ""), "comma-separated origins allowed to call the API (env GAMEVAULT_CORS_ORIGINS)")
	fs.DurationVar(&c.BackupInterval, "backup-interval", interval, "automatic backup interval, 0 to disable (env GAMEVAULT_BACKUP_INTERVAL)")
	fs.IntVar(&c.BackupKeep, "backup-keep", keep, "number of backups to keep (env GAMEVAULT_BACKUP_KEEP)")
	fs.StringVar(&trusted, "trusted-networks", env("GAMEVAULT_TRUSTED_NETWORKS", ""), "comma-separated networks trusted until the security settings are saved; default this computer (env GAMEVAULT_TRUSTED_NETWORKS)")
	fs.BoolVar(&c.NoUnattended, "no-unattended", false, "no scheduled scans or session keep-alives (for a test server on a copy of the config)")
	fs.BoolVar(&c.ResetAuth, "reset-auth", false, "on start, stop requiring sign-in from trusted networks (forgotten password)")

	if err := fs.Parse(args); err != nil {
		return c, err
	}

	c.CORSOrigins = splitList(cors)
	c.TrustedNetworks = splitList(trusted)

	if c.UIDir != "" {
		if info, err := os.Stat(c.UIDir); err != nil || !info.IsDir() {
			c.UIDir = "" // UI not built: run the API only
		}
	}

	c.ConfigDir, err = filepath.Abs(c.ConfigDir)

	return c, err
}

// splitList parses a comma-separated flag, ignoring blanks.
func splitList(s string) []string {
	var out []string

	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}

	return out
}
