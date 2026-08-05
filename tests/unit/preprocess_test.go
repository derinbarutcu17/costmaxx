package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

func TestStripANSI(t *testing.T) {
	input := "\x1b[31mred\x1b[0m normal"
	output := shared.StripANSI(input)
	if output != "red normal" {
		t.Errorf("expected 'red normal', got %q", output)
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	input := "line1\r\nline2\rline3\n"
	output := shared.NormalizeLineEndings(input)
	expected := "line1\nline2\nline3\n"
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestCollapseDuplicateLines(t *testing.T) {
	input := "a\na\nb\nb\nb\nc"
	output := shared.CollapseDuplicateLines(input)
	expected := "a\nb\nc"
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestIsBinary(t *testing.T) {
	if shared.IsBinary([]byte("hello")) {
		t.Error("expected text to not be binary")
	}
	if !shared.IsBinary([]byte("he\x00llo")) {
		t.Error("expected null byte to be binary")
	}
}

func TestExtractPaths(t *testing.T) {
	input := "Error in src/main.ts:45:10 and src/utils.ts:12"
	paths := shared.ExtractPaths(input)
	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d: %v", len(paths), paths)
	}
}

func TestExtractErrors(t *testing.T) {
	input := "Error: something broke\nFailed: test failed\nJust a line"
	errors := shared.ExtractErrors(input)
	if len(errors) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(errors), errors)
	}
}

func TestPrintables(t *testing.T) {
	input := "hello\x00world\x01test"
	output := shared.Printables(input)
	if output != "helloworldtest" {
		t.Errorf("expected printable only, got %q", output)
	}
}
