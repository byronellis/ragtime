package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <collection> <content>",
		Short: "Add content to a RAG collection",
		Long:  "Adds text content directly to a RAG collection. Also accepts input from stdin.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("add: not yet implemented (collection=%s)\n", args[0])
			return nil
		},
	}

	cmd.Flags().String("source", "", "source identifier for the content")
	cmd.Flags().StringToString("metadata", nil, "key=value metadata pairs")

	return cmd
}
