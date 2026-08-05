package generic

import (
	"fmt"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "generic" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "generic" && size > 2000 {
		return 0.6
	}
	if size > 4000 {
		return 0.5
	}
	return 0
}

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)
	lines := strings.Split(cleaned, "\n")

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n", meta.ExitCode))
	compact.WriteString(fmt.Sprintf("Lines: %d, Chars: %d\n\n", len(lines), len(cleaned)))

	first := 15
	last := 10
	if len(lines) <= first+last+5 {
		compact.WriteString(cleaned)
	} else {
		compact.WriteString(fmt.Sprintf("--- First %d lines ---\n", first))
		for i := 0; i < first; i++ {
			compact.WriteString(lines[i] + "\n")
		}
		omitted := len(lines) - first - last
		compact.WriteString(fmt.Sprintf("\n... %d lines omitted ...\n\n", omitted))
		compact.WriteString(fmt.Sprintf("--- Last %d lines ---\n", last))
		for i := len(lines) - last; i < len(lines); i++ {
			compact.WriteString(lines[i] + "\n")
		}
	}

	compact.WriteString(fmt.Sprintf("\n---\nRaw evidence: cmx://artifact/%s\n", meta.ToolName))

	result := compact.String()
	return &artifacts.ReductionRecord{
		ReductionID:      fmt.Sprintf("red-gen-%d", len(input)),
		ArtifactID:       meta.ToolName,
		ReducerName:      r.Name(),
		ReducerVersion:   r.Version(),
		CompactContent:   result,
		OriginalBytes:    int64(len(input)),
		CompactBytes:     int64(len(result)),
		OriginalTokenEst: len(input) / 4,
		CompactTokenEst:  len(result) / 4,
		Reason:           "generic output truncated to first/last lines",
	}, nil
}
