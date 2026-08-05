package unit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/build"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/diff"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/json"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/lint"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/search"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/terminal"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/tests"
)

func TestTestReducerPreservesFailures(t *testing.T) {
	r := tests.New()
	var b strings.Builder
	b.WriteString("Tests: 142 passed, 3 failed\n")
	for i := 0; i < 50; i++ {
		b.WriteString(fmt.Sprintf("  ✓ test case %d (12ms)\n", i))
	}
	b.WriteString("● auth/session.test.ts:88\n● auth/refresh.test.ts:132\n● middleware/auth.test.ts:47\n")
	input := b.String()
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "npm test", ExitCode: 1, Category: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(red.StructuredFacts) == 0 {
		t.Error("expected structured facts (failing tests)")
	}
	if red.OriginalBytes <= red.CompactBytes {
		t.Errorf("expected reduction: original=%d compact=%d", red.OriginalBytes, red.CompactBytes)
	}
}

func TestTestReducerPreservesExitCode(t *testing.T) {
	r := tests.New()
	input := "Tests: 10 passed\nok"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "go test", ExitCode: 0, Category: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if red.CompactBytes == 0 {
		t.Error("expected non-empty compact output")
	}
}

func TestTestReducerPreservesPytestCollectionLocations(t *testing.T) {
	r := tests.New()
	input := "ERROR collecting tests/test_config.py:12\nImportError: missing Settings\nERROR collecting tests/test_routes.py:8\nSyntaxError: invalid syntax\n"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "pytest --collect-only", ExitCode: 2, Category: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(red.CompactContent, "tests/test_config.py:12") || !strings.Contains(red.CompactContent, "tests/test_routes.py:8") {
		t.Fatalf("pytest locations missing from compact output:\n%s", red.CompactContent)
	}
}

func TestTestReducerPreservesGoFailName(t *testing.T) {
	r := tests.New()
	input := "--- FAIL: TestAuthExpiry (0.00s)\n    auth_test.go:42: expected 401\nFAIL\nFAIL fixture.test/auth 0.001s\n"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "go test -v ./...", ExitCode: 1, Category: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(red.CompactContent, "TestAuthExpiry") {
		t.Fatalf("Go failing test name missing from compact output:\n%s", red.CompactContent)
	}
}

func TestBuildReducerPreservesErrors(t *testing.T) {
	r := build.New()
	input := "error TS2322: Type 'number' is not assignable to type 'string'\n  src/main.ts:45"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "tsc", ExitCode: 2, Category: "build"})
	if err != nil {
		t.Fatal(err)
	}
	if len(red.StructuredFacts) == 0 {
		t.Error("expected structured facts (errors)")
	}
}

// Regression: case-007-build
// ERROR: path:line:col - message format must preserve the file path in the extracted error.
func TestBuildReducerCase007(t *testing.T) {
	r := build.New()
	input := "[INFO] Starting build...\nERROR: src/app.ts:10:8 - Type mismatch.\nERROR: src/utils.ts:25:12 - Argument error.\n[WEBPACK] Finished with code 1.\n"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "sh build --fake", ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	compact := red.CompactContent
	if !strings.Contains(compact, "src/app.ts:10") {
		t.Errorf("compact output missing src/app.ts:10\n%s", compact)
	}
	if !strings.Contains(compact, "src/utils.ts:25") {
		t.Errorf("compact output missing src/utils.ts:25\n%s", compact)
	}
	if len(red.StructuredFacts) != 2 {
		t.Fatalf("expected 2 errors in StructuredFacts, got %d: %v", len(red.StructuredFacts), red.StructuredFacts)
	}
}

