package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var savingsCmd = &cobra.Command{
	Use:   "savings",
	Short: "Aggregate CostMax savings from the ledger over a --since window (default 168h; --since 0 = all history)",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, _ := cmd.Flags().GetDuration("since")
		var cutoff time.Time
		if since <= 0 {
			// A non-positive window means all history.
			cutoff = time.Time{}
		} else {
			cutoff = time.Now().Add(-since)
		}

		// Savings derive from the immutable per-call ledger keyed by actual
		// event timestamp, never from the cumulative session_metrics table.
		sum, err := db.LedgerSummary(cutoff)
		if err != nil {
			return fmt.Errorf("savings: %w", err)
		}

		window := "all history"
		if !cutoff.IsZero() {
			window = "last " + since.String()
		}

		fmt.Printf("CostMax savings (%s)\n", window)
		fmt.Printf("Calls processed:           %d\n", sum.CallsProcessed)
		fmt.Printf("Artifacts stored:          %d\n", sum.ArtifactsStored)
		fmt.Printf("Reductions attempted:      %d\n", sum.ReductionsAttempted)
		fmt.Printf("Reductions applied:        %d\n", sum.ReductionsApplied)
		fmt.Printf("Pass-through (no saving):  %d\n", sum.Passthroughs)
		fmt.Printf("Guard downgrades:          %d\n", sum.GuardDowngrades)
		fmt.Printf("Preserve-full decisions:   %d\n", sum.Preserved)
		fmt.Printf("Rehydrations:              %d\n", sum.Rehydrations)
		fmt.Printf("Errors/fail-open:          %d\n", sum.Errors)
		fmt.Printf("Raw input tokens (est):    %d\n", sum.RawTokens)
		fmt.Printf("Model-visible tokens (est): %d\n", sum.ModelVisibleTokens)
		fmt.Printf("Tokens saved (est):        %d (%.1f%%)\n", sum.SavedTokens(), sum.ReductionRate()*100)
		fmt.Printf("Raw bytes:                 %d\n", sum.RawBytes)
		fmt.Printf("Model-visible bytes:       %d\n", sum.ModelVisibleBytes)
		fmt.Printf("Bytes dropped (stored, retrievable): %d\n", sum.RawBytes-sum.ModelVisibleBytes)
		return nil
	},
}
