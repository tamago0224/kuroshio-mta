//go:build !linux && !darwin && !freebsd && !openbsd && !dragonfly

package queue

import (
	"errors"
	"os"
)

func diskFreeBytes(string) (uint64, error) {
	return 0, errors.New("queue disk capacity check unsupported on this platform")
}

func lockQueue(string) (*os.File, error) {
	return nil, errors.New("queue capacity locking unsupported on this platform")
}
