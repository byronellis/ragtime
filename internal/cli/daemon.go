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
		Short: "Start the ragtime daemon",
		Long:  "Starts the ragtime daemon in the foreground. The daemon listens on a Unix socket and an HTTP port for hook events, CLI commands, and web UI requests.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := cmd.Flags().GetString("config")
			if cwd == "" {
				cwd = "."
			}
			cfg, err := config.Load(cwd)
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
