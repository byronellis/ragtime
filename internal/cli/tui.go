package cli

import (
	"github.com/spf13/cobra"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/byronellis/ragtime/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the live dashboard",
		Long:  "Connects to the running daemon and displays live hook events, active sessions, and daemon status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			socketPath := cfg.Daemon.Socket
			if flagSocket != "" {
				socketPath = flagSocket
			}

			if err := daemon.EnsureRunning(socketPath); err != nil {
				return err
			}

			return tui.Run(socketPath)
		},
	}
}
