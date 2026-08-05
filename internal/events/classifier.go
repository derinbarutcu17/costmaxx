package events

import (
	"path/filepath"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Classifier struct{}

func NewClassifier() *Classifier { return &Classifier{} }

func (c *Classifier) Classify(toolName, command, output string, exitCode int, size int64) OutputCategory {
	if size == 0 {
		return OutputGeneric
	}

	if shared.IsBinary([]byte(output)) {
		return OutputBinary
	}

	if cat := c.classifyByTool(toolName, command); cat != "" {
		return cat
	}

	if cat := c.classifyBySignature(output); cat != "" {
		return cat
	}

	return OutputTerminal
}

func (c *Classifier) classifyByTool(tool, command string) OutputCategory {
	name := strings.ToLower(filepath.Base(strings.Fields(tool)[0]))
	cmd := strings.ToLower(command)

	if strings.Contains(cmd, "test") || strings.Contains(cmd, "jest ") || strings.Contains(cmd, "vitest ") || strings.Contains(cmd, "pytest ") || strings.Contains(cmd, "go test ") || strings.Contains(cmd, "cargo test") || strings.Contains(cmd, "mocha ") || strings.Contains(cmd, "rspec ") {
		return OutputTest
	}
	if strings.Contains(cmd, "build") || strings.Contains(cmd, "tsc ") || strings.Contains(cmd, "cargo build") || strings.Contains(cmd, "go build") || strings.Contains(cmd, "make ") || strings.Contains(cmd, "compile") {
		return OutputBuild
	}
	if strings.HasPrefix(cmd, "git diff") || strings.HasPrefix(cmd, "diff ") {
		return OutputDiff
	}
	if strings.HasPrefix(cmd, "rg ") || strings.HasPrefix(cmd, "grep ") || strings.HasPrefix(cmd, "ag ") || strings.HasPrefix(cmd, "find ") && strings.Contains(cmd, "name") {
		return OutputSearch
	}
	if strings.Contains(cmd, "eslint ") || strings.Contains(cmd, "tslint ") || strings.Contains(cmd, "ruff ") || strings.Contains(cmd, "flake8 ") || strings.Contains(cmd, "golangci") || strings.Contains(cmd, "clippy ") {
		return OutputLint
	}
	if strings.Contains(name, "test") || strings.Contains(cmd, "test") {
		return OutputTest
	}

	return ""
}

func (c *Classifier) classifyBySignature(output string) OutputCategory {
	lower := strings.ToLower(output)

	testSignals := 0
	for _, s := range []string{"tests:", "✓ ", "✗ ", "● ", "passed", "failed", "test suite", "test file", "expect(", "assert."} {
		if strings.Contains(lower, s) {
			testSignals++
		}
	}
	if testSignals >= 3 {
		return OutputTest
	}

	buildSignals := 0
	for _, s := range []string{"error[", "warning[", "compiling ", "error:"} {
		if strings.Contains(lower, s) {
			buildSignals++
		}
	}
	if buildSignals >= 2 && (strings.Contains(lower, ".rs:") || strings.Contains(lower, ".go:") || strings.Contains(lower, ".ts:")) {
		return OutputBuild
	}

	if strings.HasPrefix(output, "{") || strings.HasPrefix(output, "[") {
		return OutputJSON
	}

	return ""
}
