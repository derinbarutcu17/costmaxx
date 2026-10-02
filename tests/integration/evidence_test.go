package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestEvidenceShowHintPointsAtArtifactRetrieve proves the `evidence show`
// placeholder guides the user to the implemented retrieval command. The hint
// must name `costmax artifact retrieve`, never a command that does not exist.
func TestEvidenceShowHintPointsAtArtifactRetrieve(t *testing.T) {
	home := newIsolatedHome(t)
	cmd := exec.Command(costmaxBinary, "evidence", "show", "any-artifact-id")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("evidence show failed: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "costmax artifact retrieve") {
		t.Errorf("evidence show hint does not mention the implemented command:\n%s", text)
	}
	if strings.Contains(text, "costmax evidence retrieve") {
		t.Errorf("evidence show hint references a non-existent command:\n%s", text)
	}
}
