package cli

import (
	"fmt"
	"path/filepath"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/hook"
	"github.com/byronellis/ragtime/internal/project"
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
			cfg, err := config.Load(".")
			if err != nil {
				return err
			}

			// Collect rules from all sources
			rules := append([]config.RuleConfig{}, cfg.Rules...)

			globalDir := project.GlobalDir()
			if globalDir != "" {
				dirRules, _ := hook.LoadRulesFromDirs(filepath.Join(globalDir, "rules"))
				rules = append(rules, dirRules...)
			}

			projDir := project.RagtimeDir(".")
			if projDir != "" {
				dirRules, _ := hook.LoadRulesFromDirs(filepath.Join(projDir, "rules"))
				rules = append(rules, dirRules...)
			}

			if len(rules) == 0 {
				fmt.Println("No rules configured.")
				fmt.Println("Add rules to ~/.ragtime/rules/ or .ragtime/rules/")
				return nil
			}

			for _, r := range rules {
				match := ""
				if r.Match.Agent != "" {
					match += "agent=" + r.Match.Agent + " "
				}
				if r.Match.Event != "" {
					match += "event=" + r.Match.Event + " "
				}
				if r.Match.Tool != "" {
					match += "tool=" + r.Match.Tool + " "
				}
				if r.Match.PathGlob != "" {
					match += "path=" + r.Match.PathGlob + " "
				}
				if match == "" {
					match = "(match all) "
				}

				actions := ""
				for i, a := range r.Actions {
					if i > 0 {
						actions += ", "
					}
					actions += a.Type
				}

				fmt.Printf("  %-25s %s-> %s\n", r.Name, match, actions)
			}

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
