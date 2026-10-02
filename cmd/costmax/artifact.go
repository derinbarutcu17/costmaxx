package main

import (
	"fmt"
	"io"

	"github.com/google/uuid"
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
		// recommend, guard, metrics, ledger) so the CLI emits the same envelope
		// as the MCP costmax_run tool for identical inputs. The session id is
		// the stable CLI constant.
		//
		// Call identity: two independent `artifact add` invocations are two
		// real calls and must both count, so the default call ref is a fresh
		// unique id per invocation. Passing --call-ref/--idempotency-key opts
		// into retry deduplication: the same explicit ref re-derives the same
		// ledger idempotency key, and the pipeline replays the stored envelope
		// instead of recording a second call.
		callRef, _ := cmd.Flags().GetString("call-ref")
		if callRef == "" {
			// --idempotency-key is a documented alias for --call-ref.
			callRef, _ = cmd.Flags().GetString("idempotency-key")
		}
		if callRef == "" {
			callRef = uuid.New().String()
		}
		responseText, err := pipeline.Process(pipeline.Deps{
			Store:      artStore,
			DB:         db,
			Classifier: events.NewClassifier(),
			Registry:   reducers.NewRegistry(cfg),
			Redactor:   privacy.NewRedactor(),
			SessionID:  "cli",
			Harness:    "cli",
			CallRef:    callRef,
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
