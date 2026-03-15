package cli

import (
	"fmt"

	"github.com/byronellis/ragtime/internal/rag"
	"github.com/spf13/cobra"
)

func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage RAG indexes",
	}

	cmd.AddCommand(
		newIndexCreateCmd(),
		newIndexUpdateCmd(),
		newIndexListCmd(),
		newIndexDeleteCmd(),
	)

	return cmd
}

func newIndexCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name> --path <dir>",
		Short: "Create a new index from a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("path")
			engine, err := makeRAGEngine()
			if err != nil {
				return err
			}
			return engine.Index(args[0], path)
		},
	}
	cmd.Flags().String("path", ".", "directory to index")
	return cmd
}

func newIndexUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update <name>",
		Short: "Re-index an existing collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("index update: not yet implemented (name=%s)\n", args[0])
			return nil
		},
	}
}

func newIndexListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all indexes",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := makeRAGEngine()
			if err != nil {
				return err
			}

			collections, err := engine.ListCollections()
			if err != nil {
				return err
			}

			if len(collections) == 0 {
				fmt.Println("No indexes found.")
				fmt.Println("Create one with: rt index create <name> --path <dir>")
				return nil
			}

			for _, c := range collections {
				fmt.Printf("  %-25s %d chunks  (provider=%s, dims=%d)\n",
					c.Name, c.ChunkCount, c.Provider, c.Dimensions)
			}
			return nil
		},
	}
}

func newIndexDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := makeRAGEngine()
			if err != nil {
				return err
			}
			if err := engine.DeleteCollection(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted index %q\n", args[0])
			return nil
		},
	}
}

func makeRAGEngine() (*rag.Engine, error) {
	cfg, indexDirs, err := resolveRAGConfig()
	if err != nil {
		return nil, err
	}
	provider := makeProvider(cfg)
	return rag.NewEngine(indexDirs, provider, nil), nil
}
