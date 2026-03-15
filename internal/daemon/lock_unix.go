//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"syscall"
)

// lockFile holds an exclusive flock on a file to enforce single-instance.
type lockFile struct {
	f *os.File
}

// acquireLock tries to get an exclusive lock on path. Returns an error if
// another daemon already holds the lock.
func acquireLock(path string) (*lockFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	// LOCK_EX | LOCK_NB: exclusive, non-blocking
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another daemon is already running (lock held on %s)", path)
	}

	return &lockFile{f: f}, nil
}

// release drops the lock and closes the file.
func (l *lockFile) release() {
	if l.f != nil {
		syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
		l.f.Close()
	}
}
