package integration

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestActivePathVerifier runs the real active-path verifier against the binary
// built in TestMain. The positive direction must pass (exit 0). The negative
// direction corrupts a stored digest and must FAIL with a real non-zero exit
// that names the tampered invariant — not a swallowed internal self-report.
func TestActivePathVerifier(t *testing.T) {
	script := filepath.Join(findModuleRoot(), "scripts", "verify-active-path.sh")

	t.Run("positive", func(t *testing.T) {
		args := []string{script, costmaxBinary}
		out, err := exec.Command("bash", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("verifier failed (exit %v):\n%s", err, out)
		}
		text := string(out)
		if !strings.Contains(text, "PASS:") {
			t.Errorf("verifier did not report PASS:\n%s", text)
		}
	})

	t.Run("negative", func(t *testing.T) {
		args := []string{script, costmaxBinary, "--negative"}
		out, err := exec.Command("bash", args...).CombinedOutput()
		if err == nil {
			t.Fatalf("negative verifier must exit non-zero on tampered evidence, but exited 0:\n%s", out)
		}
		text := string(out)
		if !strings.Contains(text, "FAIL:") || !strings.Contains(text, "digest mismatch") {
			t.Errorf("negative verifier did not fail for the tampered digest:\n%s", text)
		}
		if !strings.Contains(text, "NEGATIVE-DEMO PASS") {
			t.Errorf("negative verifier did not report the tamper demonstration:\n%s", text)
		}
	})
}
