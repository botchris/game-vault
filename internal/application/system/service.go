// Package system implements status and backup use cases.
package system

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/settings"
)

// DatabaseBackup is the port that writes a consistent snapshot of the database to a file.
type DatabaseBackup interface {
	// BackupTo writes a consistent copy of the database to path.
	BackupTo(ctx context.Context, path string) error
}

// Status describes the running instance.
type Status struct {
	Version      string
	ConfigDir    string
	DatabasePath string
	StartedAt    time.Time
	GameCount    int
	CopyCount    int
}

// Backup is a backup file in the backups directory.
type Backup struct {
	Name      string
	SizeBytes int64
	CreatedAt time.Time
}

// Service exposes the system use cases.
type Service struct {
	games     game.Repository
	db        DatabaseBackup
	prefs     settings.Repository
	now       port.Clock
	log       *slog.Logger
	status    Status
	backupDir string
	keep      int
}

// NewService builds the service. Backups are written to backupDir and only the newest keep are kept.
func NewService(games game.Repository, db DatabaseBackup, prefs settings.Repository, now port.Clock, log *slog.Logger,
	status Status, backupDir string, keep int) *Service {
	return &Service{
		games:     games,
		db:        db,
		prefs:     prefs,
		now:       now,
		log:       log,
		status:    status,
		backupDir: backupDir,
		keep:      keep,
	}
}

// Status reports the running version and the size of the catalog.
func (s *Service) Status(ctx context.Context) (Status, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return Status{}, err
	}

	st := s.status

	st.GameCount = len(games)
	for _, g := range games {
		st.CopyCount += len(g.Copies())
	}

	return st, nil
}

// CreateBackup snapshots the database into the backups directory and prunes old backups.
func (s *Service) CreateBackup(ctx context.Context) (Backup, error) {
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return Backup{}, err
	}

	name := fmt.Sprintf("gamevault-%s.db", s.now().UTC().Format("20060102-150405"))

	path := filepath.Join(s.backupDir, name)
	if err := s.db.BackupTo(ctx, path); err != nil {
		return Backup{}, err
	}

	if err := s.prune(); err != nil {
		s.log.Warn("pruning backups", "error", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}

	return Backup{
		Name:      name,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime(),
	}, nil
}

// ListBackups returns the backups, newest first.
func (s *Service) ListBackups(context.Context) ([]Backup, error) {
	entries, err := os.ReadDir(s.backupDir)
	if os.IsNotExist(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var out []Backup

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		out = append(out, Backup{
			Name:      e.Name(),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	// By date, not by name: a pre-migration copy (pre-migration-0009.db) would otherwise always sort
	// above the scheduled ones (gamevault-…), stay at the top and never be pruned.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}

		return out[i].Name > out[j].Name
	})

	return out, nil
}

func (s *Service) prune() error {
	backups, err := s.ListBackups(context.Background())
	if err != nil || len(backups) <= s.keep {
		return err
	}

	for _, b := range backups[s.keep:] {
		if err := os.Remove(filepath.Join(s.backupDir, b.Name)); err != nil {
			return err
		}
	}

	return nil
}

// RunScheduledBackups creates a backup every interval until ctx ends.
func (s *Service) RunScheduledBackups(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if b, err := s.CreateBackup(ctx); err != nil {
				s.log.Error("scheduled backup failed", "error", err)
			} else {
				s.log.Info("scheduled backup created", "name", b.Name)
			}
		}
	}
}

// Preferences returns the user's preferences.
func (s *Service) Preferences(ctx context.Context) (settings.Preferences, error) {
	return s.prefs.Preferences(ctx)
}

// UpdatePreferences validates and stores the user's preferences.
func (s *Service) UpdatePreferences(ctx context.Context, p settings.Preferences) (settings.Preferences, error) {
	p, err := p.Validate()
	if err != nil {
		return p, err
	}

	return p, s.prefs.SavePreferences(ctx, p)
}
