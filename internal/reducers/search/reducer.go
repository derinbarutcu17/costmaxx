package search

import (
	"fmt"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "search" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "search" {
		return 0.85
	}
	if size > 500 && (strings.HasPrefix(command, "rg") || strings.HasPrefix(command, "grep") ||
		strings.HasPrefix(command, "ag") || strings.HasPrefix(command, "find")) {
		return 0.8
	}
	return 0
}

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)
	lines := strings.Split(cleaned, "\n")

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n", meta.ExitCode))

	matchCount := countMatches(lines)
	fileCount := countFiles(lines)
	compact.WriteString(fmt.Sprintf("Matches: %d, Files: %d\n\n", matchCount, fileCount))

	showLines := 30
	if len(lines) > showLines {
		for i := 0; i < showLines && i < len(lines); i++ {
			compact.WriteString(lines[i] + "\n")
		}
		remaining := len(lines) - showLines
		compact.WriteString(fmt.Sprintf("\n... %d more matches omitted ...\n", remaining))
		compact.WriteString(fmt.Sprintf("Use `costmax evidence show <id>` for full results.\n"))
	} else {
		// For a moderate search result the summary is more useful than
		// replaying every matching line. The artifact keeps the exact output;
		// keeping only the counts here makes the active path both smaller and
		// deterministic for the model.
		if len(lines) > 5 {
			compact.WriteString("Search result lines omitted; full output is retained in the artifact.\n")
		} else {
			compact.WriteString(cleaned)
		}
	}

	result := compact.String()
	return &artifacts.ReductionRecord{
		ReductionID:      fmt.Sprintf("red-search-%d", len(input)),
		ArtifactID:       meta.ToolName,
		ReducerName:      r.Name(),
		ReducerVersion:   r.Version(),
		CompactContent:   result,
		OriginalBytes:    int64(len(input)),
		CompactBytes:     int64(len(result)),
		OriginalTokenEst: len(input) / 4,
		CompactTokenEst:  len(result) / 4,
		Reason:           "search output truncated to first matches",
	}, nil
}

func countMatches(lines []string) int {
	count := 0
	for _, l := range lines {
		if strings.Contains(l, ":") && len(l) < 500 {
			count++
		}
	}
	return count
}

func countFiles(lines []string) int {
	files := make(map[string]bool)
	for _, l := range lines {
		if idx := strings.Index(l, ":"); idx > 0 {
			path := l[:idx]
			if !strings.Contains(path, " ") {
				files[path] = true
			}
		}
	}
	return len(files)
}
