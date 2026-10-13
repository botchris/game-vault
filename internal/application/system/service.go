// Package system implements status and backup use cases.
package system

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
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

// BackupStore is the port that keeps the backups: each one a database copy under the name the
// service gives it, with the list of the photos it needs. How they are stored (files, their
// layout, their dates) is the adapter's business; what to back up and what to keep is the
// service's.
type BackupStore interface {
	// Create prepares a new backup called name and returns the path the database copy is to be
	// written to (see DatabaseBackup).
	Create(name string) (path string, err error)

	// Stat returns the backup called name, as List describes it.
	Stat(name string) (Backup, error)

	// List returns every backup, in no particular order, each with how many photos its list
	// names; none when there are no backups yet.
	List() ([]Backup, error)

	// Remove deletes a backup and its photo list.
	Remove(name string) error

	// WritePhotoList records the photos a backup needs.
	WritePhotoList(name string, ids []game.PhotoID) error

	// PhotoList returns the photos a backup needs; none when it has no list (backups made before
	// backups kept photos, and pre-migration copies).
	PhotoList(name string) ([]game.PhotoID, error)
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

// Backup is a backup of the database.
type Backup struct {
	Name      string
	SizeBytes int64
	CreatedAt time.Time

	// Photos is how many photos the backup's list names; 0 for backups without a list (older ones
	// and pre-migration copies).
	Photos int
}

// DefaultKeep is how many backups are kept when NewService is not told (WithKeep): two weeks of
// daily backups, the command line's default too.
const DefaultKeep = 14

// Service exposes the system use cases.
type Service struct {
	games   game.Repository
	db      DatabaseBackup
	backups BackupStore
	prefs   settings.Repository
	photos  PhotoArchive
	now     port.Clock
	log     *slog.Logger
	status  Status
	keep    int

	// mu guards the automatic backups' state below, read by the health check.
	mu sync.Mutex

	// backupEvery is the automatic backups' interval; 0 when they are off.
	backupEvery time.Duration

	// lastBackupErr is the last automatic backup's error, nil once one succeeds.
	lastBackupErr error

	// lastBackupErrAt is when lastBackupErr happened.
	lastBackupErrAt time.Time
}

// Option configures NewService.
type Option func(*Service)

// WithPhotoArchive sets the shared store the backups' photos go to. Without one, backups hold only
// the database.
func WithPhotoArchive(photos PhotoArchive) Option { return func(s *Service) { s.photos = photos } }

// WithClock sets the clock that names the backups and dates their failures (time.Now by default).
func WithClock(now port.Clock) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger of the scheduled backups (none by default).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

// WithStatus sets what Status reports about the running instance besides the catalog's size.
func WithStatus(status Status) Option { return func(s *Service) { s.status = status } }

// WithKeep sets how many backups are kept, the newest ones (DefaultKeep by default).
func WithKeep(keep int) Option { return func(s *Service) { s.keep = keep } }

// NewService builds the service: db copies the database into the backups that backups keeps.
func NewService(games game.Repository, db DatabaseBackup, backups BackupStore, prefs settings.Repository, opts ...Option) *Service {
	s := &Service{
		games:   games,
		db:      db,
		backups: backups,
		prefs:   prefs,
		now:     time.Now,
		log:     slog.New(slog.DiscardHandler),
		keep:    DefaultKeep,
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
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

// CreateBackup snapshots the database into a new backup and prunes old backups. Its result is
// remembered for the health check.
func (s *Service) CreateBackup(ctx context.Context) (b Backup, err error) {
	defer func() { s.noteBackup(err) }()

	name := fmt.Sprintf("gamevault-%s.db", s.now().UTC().Format("20060102-150405"))

	path, err := s.backups.Create(name)
	if err != nil {
		return Backup{}, err
	}

	// The photos are read before and after the copy: the copied database references photos from
	// some moment in between, and the union holds all of them (a few extra are harmless).
	before, err := s.referencedPhotos(ctx)
	if err != nil {
		return Backup{}, err
	}

	if err := s.db.BackupTo(ctx, path); err != nil {
		return Backup{}, err
	}

	photos, photoErr := s.backupPhotos(ctx, name, before)

	// The database copy is a backup even when its photos failed, so rotation still runs.
	if err := s.prune(); err != nil {
		s.log.Warn("pruning backups", "error", err)
	}

	if photoErr != nil {
		return Backup{}, fmt.Errorf("backing up photos: %w", photoErr)
	}

	b, err = s.backups.Stat(name)
	if err != nil {
		return Backup{}, err
	}

	b.Photos = photos

	return b, nil
}

// ListBackups returns the backups, newest first.
func (s *Service) ListBackups(context.Context) ([]Backup, error) { return s.list() }

// list returns the backups, newest first: the order rotation keeps them in.
func (s *Service) list() ([]Backup, error) {
	out, err := s.backups.List()
	if err != nil {
		return nil, err
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

// prune removes the backups beyond the newest keep, then the shared photos none of the kept ones
// lists.
func (s *Service) prune() error {
	backups, err := s.list()
	if err != nil || len(backups) <= s.keep {
		return err
	}

	for _, b := range backups[s.keep:] {
		if err := s.backups.Remove(b.Name); err != nil {
			return err
		}
	}

	if s.photos == nil {
		return nil
	}

	keep := map[game.PhotoID]bool{}

	for _, b := range backups[:s.keep] {
		// A list that cannot be read stops the pruning of photos: going on would delete the
		// photos that backup needs.
		ids, err := s.backups.PhotoList(b.Name)
		if err != nil {
			return err
		}

		for _, id := range ids {
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

// backupPhotos records the backup's photo list (the photos referenced before the copy, plus those
// referenced now) and puts them in the shared store.
func (s *Service) backupPhotos(ctx context.Context, name string, before map[game.PhotoID]bool) (int, error) {
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

	if err := s.backups.WritePhotoList(name, ids); err != nil {
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

// RunScheduledBackups creates a backup every interval until ctx ends.
func (s *Service) RunScheduledBackups(ctx context.Context, interval time.Duration) {
	s.ScheduleBackups(interval)

	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.ScheduledBackup(ctx) // logged and kept for the health check
		}
	}
}

// ScheduleBackups records the automatic backups' interval (0: off), which the health check expects
// them to keep.
func (s *Service) ScheduleBackups(interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.backupEvery = interval
}

// ScheduledBackup makes one automatic backup (see CreateBackup, which remembers the result).
func (s *Service) ScheduledBackup(ctx context.Context) error {
	b, err := s.CreateBackup(ctx)
	if err != nil {
		s.log.Error("scheduled backup failed", "error", err)
		return err
	}

	s.log.Info("scheduled backup created", "name", b.Name)

	return nil
}

// noteBackup remembers a backup's result for the health check: a failure until the next success,
// automatic or made by hand (the issue asks the user to make one).
func (s *Service) noteBackup(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err == nil {
		s.lastBackupErr = nil
		return
	}

	s.lastBackupErr, s.lastBackupErrAt = err, s.now()
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
