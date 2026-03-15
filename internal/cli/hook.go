package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Handle an agent hook event",
		Long:  "Reads a hook event from stdin, relays it to the daemon, and writes the response to stdout. Invoked by agent hook systems (Claude Code, Gemini CLI).",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("hook: not yet implemented")
			return nil
		},
	}

	cmd.Flags().String("agent", "", "agent platform (claude, gemini)")
	cmd.Flags().String("event", "", "event type (pre-tool-use, post-tool-use, stop, notification, etc.)")

	return cmd
}
