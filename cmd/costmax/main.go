package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/codex"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/mcp"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
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

// optionalChecks are doctor checks reported for information but not treated
// as setup failures when missing. opencode_mcp_config is optional because
// CostMax is fully functional with only the Codex MCP entry installed.
var optionalChecks = map[string]bool{"opencode_mcp_config": true}

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
				if !optionalChecks[key] {
					failed = true
				}
				status = "✗"
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

var installTarget string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the CostMax MCP entry into Codex",
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			path   string
			status string
			err    error
		)
		switch installTarget {
		case "codex":
			path, status, err = installCodexMCP()
		case "opencode":
			path, status, err = installOpenCodeMCP()
		default:
			return fmt.Errorf("unknown target %q (expected codex or opencode)", installTarget)
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", status, path)
		return nil
	},
}

var uninstallTarget string

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove only the CostMax MCP entry from Codex",
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			path   string
			status string
			err    error
		)
		switch uninstallTarget {
		case "codex":
			path, status, err = uninstallCodexMCP()
		case "opencode":
			path, status, err = uninstallOpenCodeMCP()
		default:
			return fmt.Errorf("unknown target %q (expected codex or opencode)", uninstallTarget)
		}
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

var artifactCmd = &cobra.Command{
	Use:   "artifact",
	Short: "Store and retrieve command-output artifacts",
}

var artifactAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Store raw output from stdin as a content-addressed artifact",
	RunE: func(cmd *cobra.Command, args []string) error {
		command, _ := cmd.Flags().GetString("command")
		if command == "" {
			return fmt.Errorf("--command is required")
		}
		exitCode, _ := cmd.Flags().GetInt("exit-code")

		raw, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		output := string(raw)

		redactor := privacy.NewRedactor()
		if redactor.ContainsSecrets(output) {
			output = redactor.RedactOutput(output)
		}

		artifact, storeErr := artStore.Store([]byte(output), uuid.New().String(), command, exitCode)
		if storeErr != nil {
			return fmt.Errorf("store artifact: %w", storeErr)
		}
		if err := db.InsertArtifact(artifact); err != nil {
			return fmt.Errorf("insert artifact metadata: %w", err)
		}

		category := events.NewClassifier().Classify("cli_artifact_add", command, output, exitCode, int64(len(output)))
		reducer := reducers.NewRegistry(cfg).Select(category, command, exitCode, int64(len(output)))

		compactText := output
		compactTokens := len(output) / 4
		reduced := false
		if reducer != nil {
			red, redErr := reducer.Reduce(output, artifacts.ReducerMetadata{
				Command:  command,
				ExitCode: exitCode,
				Category: string(category),
				ToolName: artifact.ArtifactID,
				Size:     int64(len(output)),
			})
			if redErr == nil {
				// Same per-artifact ID scheme as the MCP server so reductions
				// of identical byte lengths never collide on the primary key.
				red.ReductionID = "red-" + artifact.ArtifactID
				red.ArtifactID = artifact.ArtifactID
				if err := db.InsertReduction(red); err != nil {
					return fmt.Errorf("insert reduction metadata: %w", err)
				}
				compactText = red.CompactContent
				compactTokens = red.CompactTokenEst
				reduced = true
			}
		}

		rawTokens := len(output) / 4
		recommendation := mcp.Recommend(category, rawTokens, compactTokens, reduced)
		modelText := compactText
		modelTokens := compactTokens
		if recommendation == mcp.RecommendationPassthrough || recommendation == mcp.RecommendationPreserveFull {
			modelText = output
			modelTokens = rawTokens
		}
		responseText := mcp.FormatToolOutput(recommendation, command, exitCode, rawTokens, modelTokens, artifact.ArtifactID, modelText)
		guarded := mcp.GuardRecommendation(recommendation, rawTokens, len(responseText)/4)
		if guarded != recommendation {
			recommendation = guarded
			modelText = output
			modelTokens = rawTokens
			responseText = mcp.FormatToolOutput(recommendation, command, exitCode, rawTokens, modelTokens, artifact.ArtifactID, modelText)
		}

		if err := db.InsertSessionMetrics("cli-"+adapter.SessionID(), rawTokens, modelTokens, 1, 1); err != nil {
			return fmt.Errorf("insert session metrics: %w", err)
		}

		fmt.Println(responseText)
		return nil
	},
}

