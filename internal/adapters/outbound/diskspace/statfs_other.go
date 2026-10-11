//go:build !(linux || darwin || freebsd)

package diskspace

import "gamevault/internal/application/health"

// Space implements health.DiskSpace where filesystem statistics are not read (Windows and the other systems): the check
// reports nothing.
func (Statfs) Space(string) (free, total uint64, err error) { return 0, 0, health.ErrUnsupported }
