//go:build linux || darwin || freebsd || openbsd || dragonfly

package queue

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func diskFreeBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func lockQueue(root string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(root, ".capacity.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("queue capacity requires exclusive directory ownership: %w", err)
	}
	return f, nil
}
