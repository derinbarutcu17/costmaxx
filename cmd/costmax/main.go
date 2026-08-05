package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/codex"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/mcp"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

var cfg *config.Config
var artStore *artifacts.Store
var db *store.DB
var adapter *codex.Adapter

// version is overwritten at build time via -ldflags "-X main.version=...".
// When installed with plain `go install` (no ldflags), fall back to the
// module version recorded in the build info.
var version = "dev"

// init runs after package-level variables are initialized, so assigning
// rootCmd.Version here (not in the struct literal) is what actually sticks.
func init() {
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
	rootCmd.Version = version
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "costmax",
	Short: "CostMax - local-first efficiency layer for coding agents",
	Long: `CostMax gives coding agents the smallest sufficient working context
and measures whether that saves resources without hurting verified outcomes.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			return nil
		}
		return initCore()
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verify CostMax's Codex MCP setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		results := doctorResults()
		failed := false
		keys := make([]string, 0, len(results))
		for key := range results {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := results[key]
			status := "✓"
			if val != "OK" {
				status = "✗"
				failed = true
			}
			fmt.Printf("%-25s %s  %s\n", key, status, val)
		}
		if failed {
			fmt.Printf("\nRemediation: run %q, then rerun %q.\n", "costmax install", "costmax doctor")
			return fmt.Errorf("CostMax doctor found setup problems")
		}
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the CostMax MCP entry into Codex",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, status, err := installCodexMCP()
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", status, path)
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove only the CostMax MCP entry from Codex",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, status, err := uninstallCodexMCP()
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", status, path)
		return nil
	},
}

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

var benchmarkCmd = &cobra.Command{
	Use:    "benchmark",
	Short:  "Run benchmarks (not yet implemented — synthetic data only)",
	Hidden: true,
}

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Handle Codex lifecycle hooks (reads JSON from stdin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp := adapter.HandleHook(os.Stdin)
		return json.NewEncoder(os.Stdout).Encode(resp)
	},
}

var mcpServer *mcp.Server

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
		return mcpServer.Serve(os.Stdin, os.Stdout)
	},
}

func initCore() error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".costmax")

	cfgPath := filepath.Join(dataDir, "config.toml")
	var err error
	cfg, err = config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dirs := []string{cfg.Core.DataDir, cfg.Core.LogDir, cfg.Store.ArtifactDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	db, err = store.Open(cfg.Store.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	artStore, err = artifacts.NewStore(cfg.Store.ArtifactDir, cfg.Store.MaxArtifactSize)
	if err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}

	adapter = codex.New(cfg, artStore, db)
	return nil
}

func codexConfigPath() (string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "config.toml"), nil
}

func costmaxMCPBlock(binary string) string {
	return fmt.Sprintf("[mcp_servers.costmaxx]\ncommand = %q\nargs = [\"mcp\"]\nrequired = true\ndefault_tools_approval_mode = \"approve\"\n", binary)
}

func tableRange(text, header string) (int, int, bool) {
	start := -1
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.TrimSpace(line) == header {
			start = offset
			break
		}
		offset += len(line)
	}
	if start < 0 {
		return 0, 0, false
	}
	end := len(text)
	offset = start + len(header)
	for _, line := range strings.SplitAfter(text[offset:], "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			end = offset
			break
		}
		offset += len(line)
	}
	return start, end, true
}

func isCostmaxMCPBlock(block string) bool {
	return strings.Contains(block, `args = ["mcp"]`) && strings.Contains(strings.ToLower(block), "costmax")
}

func installCodexMCP() (string, string, error) {
	configPath, err := codexConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate Codex config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read Codex config: %w", err)
	}
	text := string(data)
	if start, end, found := tableRange(text, "[mcp_servers.costmaxx]"); found {
		if !isCostmaxMCPBlock(text[start:end]) {
			return "", "", fmt.Errorf("refusing to overwrite existing [mcp_servers.costmaxx] entry")
		}
		return configPath, "already installed", nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return "", "", fmt.Errorf("create Codex config directory: %w", err)
	}
	if len(data) > 0 {
		backup := fmt.Sprintf("%s.costmaxx.bak.%s", configPath, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.WriteFile(backup, data, 0600); err != nil {
			return "", "", fmt.Errorf("back up Codex config: %w", err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find CostMax binary: %w", err)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		text += "\n"
	}
	if err := os.WriteFile(configPath, []byte(text+costmaxMCPBlock(binary)), 0600); err != nil {
		return "", "", fmt.Errorf("write Codex config: %w", err)
	}
	return configPath, "installed", nil
}

func uninstallCodexMCP() (string, string, error) {
	configPath, err := codexConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate Codex config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return configPath, "not installed", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read Codex config: %w", err)
	}
	text := string(data)
	start, end, found := tableRange(text, "[mcp_servers.costmaxx]")
	if !found {
		return configPath, "not installed", nil
	}
	if !isCostmaxMCPBlock(text[start:end]) {
		return "", "", fmt.Errorf("refusing to remove a non-CostMax [mcp_servers.costmaxx] entry")
	}
	if err := os.WriteFile(configPath, []byte(strings.TrimLeft(text[:start]+text[end:], "\n")), 0600); err != nil {
		return "", "", fmt.Errorf("update Codex config: %w", err)
	}
	return configPath, "uninstalled", nil
}

func doctorResults() map[string]string {
	results := map[string]string{}
	if binary, err := os.Executable(); err != nil {
		results["binary"] = err.Error()
	} else if info, err := os.Stat(binary); err != nil || info.Mode()&0111 == 0 {
		results["binary"] = "not executable"
	} else {
		results["binary"] = "OK"
	}

	configPath, err := codexConfigPath()
	if err != nil {
		results["codex_mcp_config"] = err.Error()
	} else if data, readErr := os.ReadFile(configPath); readErr != nil {
		results["codex_mcp_config"] = "not installed"
	} else if start, end, found := tableRange(string(data), "[mcp_servers.costmaxx]"); !found || !isCostmaxMCPBlock(string(data)[start:end]) {
		results["codex_mcp_config"] = "not installed"
	} else {
		results["codex_mcp_config"] = "OK"
	}

	probe, err := os.CreateTemp(cfg.Store.ArtifactDir, ".costmax-doctor-*")
	if err != nil {
		results["artifact_store"] = err.Error()
	} else {
		probe.Close()
		os.Remove(probe.Name())
		results["artifact_store"] = "OK"
	}

	server, err := mcp.NewServer(cfg)
	if err != nil {
		results["mcp_handshake"] = err.Error()
		return results
	}
	defer server.Close()
	var out bytes.Buffer
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"costmax-doctor","version":"1"}}}` + "\n"
	if err := server.Serve(strings.NewReader(request), &out); err != nil || !strings.Contains(out.String(), `"serverInfo"`) {
		results["mcp_handshake"] = "failed"
	} else {
		results["mcp_handshake"] = "OK"
	}
	return results
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stateCmd)
	rootCmd.AddCommand(evidenceCmd)
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(modeCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(gcCmd)
	rootCmd.AddCommand(benchmarkCmd)
	rootCmd.AddCommand(hookCmd)
	rootCmd.AddCommand(mcpCmd)
	evidenceCmd.AddCommand(evidenceShowCmd)
	evidenceCmd.AddCommand(evidenceSearchCmd)
	evidenceShowCmd.Flags().Int("lines", 0, "Number of lines to show")
	evidenceShowCmd.Flags().Int("start", 0, "Start line")
	evidenceShowCmd.Flags().Int("end", 0, "End line")

	disableCmd.Flags().Bool("session", false, "Disable only for current session")
	resetCmd.Flags().Bool("session", false, "Reset only current session")
	gcCmd.Flags().Duration("older-than", 336*time.Hour, "Delete artifacts older than duration")

}
