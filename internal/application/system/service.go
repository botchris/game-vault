// Package system implements status and backup use cases.
package system

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

// PhotoArchive is the port that keeps the backups' photos in one shared store, each photo once,
// however many backups list it.
type PhotoArchive interface {
	// Add puts the photos into the backups' store, skipping those already there.
	Add(ids []game.PhotoID) error

	// Retain deletes from the backups' store every photo not in keep.
	Retain(keep map[game.PhotoID]bool) error

	// Size returns the total size of the backups' store in bytes.
	Size() (int64, error)
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

	// Photos is how many photos the backup's list names; 0 for backups without a list (older ones
	// and pre-migration copies).
	Photos int
}

// Service exposes the system use cases.
type Service struct {
	games     game.Repository
	db        DatabaseBackup
	prefs     settings.Repository
	photos    PhotoArchive
	now       port.Clock
	log       *slog.Logger
	status    Status
	backupDir string
	keep      int
}

// NewService builds the service. Backups are written to backupDir and only the newest keep are kept.
// photos may be nil: backups then hold only the database.
func NewService(games game.Repository, db DatabaseBackup, prefs settings.Repository, photos PhotoArchive, now port.Clock, log *slog.Logger,
	status Status, backupDir string, keep int) *Service {
	return &Service{
		games:     games,
		db:        db,
		prefs:     prefs,
		photos:    photos,
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

	// The photos are read before and after the copy: the copied database references photos from
	// some moment in between, and the union holds all of them (a few extra are harmless).
	before, err := s.referencedPhotos(ctx)
	if err != nil {
		return Backup{}, err
	}

	path := filepath.Join(s.backupDir, name)
	if err := s.db.BackupTo(ctx, path); err != nil {
		return Backup{}, err
	}

	photos, photoErr := s.backupPhotos(ctx, path, before)

	// The database copy is a backup even when its photos failed, so rotation still runs.
	if err := s.prune(); err != nil {
		s.log.Warn("pruning backups", "error", err)
	}

	if photoErr != nil {
		return Backup{}, fmt.Errorf("backing up photos: %w", photoErr)
	}

	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}

	return Backup{
		Name:      name,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime(),
		Photos:    photos,
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
			Photos:    len(readPhotoList(filepath.Join(s.backupDir, e.Name()))),
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
		path := filepath.Join(s.backupDir, b.Name)
		if err := os.Remove(path); err != nil {
			return err
		}

		if err := os.Remove(photoList(path)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if s.photos == nil {
		return nil
	}

	keep := map[game.PhotoID]bool{}

	for _, b := range backups[:s.keep] {
		for _, id := range readPhotoList(filepath.Join(s.backupDir, b.Name)) {
			keep[id] = true
		}
	}

	return s.photos.Retain(keep)
}

// PhotoStoreSize returns the size of the backups' shared photo store in bytes.
func (s *Service) PhotoStoreSize(context.Context) (int64, error) {
	if s.photos == nil {
		return 0, nil
	}

	return s.photos.Size()
}

// photoList is the file next to a backup that names the photos it needs.
func photoList(dbPath string) string { return strings.TrimSuffix(dbPath, ".db") + ".photos" }

// backupPhotos writes the backup's photo list (the photos referenced before the copy, plus those
// referenced now) and puts them in the shared store.
func (s *Service) backupPhotos(ctx context.Context, dbPath string, before map[game.PhotoID]bool) (int, error) {
	if s.photos == nil {
		return 0, nil
	}

	after, err := s.referencedPhotos(ctx)
	if err != nil {
		return 0, err
	}

	ids := make([]game.PhotoID, 0, len(before)+len(after))
	for id := range before {
		ids = append(ids, id)
	}

	for id := range after {
		if !before[id] {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)

	var list strings.Builder
	for _, id := range ids {
		list.WriteString(string(id) + "\n")
	}

	if err := os.WriteFile(photoList(dbPath), []byte(list.String()), 0o600); err != nil {
		return 0, err
	}

	return len(ids), s.photos.Add(ids)
}

// referencedPhotos returns the photos the catalog references; none when backups keep no photos.
func (s *Service) referencedPhotos(ctx context.Context) (map[game.PhotoID]bool, error) {
	ids := map[game.PhotoID]bool{}
	if s.photos == nil {
		return ids, nil
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, g := range games {
		for _, id := range g.PhotoIDs() {
			ids[id] = true
		}
	}

	return ids, nil
}

// readPhotoList returns the photos a backup's list names; none when it has no list.
func readPhotoList(dbPath string) []game.PhotoID {
	data, err := os.ReadFile(photoList(dbPath))
	if err != nil {
		return nil
	}

	var ids []game.PhotoID

	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, game.PhotoID(line))
		}
	}

	return ids
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
