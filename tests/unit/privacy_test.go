package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/privacy"
)

func TestRedactAPIKey(t *testing.T) {
	r := privacy.NewRedactor()
	input := "api_key=sk-1234567890abcdef"
	output := r.Redact(input)
	if output != "[REDACTED]" {
		t.Errorf("expected full match redaction, got %q", output)
	}
}

func TestRedactEmail(t *testing.T) {
	r := privacy.NewRedactor()
	input := "contact me at user@example.com"
	output := r.RedactOutput(input)
	if output != "contact me at [REDACTED]" {
		t.Errorf("expected redacted email, got %q", output)
	}
}

func TestRedactJWT(t *testing.T) {
	r := privacy.NewRedactor()
	input := "token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNqP3kQk4xJ_P-1234567890"
	output := r.Redact(input)
	if output != "[REDACTED]" {
		t.Errorf("expected full match redaction, got %q", output)
	}
}

func TestContainsSecrets(t *testing.T) {
	r := privacy.NewRedactor()
	if !r.ContainsSecrets("api_key=secretvalue123") {
		t.Error("expected to detect API key")
	}
	if r.ContainsSecrets("just normal text") {
		t.Error("expected no secrets in normal text")
	}
}

func TestExcludePaths(t *testing.T) {
	r := privacy.NewRedactor()
	r.AddExclude("node_modules")
	if !r.ShouldExclude("/project/node_modules/pkg/file.js") {
		t.Error("expected node_modules to be excluded")
	}
}

func TestRedactOutput(t *testing.T) {
	r := privacy.NewRedactor()
	input := "ip 192.168.1.1 and email test@test.com"
	output := r.RedactOutput(input)
	if output == input {
		t.Error("expected some redaction")
	}
}
