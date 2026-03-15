package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <collection> <query>",
		Short: "Search a RAG collection",
		Long:  "Performs a semantic search over an indexed collection and returns the most relevant chunks.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("search: not yet implemented (collection=%s, query=%s)\n", args[0], args[1])
			return nil
		},
	}

	cmd.Flags().Int("top-k", 5, "number of results to return")
	cmd.Flags().Bool("collections", false, "list available collections instead of searching")

	return cmd
}