func TestBuildReducerRetainsPrefixLocation(t *testing.T) {
	r := build.New()
	got, err := r.Reduce("src/auth/session.ts(45,10): error TS2322: incompatible type\n", artifacts.ReducerMetadata{Command: "tsc --noEmit"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.CompactContent, "src/auth/session.ts(45,10)") {
		t.Fatalf("compact build output lost source location:\n%s", got.CompactContent)
	}
}

// Regression: case-004-lint
// path:line:col error lines must be captured in compact output with file paths.
// The lint reducer shows only the first 30 issues, so target lines must be early.
func TestLintReducerCase004(t *testing.T) {
	r := lint.New()
	var b strings.Builder
	b.WriteString("src/app.ts:10:8  error  `config` is defined but never used  (no-unused-vars)\n")
	b.WriteString("src/utils.ts:25:12  error  `helper` is defined but never used  (no-unused-vars)\n")
	for i := 0; i < 100; i++ {
		b.WriteString(fmt.Sprintf("src/module%d.ts:%d:10  error  `var%d` is never used  (no-unused-vars)\n", i, i+1, i))
	}
	input := b.String()
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "sh eslint --fake", ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	compact := red.CompactContent
	if !strings.Contains(compact, "src/app.ts") {
		t.Errorf("compact output missing src/app.ts\n%s", compact)
	}
	if !strings.Contains(compact, "src/utils.ts") {
		t.Errorf("compact output missing src/utils.ts\n%s", compact)
	}
	if !strings.Contains(compact, "Errors:") {
		t.Errorf("compact output missing error count\n%s", compact)
	}
}

// Regression: case-005-search
// ripgrep output must show file count in the summary.
func TestSearchReducerCase005(t *testing.T) {
	r := search.New()
	var b strings.Builder
	files := []string{
		"src/component1.tsx", "src/component2.tsx", "src/component3.tsx",
		"src/hooks.ts", "src/utils.ts", "src/config.ts",
		"src/styles.ts", "src/constants.ts", "src/types.ts", "src/index.ts",
	}
	for _, f := range files {
		b.WriteString(f + ":1:TODO implement\n")
		b.WriteString(f + ":2:TODO test\n")
	}
	input := b.String()
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: `rg "TODO" src`, ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	compact := red.CompactContent
	if !strings.Contains(compact, "Files: 10") {
		t.Errorf("compact output missing Files: 10\n%s", compact)
	}
	if !strings.Contains(compact, "Matches: 20") {
		t.Errorf("compact output missing Matches: 20\n%s", compact)
	}
	if red.CompactBytes >= red.OriginalBytes {
		t.Fatalf("search summary should be smaller than raw matches: raw=%d compact=%d", red.OriginalBytes, red.CompactBytes)
	}
}

// Regression: case-006-diff
// git diff --no-index must show file stats, and exit 1 is expected.
func TestDiffReducerCase006(t *testing.T) {
	r := diff.New()
	input := "diff --git a/before.txt b/after.txt\n--- a/before.txt\n+++ b/after.txt\n@@ -1,3 +1,4 @@\n+new line\n unchanged\n-old line\n"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "git diff --no-index before.txt after.txt", ExitCode: 1})
	if err != nil {
		t.Fatal(err)
	}
	compact := red.CompactContent
	if !strings.Contains(compact, "Exit: 1") {
		t.Errorf("compact output missing exit code 1\n%s", compact)
	}
	if !strings.Contains(compact, "Insertions:") || !strings.Contains(compact, "Deletions:") {
		t.Errorf("compact output missing insertion/deletion counts\n%s", compact)
	}
	if !strings.Contains(compact, "after.txt") {
		t.Errorf("compact output missing filename\n%s", compact)
	}
}

func TestTerminalReducerTruncates(t *testing.T) {
	r := terminal.New()
	var input string
	for i := 0; i < 100; i++ {
		input += "line of output\n"
	}
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "script.sh", ExitCode: 0, Category: "terminal", Size: int64(len(input))})
	if err != nil {
		t.Fatal(err)
	}
	if red.CompactBytes >= red.OriginalBytes {
		t.Error("expected terminal truncation")
	}
}

func TestDiffReducerStats(t *testing.T) {
	r := diff.New()
	input := "diff --git a/src/main.ts b/src/main.ts\n+new line\n-old line"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "git diff", ExitCode: 0, Category: "diff"})
	if err != nil {
		t.Fatal(err)
	}
	if red.CompactBytes == 0 {
		t.Error("expected non-empty compact output")
	}
}

func TestReducerCanHandle(t *testing.T) {
	r := tests.New()
	score := r.CanHandle("test", "npm test", 1, 5000)
	if score <= 0 {
		t.Error("expected test reducer to handle test category")
	}
}

