package logfile

import (
	"log/slog"
	"strings"
	"testing"

	"gamevault/internal/domain/settings"
)

func TestRotationAndRetention(t *testing.T) {
	dir := t.TempDir()

	var lvl slog.LevelVar

	r, err := Open(dir, &lvl, settings.Logging{Level: "debug", MaxFileSizeMB: 1, MaxFiles: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if lvl.Level() != slog.LevelDebug {
		t.Fatalf("level not applied: %v", lvl.Level())
	}

	line := strings.Repeat("x", 1023) + "\n" // 1 KiB
	for range 5 * 1024 {                     // 5 MiB → several rotations with a 1 MiB limit
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}

	files, err := r.List()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 3 || !files[0].Current {
		t.Fatalf("expected 3 files with the current first, got %+v", files)
	}

	for _, f := range files {
		if f.SizeBytes > 1<<20 {
			t.Errorf("%s exceeds the size limit: %d", f.Name, f.SizeBytes)
		}
	}

	content, truncated, err := r.Read(files[0].Name, 10)
	if err != nil || !truncated || strings.Count(content, "\n") != 10 {
		t.Fatalf("tail read: lines=%d truncated=%v err=%v", strings.Count(content, "\n"), truncated, err)
	}

	if _, _, err := r.Read("../gamevault.db", 0); err != ErrInvalidName {
		t.Fatalf("path traversal must be rejected, got %v", err)
	}

	// Lowering retention prunes immediately; clearing removes every archived file.
	if err := r.Apply(settings.Logging{Level: "info", MaxFileSizeMB: 1, MaxFiles: 2}); err != nil {
		t.Fatal(err)
	}

	if files, _ := r.List(); len(files) != 2 {
		t.Fatalf("expected 2 files after lowering retention, got %d", len(files))
	}

	if n, err := r.ClearArchived(); err != nil || n != 1 {
		t.Fatalf("clear: n=%d err=%v", n, err)
	}
}
