package tests

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "test" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "test" {
		return 0.9
	}
	if size > 1000 && (strings.Contains(command, "test") || strings.Contains(command, "jest") ||
		strings.Contains(command, "vitest") || strings.Contains(command, "pytest") ||
		strings.Contains(command, "go test") || strings.Contains(command, "cargo test")) {
		return 0.85
	}
	return 0
}

var (
	jestFailRE    = regexp.MustCompile(`(?m)^\s*●\s+(.*)`)
	vitestFailRE  = regexp.MustCompile(`(?m)^\s*❯\s+(.*\.test\.[a-z]+:\d+)`)
	pytestFailRE  = regexp.MustCompile(`(?m)^(?:FAILED|ERROR)\s+(.*\.py:\d+)`)
	goFailRE      = regexp.MustCompile(`(?m)^\s*---\s+FAIL:\s+(.*)`)
	cargoFailRE   = regexp.MustCompile(`(?m)^test\s+.*\.\.\.\s+FAILED`)
	genericFailRE = regexp.MustCompile(`(?m)^FAIL\s+(\S+\.\S+)`)
	summaryRE     = regexp.MustCompile(`(?m)(\d+)\s+(passed|failed|skipped|todo)`)
	totalRE       = regexp.MustCompile(`(?m)^Tests:\s+(\d+)`)
)

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)
	lines := strings.Split(cleaned, "\n")

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n", meta.ExitCode))

	passed, failed, skipped := extractCounts(cleaned)
	compact.WriteString(fmt.Sprintf("Tests: %d passed, %d failed", passed, failed))
	if skipped > 0 {
		compact.WriteString(fmt.Sprintf(", %d skipped", skipped))
	}
	compact.WriteString("\n")

	failing := extractFailingTests(cleaned)
	if len(failing) > 0 {
		compact.WriteString(fmt.Sprintf("\nFailures (%d):\n", len(failing)))
		for _, f := range failing {
			compact.WriteString("- " + f + "\n")
		}
	}

	duration := extractDuration(cleaned, lines)
	if duration > 0 {
		compact.WriteString(fmt.Sprintf("\nDuration: %.1fs\n", duration))
	}

	compact.WriteString(fmt.Sprintf("\nRaw evidence: cmx://artifact/%s\n", meta.ToolName))

	result := compact.String()
	origBytes := int64(len(input))
	compactBytes := int64(len(result))

	return &artifacts.ReductionRecord{
		ReductionID:        fmt.Sprintf("red-test-%d", len(input)),
		ArtifactID:         meta.ToolName,
		ReducerName:        r.Name(),
		ReducerVersion:     r.Version(),
		CompactContent:     result,
		StructuredFacts:    failing,
		OriginalBytes:      origBytes,
		CompactBytes:       compactBytes,
		OriginalTokenEst:   len(input) / 4,
		CompactTokenEst:    len(result) / 4,
		ReplacementApplied: false,
		Reason:             "test output reduced to summary + failures",
	}, nil
}

func extractCounts(s string) (passed, failed, skipped int) {
	matches := summaryRE.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		n := atoi(m[1])
		switch m[2] {
		case "passed":
			passed += n
		case "failed":
			failed += n
		case "skipped":
			skipped += n
		}
	}
	if passed == 0 && failed == 0 {
		totalM := totalRE.FindStringSubmatch(s)
		if len(totalM) > 1 {
			failM := regexp.MustCompile(`(?m)(\d+)\s+fail`).FindStringSubmatch(s)
			if len(failM) > 1 {
				failed = atoi(failM[1])
			}
			passed = atoi(totalM[1]) - failed
		}
	}
	return
}

func extractFailingTests(s string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{jestFailRE, vitestFailRE, pytestFailRE, goFailRE, genericFailRE} {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

func extractDuration(s string, lines []string) float64 {
	re := regexp.MustCompile(`(?:Time|duration|real)\s*[:=]\s*([\d.]+)\s*s`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return atof(m[1])
	}
	return 0
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func atof(s string) float64 {
	var n float64
	fmt.Sscanf(s, "%f", &n)
	return n
}
