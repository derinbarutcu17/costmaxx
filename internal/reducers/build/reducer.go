package build

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "build" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "build" {
		return 0.85
	}
	if size > 2000 && (strings.Contains(command, "build") || strings.Contains(command, "compile") ||
		strings.Contains(command, "tsc") || strings.Contains(command, "cargo build") ||
		strings.Contains(command, "go build") || strings.Contains(command, "make")) {
		return 0.8
	}
	return 0
}

var (
	errorRE   = regexp.MustCompile(`(?m)^.*((?:error|Error|ERROR)[^:]*:\s*.*)`)
	warnRE    = regexp.MustCompile(`(?m)^.*(?:warning|WARNING)[^:]*:\s*.*`)
	summaryRE = regexp.MustCompile(`(?i)(error|warning|failure)s?:\s*(\d+)`)
)

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)
	lines := strings.Split(cleaned, "\n")

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n\n", meta.ExitCode))

	// Keep the complete diagnostic line. shared.ExtractErrors intentionally
	// starts at "error", which drops TypeScript/Rust locations before it.
	errors := errorRE.FindAllString(cleaned, -1)
	warnings := warnRE.FindAllString(cleaned, -1)

	errCount, warnCount := 0, 0
	if m := summaryRE.FindStringSubmatch(cleaned); len(m) > 0 {
		word := strings.ToLower(m[1])
		n := atoi(m[2])
		if word == "error" || word == "failure" {
			errCount = n
		} else {
			warnCount = n
		}
	}

	if errCount == 0 {
		errCount = len(errors)
	}
	if warnCount == 0 {
		warnCount = len(warnings)
	}

	compact.WriteString(fmt.Sprintf("Errors: %d, Warnings: %d\n", errCount, warnCount))

	if len(errors) > 0 {
		compact.WriteString(fmt.Sprintf("\nErrors (%d):\n", len(errors)))
		for i, e := range errors {
			if i >= 20 {
				compact.WriteString(fmt.Sprintf("... and %d more\n", len(errors)-20))
				break
			}
			compact.WriteString("  " + e + "\n")
		}
	}

	if len(lines) > 50 {
		compact.WriteString(fmt.Sprintf("\n--- Last %d lines of build output ---\n", 20))
		for _, l := range lines[max(0, len(lines)-20):] {
			compact.WriteString(l + "\n")
		}
	}

	result := compact.String()
	return &artifacts.ReductionRecord{
		ReductionID:        fmt.Sprintf("red-build-%d", len(input)),
		ArtifactID:         meta.ToolName,
		ReducerName:        r.Name(),
		ReducerVersion:     r.Version(),
		CompactContent:     result,
		StructuredFacts:    errors,
		OriginalBytes:      int64(len(input)),
		CompactBytes:       int64(len(result)),
		OriginalTokenEst:   len(input) / 4,
		CompactTokenEst:    len(result) / 4,
		ReplacementApplied: false,
		Reason:             "build output reduced to error/warning summary",
	}, nil
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
