package unit

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/pipeline"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

// newPipelineDeps mirrors the integration-test server setup: a temp config
// pointing at a scratch dir, with a real store, sqlite db, classifier,
// registry, and redactor.
func newPipelineDeps(t *testing.T) pipeline.Deps {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Store.DBPath = filepath.Join(dir, "pipeline_test.db")
	cfg.Store.ArtifactDir = filepath.Join(dir, "artifacts")

	db, err := store.Open(cfg.Store.DBPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	artStore, err := artifacts.NewStore(cfg.Store.ArtifactDir, cfg.Store.MaxArtifactSize)
	if err != nil {
		t.Fatalf("new artifact store: %v", err)
	}

	return pipeline.Deps{
		Store:      artStore,
		DB:         db,
		Classifier: events.NewClassifier(),
		Registry:   reducers.NewRegistry(cfg),
		Redactor:   privacy.NewRedactor(),
		SessionID:  "pipeline-test-session",
	}
}

// TestProcessReductionEnvelope proves the shared pipeline produces the same
// envelope the MCP server integration tests assert on: a verbose recognized
// command reduces, and the rendered envelope carries the artifact reference.
func TestProcessReductionEnvelope(t *testing.T) {
	deps := newPipelineDeps(t)

	command := "for i in $(seq 1 80); do echo \"line $i: this is verbose test output that should be reduced by the terminal reducer\"; done"
	var output strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&output, "line %d: this is verbose test output that should be reduced by the terminal reducer\n", i)
	}

	envelope, err := pipeline.Process(deps, output.String(), command, 0, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !strings.Contains(envelope, "Recommendation: reduce") {
		t.Errorf("expected reduce recommendation, got:\n%s", envelope)
	}
	if !strings.Contains(envelope, "Output mode: compact summary") {
		t.Errorf("expected compact summary mode, got:\n%s", envelope)
	}
	if !strings.Contains(envelope, "Artifact ID:") || !strings.Contains(envelope, "Artifact URI: cmx://artifact/") {
		t.Errorf("expected artifact references in envelope, got:\n%s", envelope)
	}

	// Session metrics must be persisted under the caller's session ID.
	raw, compact, reduced, calls, err := deps.DB.GetSessionMetrics(deps.SessionID)
	if err != nil {
		t.Fatalf("load session metrics: %v", err)
	}
	if raw <= compact || reduced != 1 || calls != 1 {
		t.Errorf("unexpected session metrics: raw=%d compact=%d reduced=%d calls=%d", raw, compact, reduced, calls)
	}
}

// TestProcessEnvelopeByteIdenticalAcrossCallers proves the golden rule: the
// same input, command, and exit code produce a byte-identical envelope
// regardless of the caller's session or tool tag. The only variable field is
// the artifact ID, which is a fresh uuid per invocation by design; after
// normalizing it, the two envelopes must match byte for byte.
func TestProcessEnvelopeByteIdenticalAcrossCallers(t *testing.T) {
	command := "go test ./..."
	output := strings.Repeat("=== RUN   TestThing\n--- PASS: TestThing (0.00s)\nPASS\nok  example/m 0.5s\n", 40)

	a, err := pipeline.Process(newPipelineDeps(t), output, command, 1, "mcp_costmax_run")
	if err != nil {
		t.Fatalf("Process (server): %v", err)
	}
	b, err := pipeline.Process(newPipelineDeps(t), output, command, 1, "cli_artifact_add")
	if err != nil {
		t.Fatalf("Process (cli): %v", err)
	}

	artifactIDRe := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	na := artifactIDRe.ReplaceAllString(a, "<artifact-id>")
	nb := artifactIDRe.ReplaceAllString(b, "<artifact-id>")
	if na != nb {
		t.Fatalf("envelope differs across callers:\n--- server ---\n%s\n--- cli ---\n%s", na, nb)
	}

	// Fixed envelope fields render exactly as the policy contract specifies.
	wantPrefix := "[costmax_run] Recommendation: reduce\n" +
		"Output mode: compact summary\n" +
		"Command: go test ./...\n" +
		"Exit: 1\n" +
		"Raw tokens: 720\n" +
		"Model-visible tokens: 31\n" +
		"Artifact ID: <artifact-id>\n" +
		"Artifact URI: cmx://artifact/<artifact-id>\n" +
		"---\n"
	if !strings.HasPrefix(na, wantPrefix) {
		t.Errorf("envelope prefix mismatch:\nwant prefix:\n%s\ngot:\n%s", wantPrefix, na)
	}
}
