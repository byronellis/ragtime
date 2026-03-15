package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/spf13/cobra"
)

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the ragtime daemon",
		Long:  "Stops the running daemon (if any) and starts a new one in the background. Useful after installing a new version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := resolveSocket()

			pid, running := daemon.Status(socketPath)
			if running {
				fmt.Fprintf(cmd.OutOrStdout(), "Stopping daemon (pid %d)...\n", pid)
			}

			if err := daemon.Restart(socketPath); err != nil {
				return hintLog(socketPath, err)
			}

			newPid, ok := daemon.Status(socketPath)
			if !ok {
				return hintLog(socketPath, fmt.Errorf("daemon failed to start after restart"))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Daemon started (pid %d)\n", newPid)
			return nil
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the ragtime daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := resolveSocket()

			pid, running := daemon.Status(socketPath)
			if !running {
				// Even if Status says not running, try to clean up by PID file
				if pid > 0 && daemon.IsRunning(pid) {
					fmt.Fprintf(cmd.OutOrStdout(), "Stopping daemon (pid %d)...\n", pid)
					if err := daemon.Stop(socketPath); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopped")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon not running")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Stopping daemon (pid %d)...\n", pid)
			if err := daemon.Stop(socketPath); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopped")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := resolveSocket()

			pid, running := daemon.Status(socketPath)
			if running {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon running (pid %d)\n", pid)
			} else if pid > 0 && daemon.IsRunning(pid) {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon running (pid %d) but socket not responding\n", pid)
			} else if pid > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon not running (stale pid %d)\n", pid)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon not running")
			}
			return nil
		},
	}
}

// hintLog wraps an error with a pointer to the daemon log file if it exists.
func hintLog(socketPath string, err error) error {
	logPath := filepath.Join(filepath.Dir(socketPath), "daemon.log")
	if _, statErr := os.Stat(logPath); statErr == nil {
		return fmt.Errorf("%w\ncheck daemon log: %s", err, logPath)
	}
	return err
}

// resolveSocket returns the socket path from the flag or config default.
func resolveSocket() string {
	if flagSocket != "" {
		return flagSocket
	}
	cfg, err := config.Load(flagConfig)
	if err != nil {
		cfg = config.Defaults()
	}
	return cfg.Daemon.Socket
}
