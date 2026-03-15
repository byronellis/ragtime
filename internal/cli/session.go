package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Inspect and annotate agent sessions",
	}

	cmd.AddCommand(
		newSessionListCmd(),
		newSessionHistoryCmd(),
		newSessionNoteCmd(),
	)

	return cmd
}

func newSessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active and recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("session list: not yet implemented")
			return nil
		},
	}
}

func newSessionHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show event history for a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("session history: not yet implemented")
			return nil
		},
	}
	cmd.Flags().Int("last", 20, "number of recent events to show")
	return cmd
}

func newSessionNoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "note <text>",
		Short: "Save a note to the current session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("session note: not yet implemented (note=%s)\n", args[0])
			return nil
		},
	}
}