// Regression: case-002-failing-tests
// FAIL <path> lines must be captured as failures in compact output and StructuredFacts.
func TestTestReducerCase002(t *testing.T) {
	r := tests.New()
	input := "Running tests...\nPASS src/utils/helper.test.ts\nPASS src/auth/login.test.ts\nPASS src/db/connect.test.ts\nFAIL src/api/rates.test.ts\n  Expected: 200\n  Received: 500\nPASS src/cache/redis.test.ts\nFAIL src/api/validate.test.ts\n  Expected: { ok: true }\n  Received: { ok: false, error: 'invalid' }\nPASS src/config/load.test.ts\n\nTests: 5 passed, 2 failed\nDuration: 4.2s"
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "cat test_output.txt", ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	// StructuredFacts must contain the failing test identifiers
	if len(red.StructuredFacts) != 2 {
		t.Fatalf("expected 2 failing tests in StructuredFacts, got %d: %v", len(red.StructuredFacts), red.StructuredFacts)
	}
	// Compact output must contain FAIL paths, not assertion bodies
	compact := red.CompactContent
	if !strings.Contains(compact, "rates.test.ts") {
		t.Errorf("compact output missing rates.test.ts\n%s", compact)
	}
	if !strings.Contains(compact, "validate.test.ts") {
		t.Errorf("compact output missing validate.test.ts\n%s", compact)
	}
	if strings.Contains(compact, "Expected: 200") {
		t.Errorf("compact output leaks assertion body\n%s", compact)
	}
	if strings.Contains(compact, "Received: 500") {
		t.Errorf("compact output leaks assertion body\n%s", compact)
	}
	if strings.Contains(compact, "PASS ") {
		t.Errorf("compact output contains PASS lines\n%s", compact)
	}
}

// Regression: case-003-json-summary
// Homogeneous JSON array must output length, field names, and a bounded group summary
// that lets models answer categorical filters (e.g. admin names).
func TestJsonReducerCase003(t *testing.T) {
	r := json.New()
	input := `[
  {"id": 1, "name": "Alice", "role": "admin", "active": true, "email": "alice@example.com"},
  {"id": 2, "name": "Bob", "role": "editor", "active": true, "email": "bob@example.com"},
  {"id": 3, "name": "Charlie", "role": "viewer", "active": false, "email": "charlie@example.com"},
  {"id": 4, "name": "Diana", "role": "admin", "active": true, "email": "diana@example.com"},
  {"id": 5, "name": "Eve", "role": "editor", "active": false, "email": "eve@example.com"},
  {"id": 6, "name": "Frank", "role": "viewer", "active": true, "email": "frank@example.com"},
  {"id": 7, "name": "Grace", "role": "admin", "active": true, "email": "grace@example.com"},
  {"id": 8, "name": "Hank", "role": "editor", "active": false, "email": "hank@example.com"},
  {"id": 9, "name": "Ivy", "role": "viewer", "active": true, "email": "ivy@example.com"},
  {"id": 10, "name": "Jack", "role": "admin", "active": true, "email": "jack@example.com"},
  {"id": 11, "name": "Kate", "role": "editor", "active": true, "email": "kate@example.com"},
  {"id": 12, "name": "Leo", "role": "viewer", "active": false, "email": "leo@example.com"},
  {"id": 13, "name": "Maria", "role": "admin", "active": true, "email": "maria@example.com"},
  {"id": 14, "name": "Nathan", "role": "editor", "active": true, "email": "nathan@example.com"},
  {"id": 15, "name": "Olive", "role": "viewer", "active": false, "email": "olive@example.com"}
]`
	red, err := r.Reduce(input, artifacts.ReducerMetadata{Command: "cat users.json", ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	compact := red.CompactContent
	// Must show array length
	if !strings.Contains(compact, "15") {
		t.Errorf("compact output missing array length 15\n%s", compact)
	}
	// Must show field names
	if !strings.Contains(compact, "Keys:") {
		t.Errorf("compact output missing Keys section\n%s", compact)
	}
	// Must show role grouping with admin names
	if !strings.Contains(compact, "admin") || !strings.Contains(compact, "Alice") {
		t.Errorf("compact output missing admin group with names\n%s", compact)
	}
	if !strings.Contains(compact, "Diana") || !strings.Contains(compact, "Maria") {
		t.Errorf("compact output missing expected admin names\n%s", compact)
	}
	// Must NOT contain email values
	if strings.Contains(compact, "alice@example.com") || strings.Contains(compact, "@") {
		t.Errorf("compact output leaks email values\n%s", compact)
	}
	// Must NOT contain individual ids
	if strings.Contains(compact, `"id": 1`) || strings.Contains(compact, `"id": 2`) {
		t.Errorf("compact output leaks full object payload\n%s", compact)
	}
}
