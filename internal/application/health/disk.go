package health

import (
	"context"
	"errors"
	"fmt"
)

// DiskSpace is the port that reads how much room the filesystem holding a path has.
type DiskSpace interface {
	// Space returns the bytes available to Game Vault and the filesystem's size. An error (an
	// unsupported system) means it cannot tell, and the check reports nothing.
	Space(path string) (free, total uint64, err error)
}

// ErrUnsupported means the system cannot report disk space.
var ErrUnsupported = errors.New("disk space cannot be read on this system")

// Thresholds of the disk check.
const (
	diskWarnBytes    = 1 << 30   // 1 GB
	diskWarnRatio    = 0.05      // 5% of the filesystem…
	diskWarnRatioCap = 10 << 30  // …but never more than 10 GB, so a large disk does not warn with 199 GB free
	diskErrorBytes   = 200 << 20 // 200 MB
)

// DiskCheck warns when the filesystem holding the config directory runs low: under 1 GB, or under 5%
// of it (10 GB at most), is a warning, under 200 MB an error (the database, backups and images live
// there).
type DiskCheck struct {
	// Path is the config directory.
	Path string

	// Space reads the filesystem; nil means the check is off.
	Space DiskSpace
}

// Issues implements Check.
func (d DiskCheck) Issues(context.Context) []Issue {
	if d.Space == nil {
		return nil
	}

	free, total, err := d.Space.Space(d.Path)
	if err != nil || total == 0 {
		return nil
	}

	percent := fmt.Sprintf("%.0f", float64(free)*100/float64(total))
	issue := Issue{
		Key:  "disk:config",
		Kind: KindDisk,
		Params: map[string]string{
			"free":    humanBytes(free),
			"percent": percent,
			"path":    d.Path,
		},
		Message: fmt.Sprintf("only %s free (%s%%) on the disk holding the config directory %s: free some space, or move old backups away",
			humanBytes(free), percent, d.Path),
	}

	switch {
	case free < diskErrorBytes:
		issue.Level, issue.Reason = LevelError, ReasonDiskCritical
	case free < diskWarnBytes || float64(free) < min(diskWarnRatio*float64(total), diskWarnRatioCap):
		issue.Level, issue.Reason = LevelWarning, ReasonDiskLow
	default:
		return nil
	}

	return []Issue{issue}
}

// humanBytes writes a size in MB or GB.
func humanBytes(b uint64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}

	return fmt.Sprintf("%d MB", b>>20)
}
