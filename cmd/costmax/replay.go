package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

var replayCmd = &cobra.Command{
	Use:   "replay <artifact-id>",
	Short: "Re-run the stored command of an artifact and print its output",
	Args:  cobra.ExactArgs(1),
	// Runtime failures (missing cwd, dead command) are not usage errors; do
	// not dump the full help text at the user for them.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, err := db.GetArtifact(args[0])
		if err != nil {
			return fmt.Errorf("lookup artifact: %w", err)
		}
		if meta == nil {
			return fmt.Errorf("artifact not found: %s", args[0])
		}
		if meta.Command == "" {
			return fmt.Errorf("artifact has no stored command: %s", args[0])
		}

		run := exec.Command("sh", "-c", meta.Command)
		if meta.Cwd != "" {
			run.Dir = meta.Cwd
		}
		run.Stdout = os.Stdout
		run.Stderr = os.Stderr
		if err := run.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					code = 128 + int(ws.Signal())
				}
				os.Exit(code)
			}
			return fmt.Errorf("replay exec: %w", err)
		}
		return nil
	},
}
