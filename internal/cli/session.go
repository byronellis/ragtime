package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Inspect and annotate agent sessions",
	}

	cmd.AddCommand(
		newSessionListCmd(),
		newSessionHistoryCmd(),
		newSessionNoteCmd(),
	)

	return cmd
}

func newSessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active and recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sendCommand("sessions", nil)
			if err != nil {
				return fmt.Errorf("session list: %w", err)
			}

			sessions, _ := resp.Data.([]interface{})
			if len(sessions) == 0 {
				fmt.Println("No active sessions.")
				fmt.Println("Sessions are tracked automatically when hooks fire.")
				return nil
			}

			fmt.Printf("%-10s  %-36s  %6s  %s\n", "AGENT", "SESSION ID", "EVENTS", "STARTED")
			fmt.Println("----------  ------------------------------------  ------  -------")
			for _, item := range sessions {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				agent, _ := m["agent"].(string)
				sid, _ := m["session_id"].(string)
				count, _ := m["event_count"].(float64)
				startedStr, _ := m["started_at"].(string)
				started := parseTime(startedStr)
				fmt.Printf("%-10s  %-36s  %6d  %s\n",
					agent, sid, int(count), started.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
}

func newSessionHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <session-id>",
		Short: "Show event history for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			last, _ := cmd.Flags().GetInt("last")
			agent, _ := cmd.Flags().GetString("agent")

			resp, err := sendCommand("session-history", map[string]any{
				"agent":      agent,
				"session_id": args[0],
				"last":       float64(last),
			})
			if err != nil {
				return fmt.Errorf("session history: %w", err)
			}

			events, _ := resp.Data.([]interface{})
			if len(events) == 0 {
				fmt.Println("No events found.")
				return nil
			}

			for _, item := range events {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				ts := parseTime(stringField(m, "timestamp"))
				eventType := stringField(m, "event_type")
				tool := stringField(m, "tool_name")
				detail := stringField(m, "detail")

				line := fmt.Sprintf("%s  %-18s", ts.Format("15:04:05"), eventType)
				if tool != "" {
					line += fmt.Sprintf("  %-12s", tool)
				}
				if detail != "" {
					if len(detail) > 60 {
						detail = detail[:60] + "…"
					}
					line += "  " + detail
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	cmd.Flags().Int("last", 20, "number of recent events to show")
	cmd.Flags().String("agent", "claude", "agent platform")
	return cmd
}

func newSessionNoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "note <text>",
		Short: "Save a note to the current session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: connect to daemon and add note to current session
			fmt.Printf("session note: requires running daemon (rt daemon)\n")
			return nil
		},
	}
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Local()
		}
	}
	return time.Time{}
}

func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
