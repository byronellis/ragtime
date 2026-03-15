package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Start the ragtime daemon",
		Long:  "Starts the ragtime daemon in the foreground. The daemon listens on a Unix socket and an HTTP port for hook events, CLI commands, and web UI requests.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("daemon: not yet implemented")
			return nil
		},
	}
}
