package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
)

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

func init() {
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(artifactCmd)
	rootCmd.AddCommand(replayCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stateCmd)
	rootCmd.AddCommand(evidenceCmd)
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(modeCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(gcCmd)
	rootCmd.AddCommand(savingsCmd)
	rootCmd.AddCommand(hookCmd)
	rootCmd.AddCommand(mcpCmd)
	installCmd.Flags().StringVar(&installTarget, "target", "codex", "MCP config target: codex or opencode")
	uninstallCmd.Flags().StringVar(&uninstallTarget, "target", "codex", "MCP config target: codex or opencode")
	mcpCmd.Flags().BoolVar(&mcpSpecFraming, "spec-framing", false, "Use MCP spec Content-Length framing instead of newline-delimited framing (the newline framing is what the official TypeScript MCP SDK and opencode speak)")
	evidenceCmd.AddCommand(evidenceShowCmd)
	evidenceCmd.AddCommand(evidenceSearchCmd)
	artifactCmd.AddCommand(artifactAddCmd)
	artifactCmd.AddCommand(artifactRetrieveCmd)
	artifactCmd.AddCommand(artifactPathCmd)
	artifactAddCmd.Flags().String("command", "", "Shell command that produced the output (required)")
	artifactAddCmd.Flags().Int("exit-code", 0, "Exit code of the command")
	artifactAddCmd.Flags().String("cwd", "", "Working directory the command ran in")
	evidenceShowCmd.Flags().Int("lines", 0, "Number of lines to show")
	evidenceShowCmd.Flags().Int("start", 0, "Start line")
	evidenceShowCmd.Flags().Int("end", 0, "End line")

	disableCmd.Flags().Bool("session", false, "Disable only for current session")
	resetCmd.Flags().Bool("session", false, "Reset only current session")
	gcCmd.Flags().Duration("older-than", 336*time.Hour, "Delete artifacts older than duration")
	savingsCmd.Flags().Duration("since", 7*24*time.Hour, "Report window (default 168h = 7 days)")

}
