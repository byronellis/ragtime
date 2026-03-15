package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage RAG indexes",
		Long:  "Create, update, list, and delete RAG indexes for document collections.",
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
			fmt.Printf("index create: not yet implemented (name=%s, path=%s)\n", args[0], path)
			return nil
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
			fmt.Println("index list: not yet implemented")
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
			fmt.Printf("index delete: not yet implemented (name=%s)\n", args[0])
			return nil
		},
	}
}
