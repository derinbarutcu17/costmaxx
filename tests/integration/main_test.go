package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var costmaxBinary string

func TestMain(m *testing.M) {
	// Build the binary from current source at test time
	dir, err := os.MkdirTemp("", "costmax-test-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	binary := filepath.Join(dir, "costmaxx-test")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "./cmd/costmax/")
	cmd.Dir = findModuleRoot()
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build test binary: %v\n", err)
		os.Exit(1)
	}

	costmaxBinary = binary
	os.Setenv("COSTMAX_BINARY", binary)

	os.Exit(m.Run())
}

// findModuleRoot walks up from the test file to find go.mod
func findModuleRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
