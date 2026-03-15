package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// writePIDFile writes the current process ID to the given path.
func writePIDFile(path string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)
}

// ReadPIDFile reads a PID from the given file. Returns 0 if the file doesn't exist.
func ReadPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	return pid, nil
}

// IsRunning checks if a process with the given PID is still running.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Use syscall.Kill with signal 0 to probe process existence.
	// os.Process.Signal(nil) does NOT work — it returns "unsupported signal type".
	return syscall.Kill(pid, 0) == nil
}
