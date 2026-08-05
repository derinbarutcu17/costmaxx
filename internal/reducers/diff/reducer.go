package diff

import (
	"fmt"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "diff" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "diff" {
		return 0.8
	}
	if size > 500 && (strings.HasPrefix(command, "git diff") || strings.HasPrefix(command, "diff")) {
		return 0.85
	}
	return 0
}

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)
	lines := strings.Split(cleaned, "\n")

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n\n", meta.ExitCode))

	var files []string
	var adds, removes int
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				before := strings.TrimPrefix(parts[len(parts)-2], "a/")
				after := strings.TrimPrefix(parts[len(parts)-1], "b/")
				files = append(files, before+" -> "+after)
			}
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			adds++
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removes++
		}
	}

	compact.WriteString(fmt.Sprintf("Files changed: %d\n", len(files)))
	compact.WriteString(fmt.Sprintf("Insertions: %d, Deletions: %d\n\n", adds, removes))

	if len(files) > 0 {
		compact.WriteString("Files:\n")
		for _, f := range files {
			compact.WriteString("  " + f + "\n")
		}
	}

	if len(lines) > 100 {
		compact.WriteString(fmt.Sprintf("\nFull diff truncated (%d lines). ", len(lines)))
		compact.WriteString("Use `costmax evidence show <id>` for the complete diff.\n")
	}

	result := compact.String()
	return &artifacts.ReductionRecord{
		ReductionID:      fmt.Sprintf("red-diff-%d", len(input)),
		ArtifactID:       meta.ToolName,
		ReducerName:      r.Name(),
		ReducerVersion:   r.Version(),
		CompactContent:   result,
		PreservedAnchors: files,
		OriginalBytes:    int64(len(input)),
		CompactBytes:     int64(len(result)),
		OriginalTokenEst: len(input) / 4,
		CompactTokenEst:  len(result) / 4,
		Reason:           "diff reduced to file list and stat summary",
	}, nil
}