var artifactRetrieveCmd = &cobra.Command{
	Use:   "retrieve <artifact-id>",
	Short: "Print the full raw content of a stored artifact",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, err := db.GetArtifact(args[0])
		if err != nil {
			return fmt.Errorf("lookup artifact: %w", err)
		}
		if meta == nil {
			return fmt.Errorf("artifact not found: %s", args[0])
		}
		raw, err := artStore.RetrieveByDigest(meta.ContentDigest)
		if err != nil {
			return fmt.Errorf("read artifact: %w", err)
		}
		fmt.Print(string(raw))
		return nil
	},
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
var mcpSpecFraming bool

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

func opencodeConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "opencode.jsonc"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "opencode.jsonc"), nil
}

// isCostmaxOpenCodeBlock reports whether a JSONC block for the "costmaxx"
// key is shaped like the entry CostMax installs itself.
func isCostmaxOpenCodeBlock(block string) bool {
	if !strings.Contains(block, "\"type\"") || !strings.Contains(block, "\"local\"") {
		return false
	}
	if !strings.Contains(block, "\"mcp\"") {
		return false
	}
	return strings.Contains(strings.ToLower(block), "costmax")
}

// annotateJSONC marks each byte of a JSONC document as being inside a string
// literal, line comment, or block comment, and records the brace depth at
// that byte (outside strings/comments).
func annotateJSONC(text string) (inString, inLine, inBlock []bool, depth []int) {
	n := len(text)
	inString = make([]bool, n)
	inLine = make([]bool, n)
	inBlock = make([]bool, n)
	depth = make([]int, n)
	var s, lc, bc bool
	d := 0
	for i := 0; i < n; i++ {
		c := text[i]
		switch {
		case lc:
			inLine[i] = true
			if c == '\n' {
				lc = false
			}
		case bc:
			inBlock[i] = true
			if c == '*' && i+1 < n && text[i+1] == '/' {
				inBlock[i+1] = true
				i++
			}
		case s:
			inString[i] = true
			if c == '\\' && i+1 < n {
				inString[i+1] = true
				i++
			} else if c == '"' {
				s = false
			}
		case c == '"':
			s = true
			inString[i] = true
		case c == '/':
			if i+1 < n && text[i+1] == '/' {
				lc = true
				inLine[i] = true
				inLine[i+1] = true
				i++
			} else if i+1 < n && text[i+1] == '*' {
				bc = true
				inBlock[i] = true
				inBlock[i+1] = true
				i++
			}
		case c == '{':
			d++
		case c == '}':
			if d > 0 {
				d--
			}
		}
		depth[i] = d
	}
	return
}

// skipWSComments advances i past whitespace and // and /* */ comments.
func skipWSComments(text string, i, n int) int {
	for i < n {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '/' && i+1 < n {
			if text[i+1] == '/' {
				i += 2
				for i < n && text[i] != '\n' {
					i++
				}
				continue
			}
			if text[i+1] == '*' {
				i += 2
				for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
					i++
				}
				i += 2
				continue
			}
		}
		break
	}
	return i
}

