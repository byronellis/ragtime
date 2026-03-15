package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <collection> [content]",
		Short: "Add content to a RAG collection",
		Long:  "Adds text content directly to a RAG collection. If content is omitted, reads from stdin.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection := args[0]

			var content string
			if len(args) >= 2 {
				content = args[1]
			} else {
				// Read from stdin
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				content = strings.TrimSpace(string(data))
			}

			if content == "" {
				return fmt.Errorf("no content provided")
			}

			source, _ := cmd.Flags().GetString("source")
			if source == "" {
				source = "cli-input"
			}

			metadata, _ := cmd.Flags().GetStringToString("metadata")

			engine, err := makeRAGEngine()
			if err != nil {
				return err
			}

			if err := engine.AddContent(collection, content, source, metadata); err != nil {
				return err
			}

			fmt.Printf("Added content to %q (source: %s)\n", collection, source)
			return nil
		},
	}

	cmd.Flags().String("source", "", "source identifier for the content")
	cmd.Flags().StringToString("metadata", nil, "key=value metadata pairs")

	return cmd
}
