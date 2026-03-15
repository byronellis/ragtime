package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List and manage hook rules",
	}

	cmd.AddCommand(
		newRulesListCmd(),
		newRulesExplainCmd(),
	)

	return cmd
}

func newRulesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all loaded rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("rules list: not yet implemented")
			return nil
		},
	}
}

func newRulesExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <name>",
		Short: "Show what a rule does and when it fires",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("rules explain: not yet implemented (name=%s)\n", args[0])
			return nil
		},
	}
}
