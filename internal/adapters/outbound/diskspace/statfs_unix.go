//go:build linux || darwin || freebsd

package diskspace

import "syscall"

// Space implements health.DiskSpace: the bytes available to unprivileged users and the size of the
// filesystem holding path. It builds on Linux, macOS and FreeBSD, whose Statfs_t has these fields;
// OpenBSD and NetBSD name them differently and use the fallback.
func (Statfs) Space(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}

	bsize := uint64(st.Bsize) // a block size is positive; its type varies by system

	return uint64(st.Bavail) * bsize, uint64(st.Blocks) * bsize, nil // the fields' types vary by system
}
