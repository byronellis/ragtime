package cli

import (
	"context"
	"fmt"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the ragtime daemon in the foreground",
		Long:  "Runs the ragtime daemon in the foreground (for debugging, systemd, or launchd). Use 'rt start' to run it in the background.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Allow socket override via flag
			if flagSocket != "" {
				cfg.Daemon.Socket = flagSocket
			}

			d := daemon.New(cfg)
			return d.Run(context.Background())
		},
	}
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the ragtime daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := resolveSocket()

			pid, running := daemon.Status(socketPath)
			if running {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon already running (pid %d)\n", pid)
				return nil
			}

			if err := daemon.EnsureRunning(socketPath); err != nil {
				return hintLog(socketPath, err)
			}

			newPid, ok := daemon.Status(socketPath)
			if !ok {
				return hintLog(socketPath, fmt.Errorf("daemon failed to start"))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Daemon started (pid %d)\n", newPid)
			return nil
		},
	}
}
