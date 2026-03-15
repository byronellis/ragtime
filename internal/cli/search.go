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
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			listMode, _ := cmd.Flags().GetBool("collections")
			if listMode || len(args) == 0 {
				return listCollections()
			}

			if len(args) < 2 {
				return fmt.Errorf("usage: rt search <collection> <query>")
			}

			topK, _ := cmd.Flags().GetInt("top-k")
			return searchCollection(args[0], args[1], topK)
		},
	}

	cmd.Flags().Int("top-k", 5, "number of results to return")
	cmd.Flags().Bool("collections", false, "list available collections instead of searching")

	return cmd
}

func listCollections() error {
	engine, err := makeRAGEngine()
	if err != nil {
		return err
	}
	collections, err := engine.ListCollections()
	if err != nil {
		return err
	}
	if len(collections) == 0 {
		fmt.Println("No collections found.")
		return nil
	}
	for _, c := range collections {
		fmt.Printf("  %-25s %d chunks\n", c.Name, c.ChunkCount)
	}
	return nil
}

func searchCollection(collection, query string, topK int) error {
	engine, err := makeRAGEngine()
	if err != nil {
		return err
	}

	results, err := engine.Search(collection, query, topK)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	for i, r := range results {
		fmt.Printf("--- Result %d (score: %.4f, source: %s) ---\n", i+1, r.Score, r.Source)
		// Truncate long content for display
		content := r.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		fmt.Println(content)
		fmt.Println()
	}

	return nil
}
