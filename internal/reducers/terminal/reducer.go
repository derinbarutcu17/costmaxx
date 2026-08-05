package terminal

import (
	"fmt"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer { return &Reducer{} }

func (r *Reducer) Name() string    { return "terminal" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "terminal" && size > 1000 {
		return 0.7
	}
	return 0
}

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)
	lines := strings.Split(cleaned, "\n")

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n", meta.ExitCode))

	firstLines := 20
	lastLines := 15
	totalLines := len(lines)

	if totalLines <= firstLines+lastLines+5 {
		compact.WriteString(cleaned)
	} else {
		compact.WriteString(fmt.Sprintf("\n--- First %d lines ---\n", firstLines))
		for i := 0; i < firstLines && i < totalLines; i++ {
			compact.WriteString(lines[i] + "\n")
		}
		compact.WriteString(fmt.Sprintf("\n... %d lines omitted ...\n", totalLines-firstLines-lastLines))
		compact.WriteString(fmt.Sprintf("--- Last %d lines ---\n", lastLines))
		for i := totalLines - lastLines; i < totalLines; i++ {
			compact.WriteString(lines[i] + "\n")
		}
	}

	omitted := [][2]int{}
	if totalLines > firstLines+lastLines+5 {
		omitted = append(omitted, [2]int{firstLines, totalLines - lastLines})
	}

	result := compact.String()
	origBytes := int64(len(input))
	compactBytes := int64(len(result))
	origTokens := len(input) / 4
	compactTokens := len(result) / 4

	return &artifacts.ReductionRecord{
		ReductionID:        fmt.Sprintf("red-term-%d", len(input)),
		ArtifactID:         meta.ToolName,
		ReducerName:        r.Name(),
		ReducerVersion:     r.Version(),
		CompactContent:     result,
		PreservedAnchors:   []string{"command", "exit_code"},
		OmittedLineRanges:  omitted,
		OriginalBytes:      origBytes,
		CompactBytes:       compactBytes,
		OriginalTokenEst:   origTokens,
		CompactTokenEst:    compactTokens,
		ReplacementApplied: false,
		Reason:             "terminal output truncated to first/last lines",
	}, nil
}
