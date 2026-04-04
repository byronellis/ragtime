package cli

import (
	"github.com/spf13/cobra"
)

var (
	flagSocket string
	flagConfig string
)

// NewRootCmd creates the top-level rt command.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rt",
		Short: "Ragtime — programmable context management for AI agents",
		Long:  "Ragtime integrates with AI coding agent hook systems to provide dynamic context injection, RAG search, and programmable tool approval.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&flagSocket, "socket", "", "path to daemon socket (default ~/.ragtime/daemon.sock)")
	cmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to config file")

	cmd.AddCommand(
		newDaemonCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newStatusCmd(),
		newHookCmd(),
		newSearchCmd(),
		newIndexCmd(),
		newAddCmd(),
		newSetupCmd(),
		newSessionCmd(),
		newRulesCmd(),
		newTUICmd(),
		newStatuslineCmd(),
		newShCmd(),
		newMountCmd(),
		newUmountCmd(),
	)

	return cmd
}
