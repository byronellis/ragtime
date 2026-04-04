//go:build darwin || linux || freebsd

package fs

import "syscall"

func platformUnmount(path string) error {
	return syscall.Unmount(path, 0)
}
