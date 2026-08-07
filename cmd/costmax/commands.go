package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/derinbarutcu17/costmaxx/internal/mcp"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current CostMax status",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Mode:      %s\n", cfg.Mode)
		fmt.Printf("Session:   %s\n", adapter.SessionID())
		fmt.Printf("Data dir:  %s\n", cfg.Core.DataDir)
		fmt.Printf("Artifacts: %s\n", cfg.Store.ArtifactDir)

		metricsSnap := adapter.Metrics().Snapshot()
		fmt.Printf("\nMetrics:\n")
		fmt.Printf("  Tool calls:      %d\n", metricsSnap.ToolCalls)
		fmt.Printf("  Turns:           %d\n", metricsSnap.Turns)
		fmt.Printf("  Retries:         %d\n", metricsSnap.Retries)
		fmt.Printf("  Artifacts reduced: %d\n", metricsSnap.ArtifactsReduced)
		fmt.Printf("  Rehydrations:    %d\n", metricsSnap.EvidenceRehydrated)
		fmt.Printf("  Hook failures:   %d\n", metricsSnap.HookFailures)
		return nil
	},
}

var stateCmd = &cobra.Command{
	Use:   "state <session-id>",
	Short: "Show task state for a session (experimental)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ts, err := db.LoadTaskState(args[0])
		if err != nil || ts == nil {
			fmt.Println("No state found for session.")
			return nil
		}
		fmt.Printf("Task ID:     %s\n", ts.TaskID)
		fmt.Printf("Objective:   %s\n", ts.Objective)
		if len(ts.UnresolvedIssues) > 0 {
			fmt.Printf("Unresolved:  \n")
			for _, i := range ts.UnresolvedIssues {
				fmt.Printf("  - %s\n", i)
			}
		}
		if len(ts.Decisions) > 0 {
			fmt.Printf("Decisions:\n")
			for _, d := range ts.Decisions {
				fmt.Printf("  - %s\n", d.Value)
			}
		}
		fmt.Printf("Next action: %s\n", ts.NextAction)
		return nil
	},
}

var evidenceCmd = &cobra.Command{
	Use:   "evidence",
	Short: "Manage stored evidence",
}

var evidenceShowCmd = &cobra.Command{
	Use:   "show <artifact-id>",
	Short: "Show stored evidence for an artifact",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifactID := args[0]
		lines, _ := cmd.Flags().GetInt("lines")
		start, _ := cmd.Flags().GetInt("start")
		end, _ := cmd.Flags().GetInt("end")

		fmt.Printf("Evidence: %s\n\n", artifactID)

		if start > 0 && end > 0 {
			_ = lines
			fmt.Printf("Range: lines %d-%d\n\n", start, end)
		}

		fmt.Println("[Use costmax evidence retrieve <id> for full content]")
		return nil
	},
}

var evidenceSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search stored evidence",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		fmt.Printf("Searching evidence for: %s\n\n", query)
		fmt.Println("Search is a v1 placeholder. Full search coming in Phase 2.")
		return nil
	},
}

var reportCmd = &cobra.Command{
	Use:   "report <session-id>",
	Short: "Generate a session report from persisted metrics (experimental)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]

		evts, err := db.GetSessionEvents(sessionID)
		if err != nil {
			return fmt.Errorf("load events: %w", err)
		}

		rt, ct, ar, tc, err := db.GetSessionMetrics(sessionID)
		if err != nil {
			return fmt.Errorf("load metrics: %w", err)
		}

		fmt.Printf("Session: %s\n", sessionID)
		fmt.Printf("Events:  %d\n\n", len(evts))

		fmt.Println("Context Reduction")
		if rt > 0 {
			pct := float64(rt-ct) / float64(rt) * 100
			fmt.Printf("  Eligible raw output:           %d estimated tokens\n", rt)
			fmt.Printf("  Model-visible output:          %d estimated tokens\n", ct)
			fmt.Printf("  Reduction:                     %.1f%%\n", pct)
		}

		fmt.Println("\nEvidence")
		fmt.Printf("  Stored artifacts:              %d\n", ar)

		fmt.Println("\nTool calls")
		fmt.Printf("  Total:                         %d\n", tc)

		return nil
	},
}

var modeCmd = &cobra.Command{
	Use:   "mode [observe|active]",
	Short: "Set CostMax mode (active not yet implemented)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode := args[0]
		if mode != "observe" {
			return fmt.Errorf("only observe mode is implemented; active reduction not yet available")
		}
		cfg.Mode = mode
		if err := cfg.Save(filepath.Join(cfg.Core.DataDir, "config.toml")); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("Mode set to: %s\n", mode)
		return nil
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable CostMax for the current session",
	RunE: func(cmd *cobra.Command, args []string) error {
		session, _ := cmd.Flags().GetBool("session")
		if session {
			fmt.Println("CostMax disabled for this session. Set COSTMAX_DISABLE=1 to disable.")
		} else {
			cfg.Mode = "observe"
			cfg.Save(filepath.Join(cfg.Core.DataDir, "config.toml"))
			fmt.Println("CostMax disabled. Mode set to observe.")
		}
		return nil
	},
}

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset CostMax session state",
	RunE: func(cmd *cobra.Command, args []string) error {
		session, _ := cmd.Flags().GetBool("session")
		if session {
			fmt.Println("Session state reset.")
		} else {
			os.RemoveAll(cfg.Core.DataDir)
			fmt.Println("All CostMax data removed.")
		}
		return nil
	},
}

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Run garbage collection on stored artifacts",
	RunE: func(cmd *cobra.Command, args []string) error {
		olderThan, _ := cmd.Flags().GetDuration("older-than")
		if olderThan == 0 {
			olderThan = 336 * time.Hour
		}
		if err := artStore.DeleteOlderThan(olderThan); err != nil {
			return fmt.Errorf("gc: %w", err)
		}
		fmt.Printf("Cleaned artifacts older than %v\n", olderThan)
		return nil
	},
}

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Handle Codex lifecycle hooks (reads JSON from stdin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp := adapter.HandleHook(os.Stdin)
		return json.NewEncoder(os.Stdout).Encode(resp)
	},
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start CostMax MCP server (stdio JSON-RPC)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpServer == nil {
			var err error
			mcpServer, err = mcp.NewServer(cfg)
			if err != nil {
				return fmt.Errorf("mcp init: %w", err)
			}
			defer mcpServer.Close()
		}
		if mcpSpecFraming {
			return mcpServer.ServeSpec(os.Stdin, os.Stdout)
		}
		return mcpServer.Serve(os.Stdin, os.Stdout)
	},
}
