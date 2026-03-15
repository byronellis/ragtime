package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup <agent>",
		Short: "Generate hook configs and skill definitions for an agent",
		Long:  "Generates the hook configuration and agent skill documentation for the specified agent platform.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("setup: not yet implemented (agent=%s)\n", args[0])
			return nil
		},
	}

	return cmd
}
