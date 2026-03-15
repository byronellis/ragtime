package daemon

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// EnsureRunning checks if the daemon is running. If not, starts it in the
// background and waits for the socket to become available.
func EnsureRunning(socketPath string) error {
	// Try connecting to existing socket
	if conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond); err == nil {
		conn.Close()
		return nil
	}

	// Check PID file
	stateDir := filepath.Dir(socketPath)
	pidPath := filepath.Join(stateDir, "daemon.pid")
	if pid, err := ReadPIDFile(pidPath); err == nil && IsRunning(pid) {
		// Daemon process exists but socket isn't ready, wait a bit
		return waitForSocket(socketPath, 3*time.Second)
	}

	// Need to start daemon
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// Detach from parent process group
	cmd.SysProcAttr = daemonSysProcAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Detach — we don't wait for the daemon process
	cmd.Process.Release()

	return waitForSocket(socketPath, 5*time.Second)
}

func waitForSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start within %s", timeout)
}
