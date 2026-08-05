package lint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "lint" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "lint" {
		return 0.9
	}
	if size > 500 && (strings.Contains(command, "eslint") || strings.Contains(command, "tslint") ||
		strings.Contains(command, "ruff") || strings.Contains(command, "flake8") ||
		strings.Contains(command, "golangci") || strings.Contains(command, "clippy") ||
		strings.Contains(command, "prettier") || strings.Contains(command, "check")) {
		return 0.85
	}
	return 0
}

var (
	lintErrorRE   = regexp.MustCompile(`(?m)^.*:\d+:\d+.*(?:error|Error|ERROR).*`)
	lintWarnRE    = regexp.MustCompile(`(?m)^.*:\d+:\d+.*(?:warning|WARNING).*`)
	lintProbCount = regexp.MustCompile(`(\d+)\s+(problem|error|warning|violation)`)
)

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n\n", meta.ExitCode))

	problems := lintErrorRE.FindAllString(cleaned, -1)
	warnings := lintWarnRE.FindAllString(cleaned, -1)

	for _, m := range lintProbCount.FindAllStringSubmatch(cleaned, -1) {
		compact.WriteString(fmt.Sprintf("%s %s\n", m[1], m[2]))
	}

	compact.WriteString(fmt.Sprintf("Errors: %d, Warnings: %d\n\n", len(problems), len(warnings)))

	allProblems := problems
	if len(warnings) > 0 {
		allProblems = append(allProblems, warnings...)
	}

	if len(allProblems) > 0 {
		show := min(30, len(allProblems))
		compact.WriteString(fmt.Sprintf("Issues (showing %d of %d):\n", show, len(allProblems)))
		for i := 0; i < show; i++ {
			compact.WriteString("  " + allProblems[i] + "\n")
		}
		if len(allProblems) > show {
			compact.WriteString(fmt.Sprintf("  ... %d more issues\n", len(allProblems)-show))
		}
	}

	result := compact.String()
	return &artifacts.ReductionRecord{
		ReductionID:      fmt.Sprintf("red-lint-%d", len(input)),
		ArtifactID:       meta.ToolName,
		ReducerName:      r.Name(),
		ReducerVersion:   r.Version(),
		CompactContent:   result,
		OriginalBytes:    int64(len(input)),
		CompactBytes:     int64(len(result)),
		OriginalTokenEst: len(input) / 4,
		CompactTokenEst:  len(result) / 4,
		Reason:           "lint output reduced to problem count + first issues",
	}, nil
}
