package events

import (
	"path/filepath"
	"strings"
	"unicode"

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

// commandTokens splits a shell command into word tokens. Matching on exact
// tokens (instead of substrings) removes the false positives where a path like
// /tmp/test-data or a file like build-notes.md contained a keyword.
func commandTokens(cmd string) []string {
	return strings.FieldsFunc(cmd, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(";&|<>()\"'`", r)
	})
}

func (c *Classifier) classifyByTool(tool, command string) OutputCategory {
	fields := strings.Fields(tool)
	name := ""
	if len(fields) > 0 {
		// Malformed tool names (empty or whitespace-only) have no binary to
		// derive a category from; the command token matcher below still runs,
		// so classification stays correct from the command alone.
		name = strings.ToLower(filepath.Base(fields[0]))
	}
	cmd := strings.ToLower(command)
	tokens := commandTokens(cmd)
	has := func(names ...string) bool {
		for _, t := range tokens {
			for _, n := range names {
				if t == n {
					return true
				}
			}
		}
		return false
	}
	hasPrefix := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(cmd, p) {
				return true
			}
		}
		return false
	}

	if has("test", "jest", "vitest", "mocha", "rspec", "pytest") || hasPrefix("go test ", "cargo test") || strings.Contains(name, "test") {
		return OutputTest
	}
	if has("build", "tsc", "make", "compile", "cmake", "ninja", "gradle") || hasPrefix("go build", "cargo build") || strings.Contains(name, "build") {
		return OutputBuild
	}
	if hasPrefix("git diff", "diff ") {
		return OutputDiff
	}
	if has("rg", "grep", "ag", "find", "ack", "ripgrep") {
		return OutputSearch
	}
	if has("eslint", "tslint", "ruff", "flake8", "golangci", "golangci-lint", "clippy", "shellcheck", "hadolint") {
		return OutputLint
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

	// JSON signatures may carry leading whitespace (pretty-printed streams,
	// command wrappers); ignore it before deciding the shape.
	trimmed := strings.TrimLeft(output, " \t\r\n")
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return OutputJSON
	}

	return ""
}
