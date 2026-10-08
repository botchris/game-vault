// Package logs implements the log viewer and log rotation settings use cases.
package logs

import (
	"context"
	"errors"
	"time"

	"gamevault/internal/domain/settings"
)

// Errors returned by Files.Read.
var (
	ErrInvalidName = errors.New("invalid log file name")
	ErrNotFound    = errors.New("log file not found")
)

// File describes one log file.
type File struct {
	Name       string
	SizeBytes  int64
	ModifiedAt time.Time
	Current    bool
}

// Sink is the port to the running logger: it applies level and rotation settings live.
type Sink interface {
	Apply(settings.Logging) error
}

// Files is the port to the log files on disk.
type Files interface {
	List() ([]File, error)
	// Read returns a file's content; with tailLines > 0 only the last lines.
	Read(name string, tailLines int) (content string, truncated bool, err error)
	// ClearArchived deletes every rotated file, keeping the current one.
	ClearArchived() (int, error)
}

// Service exposes the log use cases.
type Service struct {
	repo  settings.Repository
	sink  Sink
	files Files
}

func NewService(repo settings.Repository, sink Sink, files Files) *Service {
	return &Service{repo: repo, sink: sink, files: files}
}

// Init applies the saved settings to the running logger. Call it once at start-up.
func (s *Service) Init(ctx context.Context) error {
	l, err := s.repo.Logging(ctx)
	if err != nil {
		return err
	}
	return s.sink.Apply(l)
}

func (s *Service) Settings(ctx context.Context) (settings.Logging, error) {
	return s.repo.Logging(ctx)
}

// UpdateSettings validates, saves and applies new settings immediately (no restart needed).
func (s *Service) UpdateSettings(ctx context.Context, l settings.Logging) (settings.Logging, error) {
	l, err := l.Validate()
	if err != nil {
		return l, err
	}
	if err := s.repo.SaveLogging(ctx, l); err != nil {
		return l, err
	}
	return l, s.sink.Apply(l)
}

func (s *Service) ListFiles(context.Context) ([]File, error) { return s.files.List() }

func (s *Service) ReadFile(_ context.Context, name string, tailLines int) (string, bool, error) {
	return s.files.Read(name, tailLines)
}

func (s *Service) ClearArchived(context.Context) (int, error) { return s.files.ClearArchived() }
