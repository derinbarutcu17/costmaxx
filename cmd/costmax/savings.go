package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var savingsCmd = &cobra.Command{
	Use:   "savings",
	Short: "Aggregate CostMax savings over a time window (default: last 7 days)",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, _ := cmd.Flags().GetDuration("since")
		if since <= 0 {
			since = 7 * 24 * time.Hour
		}
		cutoff := time.Now().Add(-since)

		sum, err := db.SavingsSummary(cutoff)
		if err != nil {
			return fmt.Errorf("savings: %w", err)
		}

		saved := sum.RawTokens - sum.ModelVisible
		pct := 0.0
		if sum.RawTokens > 0 {
			pct = float64(saved) / float64(sum.RawTokens) * 100
		}

		fmt.Printf("CostMax savings (last %s)\n", since.String())
		fmt.Printf("Sessions recorded:      %d\n", sum.Sessions)
		fmt.Printf("Tool calls captured:    %d\n", sum.ToolCalls)
		fmt.Printf("Artifacts stored:       %d\n", sum.ArtifactsStored)
		fmt.Printf("Reductions applied:     %d\n", sum.ReductionsApplied)
		fmt.Printf("Raw input tokens:       %d\n", sum.RawTokens)
		fmt.Printf("Model-visible tokens:   %d\n", sum.ModelVisible)
		fmt.Printf("Tokens saved:           %d (%.1f%%)\n", saved, pct)
		fmt.Printf("Evidence bytes dropped: %d (stored, retrievable)\n", sum.BytesDropped)
		return nil
	},
}