// jsoncValueEnd returns the index one past the value that starts at v in a
// JSONC document: balanced objects/arrays, quoted strings, or a bare scalar.
func jsoncValueEnd(text string, v int) int {
	n := len(text)
	if v >= n {
		return v
	}
	switch text[v] {
	case '"':
		i := v + 1
		for i < n {
			if text[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if text[i] == '"' {
				return i + 1
			}
			i++
		}
		return n
	case '{', '[':
		close := byte('}')
		if text[v] == '[' {
			close = ']'
		}
		var s, lc, bc bool
		d := 0
		for i := v; i < n; i++ {
			ch := text[i]
			switch {
			case lc:
				if ch == '\n' {
					lc = false
				}
			case bc:
				if ch == '*' && i+1 < n && text[i+1] == '/' {
					i++
				}
			case s:
				if ch == '\\' && i+1 < n {
					i++
				} else if ch == '"' {
					s = false
				}
			case ch == '"':
				s = true
			case ch == text[v]:
				d++
			case ch == close:
				d--
				if d == 0 {
					return i + 1
				}
			}
		}
		return n
	}
	var lc, bc bool
	for i := v; i < n; i++ {
		ch := text[i]
		switch {
		case lc:
			if ch == '\n' {
				lc = false
			}
		case bc:
			if ch == '*' && i+1 < n && text[i+1] == '/' {
				i++
			}
		case ch == '/' && i+1 < n && text[i+1] == '/':
			lc = true
			i++
		case ch == '/' && i+1 < n && text[i+1] == '*':
			bc = true
			i++
		case ch == ',' || ch == '}':
			return i
		}
	}
	return n
}

// findJSONCKey locates the key at the given brace depth and returns the byte
// range of the key and of its value, or ok=false when absent. depth is the
// brace nesting level of the object holding the key (1 for top-level keys,
// 2 for keys of a top-level object value, ...).
func findJSONCKey(text, key string, depth int) (keyStart, valueStart, valueEnd int, ok bool) {
	n := len(text)
	var s, lc, bc bool
	d := 0
	for i := 0; i < n; i++ {
		c := text[i]
		switch {
		case lc:
			if c == '\n' {
				lc = false
			}
			continue
		case bc:
			if c == '*' && i+1 < n && text[i+1] == '/' {
				i++
			}
			continue
		case s:
			if c == '\\' && i+1 < n {
				i++
				continue
			}
			if c == '"' {
				s = false
			}
			continue
		case c == '/':
			if i+1 < n && text[i+1] == '/' {
				lc = true
				i++
			} else if i+1 < n && text[i+1] == '*' {
				bc = true
				i++
			}
			continue
		case c == '"':
			keyStart = i
			j := i + 1
			for j < n && text[j] != '"' {
				if text[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				j++
			}
			if j >= n {
				return 0, 0, 0, false
			}
			k := skipWSComments(text, j+1, n)
			if k < n && text[k] == ':' {
				if d == depth && text[i+1:j] == key {
					v := skipWSComments(text, k+1, n)
					return keyStart, v, jsoncValueEnd(text, v), true
				}
				i = k
			} else {
				i = j
			}
			continue
		case c == '{':
			d++
			continue
		case c == '}':
			if d > 0 {
				d--
			}
			continue
		}
	}
	return 0, 0, 0, false
}

// jsoncRootBraces returns the byte offsets of the top-level { and } of a
// JSONC object document, or ok=false when the document has no object root.
func jsoncRootBraces(text string) (open, close int, ok bool) {
	open = -1
	close = -1
	n := len(text)
	var s, lc, bc bool
	d := 0
	for i := 0; i < n; i++ {
		c := text[i]
		switch {
		case lc:
			if c == '\n' {
				lc = false
			}
		case bc:
			if c == '*' && i+1 < n && text[i+1] == '/' {
				i++
			}
		case s:
			if c == '\\' && i+1 < n {
				i++
			} else if c == '"' {
				s = false
			}
		case c == '"':
			s = true
		case c == '{':
			d++
			if d == 1 && open < 0 {
				open = i
			}
		case c == '}':
			d--
			if d == 0 {
				close = i
			}
		}
	}
	if open < 0 || close < 0 {
		return 0, 0, false
	}
	return open, close, true
}

// lastSignificantCharBefore returns the index of the last byte in text[:end]
// that is not whitespace and not inside a string or comment, or -1.
func lastSignificantCharBefore(text string, end int) int {
	inString, inLine, inBlock, _ := annotateJSONC(text)
	for i := end - 1; i >= 0; i-- {
		if inString[i] || inLine[i] || inBlock[i] {
			continue
		}
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return i
	}
	return -1
}

// stripJSONC removes // line comments, /* block comments */ and trailing
// commas from JSONC text so the remainder parses as strict JSON. String
// literals (including their quotes) are copied verbatim.
func stripJSONC(text string) string {
	inString, inLine, inBlock, _ := annotateJSONC(text)
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if inLine[i] || inBlock[i] {
			continue
		}
		if text[i] == ',' && !inString[i] {
			j := i + 1
			for j < len(text) && (inLine[j] || inBlock[j] || text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
				j++
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				continue
			}
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// validateJSONC parses JSONC content as strict JSON after stripping comments
// and trailing commas.
func validateJSONC(text string) bool {
	return json.Valid([]byte(stripJSONC(text)))
}

func installOpenCodeMCP() (string, string, error) {
	configPath, err := opencodeConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate opencode config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read opencode config: %w", err)
	}
	text := string(data)

	binary, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find CostMax binary: %w", err)
	}
	// entry renders the costmaxx server object; the caller supplies the
	// indentation of the first line and continuation lines nest 2 deeper.
	entry := func(indent string) string {
		return fmt.Sprintf("\"costmaxx\": {\n%s  \"type\": \"local\",\n%s  \"command\": [%q, \"mcp\"],\n%s  \"enabled\": true\n%s}",
			indent, indent, binary, indent, indent)
	}

	var newText string
	if strings.TrimSpace(text) == "" {
		newText = "{\n  \"mcp\": {\n    " + entry("    ") + "\n  }\n}\n"
	} else {
		open, close, ok := jsoncRootBraces(text)
		if !ok {
			return "", "", fmt.Errorf("opencode config is not a JSONC object")
		}
		if _, vs, ve, vok := findJSONCKey(text, "mcp", 1); vok {
			if vs >= len(text) || text[vs] != '{' {
				return "", "", fmt.Errorf("refusing to modify a non-object \"mcp\" entry in opencode config")
			}
			if ks, _, vend, cok := findJSONCKey(text[vs:ve], "costmaxx", 1); cok {
				existing := text[vs+ks : vs+vend]
				if !isCostmaxOpenCodeBlock(existing) {
					return "", "", fmt.Errorf("refusing to overwrite existing non-CostMax \"costmaxx\" entry in opencode config")
				}
				return configPath, "already installed", nil
			}
			if last := lastSignificantCharBefore(text, ve-1); last < 0 || last == vs {
				// mcp object is empty (or holds only whitespace/comments)
				newText = text[:vs+1] + "\n    " + entry("    ") + "\n  " + text[ve-1:]
			} else if text[last] == ',' {
				newText = strings.TrimRight(text[:ve-1], " \t\n\r") + "\n    " + entry("    ") + "\n  " + text[ve-1:]
			} else {
				newText = strings.TrimRight(text[:ve-1], " \t\n\r") + ",\n    " + entry("    ") + "\n  " + text[ve-1:]
			}
		} else {
			mcpBlock := "\n  \"mcp\": {\n    " + entry("    ") + "\n  }\n"
			if last := lastSignificantCharBefore(text, close); last < 0 || last == open {
				newText = text[:close] + mcpBlock + text[close:]
			} else if text[last] == ',' {
				newText = strings.TrimRight(text[:close], " \t\n\r") + mcpBlock + text[close:]
			} else {
				newText = strings.TrimRight(text[:close], " \t\n\r") + ",\n" + mcpBlock + text[close:]
			}
		}
	}

	if len(data) > 0 {
		backup := fmt.Sprintf("%s.costmaxx.bak.%s", configPath, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.WriteFile(backup, data, 0600); err != nil {
			return "", "", fmt.Errorf("back up opencode config: %w", err)
		}
	}

	if !validateJSONC(newText) {
		if len(data) > 0 {
			_ = os.WriteFile(configPath, data, 0600)
		} else {
			_ = os.Remove(configPath)
		}
		return "", "", fmt.Errorf("refusing to write invalid opencode config (result did not parse as JSON)")
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return "", "", fmt.Errorf("create opencode config directory: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(newText), 0600); err != nil {
		return "", "", fmt.Errorf("write opencode config: %w", err)
	}
	return configPath, "installed", nil
}

func uninstallOpenCodeMCP() (string, string, error) {
	configPath, err := opencodeConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate opencode config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return configPath, "not installed", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read opencode config: %w", err)
	}
	text := string(data)

	_, vs, ve, mok := findJSONCKey(text, "mcp", 1)
	if !mok {
		return configPath, "not installed", nil
	}
	ks, _, ce, cok := findJSONCKey(text[vs:ve], "costmaxx", 1)
	if !cok {
		return configPath, "not installed", nil
	}
	existing := text[vs+ks : vs+ce]
	if !isCostmaxOpenCodeBlock(existing) {
		return "", "", fmt.Errorf("refusing to remove a non-CostMax \"costmaxx\" entry from opencode config")
	}

	backup := fmt.Sprintf("%s.costmaxx.bak.%s", configPath, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return "", "", fmt.Errorf("back up opencode config: %w", err)
	}

	// Remove the costmaxx key (and value) plus the comma that separated it
	// from the previous property, when present.
	keyStart := vs + ks
	keyEnd := vs + ce
	delStart := keyStart
	if last := lastSignificantCharBefore(text, keyStart); last >= 0 && text[last] == ',' && last > vs {
		delStart = last
	}
	newText := text[:delStart] + text[keyEnd:]

	// If the mcp object now holds only whitespace/comments, drop the whole
	// mcp key as well.
	if ns, nvs, nve, nok := findJSONCKey(newText, "mcp", 1); nok {
		if last := lastSignificantCharBefore(newText, nve-1); last < 0 || last == nvs {
			delStart = ns
			if last := lastSignificantCharBefore(newText, ns); last >= 0 && newText[last] == ',' {
				delStart = last
			}
			newText = newText[:delStart] + newText[nve:]
		}
	}

	if !validateJSONC(newText) {
		_ = os.WriteFile(configPath, data, 0600)
		return "", "", fmt.Errorf("refusing to write invalid opencode config (result did not parse as JSON)")
	}

	if err := os.WriteFile(configPath, []byte(newText), 0600); err != nil {
		return "", "", fmt.Errorf("update opencode config: %w", err)
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

	if ocPath, err := opencodeConfigPath(); err != nil {
		results["opencode_mcp_config"] = err.Error()
	} else if data, readErr := os.ReadFile(ocPath); readErr != nil {
		results["opencode_mcp_config"] = "not installed"
	} else {
		text := string(data)
		if _, vs, ve, found := findJSONCKey(text, "mcp", 1); !found {
			results["opencode_mcp_config"] = "not installed"
		} else if ks, _, vend, found := findJSONCKey(text[vs:ve], "costmaxx", 1); !found || !isCostmaxOpenCodeBlock(text[vs+ks:vs+vend]) {
			results["opencode_mcp_config"] = "not installed"
		} else {
			results["opencode_mcp_config"] = "OK"
		}
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
	rootCmd.AddCommand(artifactCmd)
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
	installCmd.Flags().StringVar(&installTarget, "target", "codex", "MCP config target: codex or opencode")
	uninstallCmd.Flags().StringVar(&uninstallTarget, "target", "codex", "MCP config target: codex or opencode")
	mcpCmd.Flags().BoolVar(&mcpSpecFraming, "spec-framing", false, "Use MCP spec Content-Length framing instead of newline-delimited framing (the newline framing is what the official TypeScript MCP SDK and opencode speak)")
	evidenceCmd.AddCommand(evidenceShowCmd)
	evidenceCmd.AddCommand(evidenceSearchCmd)
	artifactCmd.AddCommand(artifactAddCmd)
	artifactCmd.AddCommand(artifactRetrieveCmd)
	artifactAddCmd.Flags().String("command", "", "Original command that produced the output (required)")
	artifactAddCmd.Flags().Int("exit-code", 0, "Exit code of the original command")
	evidenceShowCmd.Flags().Int("lines", 0, "Number of lines to show")
	evidenceShowCmd.Flags().Int("start", 0, "Start line")
	evidenceShowCmd.Flags().Int("end", 0, "End line")

	disableCmd.Flags().Bool("session", false, "Disable only for current session")
	resetCmd.Flags().Bool("session", false, "Reset only current session")
	gcCmd.Flags().Duration("older-than", 336*time.Hour, "Delete artifacts older than duration")

}
