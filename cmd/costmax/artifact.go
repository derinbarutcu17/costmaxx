package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/pipeline"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
)

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
		cwd, _ := cmd.Flags().GetString("cwd")

		raw, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}

		// Delegate the shared ingestion chain (redact, store, classify, reduce,
		// recommend, guard, metrics) so the CLI emits the same envelope as the
		// MCP costmax_run tool for identical inputs.
		responseText, err := pipeline.Process(pipeline.Deps{
			Store:      artStore,
			DB:         db,
			Classifier: events.NewClassifier(),
			Registry:   reducers.NewRegistry(cfg),
			Redactor:   privacy.NewRedactor(),
			SessionID:  "cli-" + adapter.SessionID(),
		}, string(raw), command, cwd, exitCode, "cli_artifact_add")
		if err != nil {
			return err
		}

		fmt.Println(responseText)
		return nil
	},
}

var artifactPathCmd = &cobra.Command{
	Use:          "path <artifact-id>",
	SilenceUsage: true,
	Short:        "Print the on-disk storage path of a stored artifact",
	Args:         cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, err := db.GetArtifact(args[0])
		if err != nil {
			return fmt.Errorf("lookup artifact: %w", err)
		}
		if meta == nil {
			return fmt.Errorf("artifact not found: %s", args[0])
		}
		fmt.Println(meta.StoragePath)
		return nil
	},
}

var artifactRetrieveCmd = &cobra.Command{
	Use:          "retrieve <artifact-id>",
	SilenceUsage: true,
	Short:        "Print the full raw content of a stored artifact",
	Args:         cobra.ExactArgs(1),
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
