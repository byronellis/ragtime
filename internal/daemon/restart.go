package daemon

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Restart stops the running daemon (if any) and starts a new one in the background.
// It waits for the old daemon to exit and the new one to become ready.
func Restart(socketPath string) error {
	stateDir := filepath.Dir(socketPath)
	pidPath := filepath.Join(stateDir, "daemon.pid")

	// Find and stop existing daemon
	if err := stopExisting(socketPath, pidPath); err != nil {
		return fmt.Errorf("stop existing daemon: %w", err)
	}

	// Start new daemon via EnsureRunning (reuses the autostart logic)
	return EnsureRunning(socketPath)
}

// stopExisting sends SIGTERM to the running daemon and waits for it to exit.
// Returns nil if no daemon is running.
func stopExisting(socketPath, pidPath string) error {
	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		return err
	}
	if pid == 0 || !IsRunning(pid) {
		// No daemon running — clean up stale files
		os.Remove(pidPath)
		os.Remove(socketPath)
		return nil
	}

	// Send SIGTERM for graceful shutdown
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited between our check and signal
		if !isProcessGone(err) {
			return fmt.Errorf("signal daemon (pid %d): %w", pid, err)
		}
		os.Remove(pidPath)
		os.Remove(socketPath)
		return nil
	}

	// Wait for process to exit
	if err := waitForExit(pid, 10*time.Second); err != nil {
		return err
	}

	// Clean up stale files in case the daemon didn't remove them
	os.Remove(pidPath)
	os.Remove(socketPath)
	return nil
}

// waitForExit polls until the process is no longer running or the timeout expires.
func waitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon (pid %d) did not exit within %s", pid, timeout)
}

// Stop sends SIGTERM to the running daemon and waits for it to exit.
// Returns nil if no daemon is running.
func Stop(socketPath string) error {
	stateDir := filepath.Dir(socketPath)
	pidPath := filepath.Join(stateDir, "daemon.pid")
	return stopExisting(socketPath, pidPath)
}

// Status returns the PID of the running daemon, or 0 if not running.
func Status(socketPath string) (pid int, running bool) {
	stateDir := filepath.Dir(socketPath)
	pidPath := filepath.Join(stateDir, "daemon.pid")

	pid, err := ReadPIDFile(pidPath)
	if err != nil || pid == 0 {
		return 0, false
	}

	if !IsRunning(pid) {
		return pid, false
	}

	// Verify socket is actually responsive
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return pid, false
	}
	conn.Close()
	return pid, true
}

func isProcessGone(err error) bool {
	// On Unix, signaling a non-existent process returns ESRCH
	return os.IsPermission(err) || err == syscall.ESRCH || os.IsNotExist(err)
}
