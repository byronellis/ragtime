package cli

import (
	"fmt"
	"os"

	"github.com/byronellis/ragtime/internal/project"
	"github.com/byronellis/ragtime/internal/skills"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup <agent>",
		Short: "Generate hook configs and skill definitions for an agent",
		Long:  "Generates the hook configuration and agent skill documentation for the specified agent platform.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agent := args[0]

			switch agent {
			case "claude":
				return setupClaude()
			default:
				return fmt.Errorf("unsupported agent: %s (supported: claude)", agent)
			}
		},
	}

	return cmd
}

func setupClaude() error {
	// Determine project directory
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	projectDir := project.FindRoot(cwd)
	if projectDir == "" {
		projectDir = cwd
	}

	fmt.Printf("Setting up Claude Code integration in %s\n\n", projectDir)
	return skills.SetupClaude(projectDir)
}
