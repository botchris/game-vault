// Package logfile writes the server log to size-limited, rotated files in config/logs and
// implements the logs.Sink and logs.Files ports.
//
// The current file is always gamevault.log. When it would exceed the size limit it is renamed to
// gamevault-YYYYMMDD-HHMMSS-mmm.log and a new one is started; only the newest MaxFiles files are kept.
package logfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gamevault/internal/application/logs"
	"gamevault/internal/domain/settings"
)

const (
	currentName = "gamevault.log"

	// readCap bounds how much of a file is returned to the UI.
	readCap = 10 << 20

	// tailWindow is how far from the end we look when only the last lines are requested.
	tailWindow = 4 << 20
)

var (
	reLogName = regexp.MustCompile(`^gamevault(-\d{8}-\d{6}-\d{3}(-\d+)?)?\.log$`)

	// ErrInvalidName is re-exported from the logs package so callers and tests can match it.
	ErrInvalidName = logs.ErrInvalidName

	// ErrNotFound is re-exported from the logs package so callers and tests can match it.
	ErrNotFound = logs.ErrNotFound
)

// Rotator is an io.Writer that rotates files by size and prunes old ones.
type Rotator struct {
	dir   string
	level *slog.LevelVar

	mu       sync.Mutex
	file     *os.File
	size     int64
	maxBytes int64
	maxFiles int
}

var (
	_ logs.Sink  = (*Rotator)(nil)
	_ logs.Files = (*Rotator)(nil)
)

// Open starts writing to dir/gamevault.log (appending) with the given settings.
// level is updated by Apply, so the logger built on it changes level live.
func Open(dir string, level *slog.LevelVar, s settings.Logging) (*Rotator, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	r := &Rotator{dir: dir, level: level}
	if err := r.Apply(s); err != nil {
		return nil, err
	}

	if err := r.openCurrent(); err != nil {
		return nil, err
	}

	return r, nil
}

// Apply changes level, size limit and retention immediately (implements logs.Sink).
func (r *Rotator) Apply(s settings.Logging) error {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(s.Level)); err != nil {
		return err
	}

	r.level.Set(lvl)
	r.mu.Lock()
	defer r.mu.Unlock()

	r.maxBytes = int64(s.MaxFileSizeMB) << 20

	r.maxFiles = s.MaxFiles
	if r.file != nil && r.size >= r.maxBytes {
		if err := r.rotate(); err != nil {
			return err
		}
	}

	return r.prune()
}

func (r *Rotator) openCurrent() error {
	f, err := os.OpenFile(filepath.Join(r.dir, currentName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close() // the Stat error is the one worth reporting

		return err
	}

	r.file, r.size = f, info.Size()

	return nil
}

// Write implements io.Writer. A single write is never split across files.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := r.file.Write(p)
	r.size += int64(n)

	return n, err
}

// rotate must be called with mu held.
func (r *Rotator) rotate() error {
	if err := r.file.Close(); err != nil {
		return err
	}
	// Millisecond timestamps keep names unique and in chronological (= lexical) order.
	base := "gamevault-" + strings.Replace(time.Now().Format("20060102-150405.000"), ".", "-", 1)

	name := base + ".log"
	for i := 1; ; i++ { // several rotations in the same second
		if _, err := os.Stat(filepath.Join(r.dir, name)); errors.Is(err, os.ErrNotExist) {
			break
		}

		name = fmt.Sprintf("%s-%d.log", base, i)
	}

	if err := os.Rename(filepath.Join(r.dir, currentName), filepath.Join(r.dir, name)); err != nil {
		return err
	}

	if err := r.openCurrent(); err != nil {
		return err
	}

	return r.prune()
}

// archived returns rotated file names, newest first.
func (r *Rotator) archived() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}

	var names []string

	for _, e := range entries {
		if !e.IsDir() && e.Name() != currentName && reLogName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	return names, nil
}

// prune keeps maxFiles files in total (the current one included). Must be called with mu held.
func (r *Rotator) prune() error {
	names, err := r.archived()
	if err != nil {
		return err
	}

	keep := max(r.maxFiles-1, 0)
	for _, n := range names[min(keep, len(names)):] {
		if err := os.Remove(filepath.Join(r.dir, n)); err != nil {
			return err
		}
	}

	return nil
}

// List implements logs.Files.
func (r *Rotator) List() ([]logs.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	names, err := r.archived()
	if err != nil {
		return nil, err
	}

	names = append([]string{currentName}, names...)

	out := make([]logs.File, 0, len(names))
	for _, n := range names {
		info, err := os.Stat(filepath.Join(r.dir, n))
		if err != nil {
			continue
		}

		out = append(out, logs.File{Name: n, SizeBytes: info.Size(), ModifiedAt: info.ModTime(), Current: n == currentName})
	}

	return out, nil
}

// Read implements logs.Files. Names are validated, so only log files in dir can be read.
func (r *Rotator) Read(name string, tailLines int) (string, bool, error) {
	if !reLogName.MatchString(name) {
		return "", false, ErrInvalidName
	}

	f, err := os.Open(filepath.Join(r.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, ErrNotFound
	}

	if err != nil {
		return "", false, err
	}

	defer func() { _ = f.Close() }() // only read: closing cannot lose data

	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}

	window := int64(readCap)
	if tailLines > 0 {
		window = tailWindow
	}

	start := max(info.Size()-window, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", false, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", false, err
	}

	truncated := start > 0
	if truncated { // drop the partial first line
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}

	if tailLines > 0 {
		lines := strings.SplitAfter(string(data), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		if len(lines) > tailLines {
			lines, truncated = lines[len(lines)-tailLines:], true
		}

		return strings.Join(lines, ""), truncated, nil
	}

	return string(data), truncated, nil
}

// ClearArchived implements logs.Files.
func (r *Rotator) ClearArchived() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	names, err := r.archived()
	if err != nil {
		return 0, err
	}

	for i, n := range names {
		if err := os.Remove(filepath.Join(r.dir, n)); err != nil {
			return i, err
		}
	}

	return len(names), nil
}

// Close closes the current file.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.file.Close()
}
