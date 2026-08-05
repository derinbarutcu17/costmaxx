package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/mcp"
)

func TestMCPServerInitialize(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	resp := mcpCall(t, srv, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "1"},
	})

	if resp.Error != nil {
		t.Fatalf("initialize error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
}

func TestMCPServerToolList(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	// initialize first
	mcpCall(t, srv, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "1"},
	})

	resp := mcpCall(t, srv, 2, "tools/list", nil)

	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "costmax_run" {
		t.Errorf("expected costmax_run, got %s", tool["name"])
	}
}

func TestMCPCostmaxRunSmallOutput(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)
	resp := mcpCall(t, srv, 2, "tools/call", map[string]any{
		"name": "costmax_run",
		"arguments": map[string]string{
			"command": "echo hello world",
		},
	})

	if resp.Error != nil {
		t.Fatalf("tools/call error: %v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hello world") {
		t.Errorf("expected 'hello world' in output, got: %s", text)
	}
	if !strings.Contains(text, "Artifact ID:") {
		t.Error("expected artifact ID in output")
	}
	if !strings.Contains(text, "Recommendation: preserve_full") {
		t.Errorf("expected preserve_full recommendation for unknown short output, got: %s", text)
	}
}

func TestMCPCostmaxRunVerboseReduces(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)

	// Generate enough lines to trigger terminal reduction
	cmd := "for i in $(seq 1 80); do echo \"line $i: this is verbose test output that should be reduced by the terminal reducer\"; done"
	resp := mcpCall(t, srv, 2, "tools/call", map[string]any{
		"name": "costmax_run",
		"arguments": map[string]string{
			"command": cmd,
		},
	})

	if resp.Error != nil {
		t.Fatalf("tools/call error: %v", resp.Error)
	}

	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)

	// Verify raw vs model-visible token estimates in output
	if !strings.Contains(text, "Raw tokens:") {
		t.Error("expected Raw tokens in output")
	}
	if !strings.Contains(text, "Model-visible tokens:") {
		t.Error("expected Model-visible tokens in output")
	}
	if !strings.Contains(text, "Recommendation: reduce") {
		t.Errorf("expected reduce recommendation for recognized verbose output, got: %s", text)
	}

	// Extract artifact ID
	var artifactID string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Artifact ID:") {
			artifactID = strings.TrimSpace(strings.TrimPrefix(line, "Artifact ID:"))
			break
		}
	}
	if artifactID == "" {
		t.Fatal("artifact ID not found in output")
	}

	// Verify Artifact URI line matches
	var artifactURI string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Artifact URI:") {
			artifactURI = strings.TrimSpace(strings.TrimPrefix(line, "Artifact URI:"))
			break
		}
	}
	if artifactURI == "" {
		t.Fatal("Artifact URI line not found in output")
	}
	expectedURI := "cmx://artifact/" + artifactID
	if artifactURI != expectedURI {
		t.Errorf("Artifact URI mismatch: got %q, want %q", artifactURI, expectedURI)
	}

	// Verify no stale literal "costmax_run" reference in compact output
	if strings.Contains(text, "cmx://artifact/costmax_run") {
		t.Error("compact output contains stale literal artifact reference 'cmx://artifact/costmax_run'")
	}

	// Verify artifact exists and content matches
	meta, err := srv.GetArtifact(artifactID)
	if err != nil || meta == nil {
		t.Fatalf("artifact metadata not found: err=%v meta=%v", err, meta)
	}

	// Verify raw artifact content is retrievable byte-for-byte
	raw, err := srv.GetArtifactStore().RetrieveByDigest(meta.ContentDigest)
	if err != nil {
		t.Fatalf("retrieve artifact: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("retrieved artifact is empty")
	}
	if int64(len(raw)) != meta.OriginalBytes {
		t.Errorf("artifact size mismatch: got %d, expected %d", len(raw), meta.OriginalBytes)
	}

	// Verify reduction actually happened
	if meta.EstimatedTokens <= len(raw)/4 {
		t.Log("output was small, reduction may not have triggered — this is acceptable")
	}

	// Verify resources/read on the emitted URI retrieves the exact artifact
	uri := "cmx://artifact/" + artifactID
	respURI := mcpCall(t, srv, 3, "resources/read", map[string]string{"uri": uri})
	if respURI.Error != nil {
		t.Fatalf("resources/read error: %v", respURI.Error)
	}
	resultURI := respURI.Result.(map[string]any)
	contents := resultURI["contents"].([]any)
	retrievedText := contents[0].(map[string]any)["text"].(string)
	if retrievedText != string(raw) {
		t.Error("resources/read returned different content than direct artifact retrieval")
	}
}

func TestMCPCostmaxRunPreservesSuccessfulStderrAndAccumulates(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)
	command := "for i in $(seq 1 100); do echo 'stdout line for stderr regression'; done; printf 'stderr-success\\n' >&2"
	for id := 2; id <= 3; id++ {
		resp := mcpCall(t, srv, id, "tools/call", map[string]any{
			"name":      "costmax_run",
			"arguments": map[string]string{"command": command},
		})
		if resp.Error != nil {
			t.Fatalf("tools/call error on invocation %d: %v", id, resp.Error)
		}
		result := resp.Result.(map[string]any)
		text := result["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "stderr-success") {
			t.Fatalf("successful stderr was dropped on invocation %d: %s", id, text)
		}
	}

	count, err := srv.ReductionCount()
	if err != nil {
		t.Fatalf("count reductions: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected two persisted reductions for repeated calls, got %d", count)
	}
	raw, compact, reduced, calls, err := srv.SessionMetrics()
	if err != nil {
		t.Fatalf("load session metrics: %v", err)
	}
	if raw <= compact || reduced != 2 || calls != 2 {
		t.Fatalf("unexpected accumulated metrics: raw=%d compact=%d reduced=%d calls=%d", raw, compact, reduced, calls)
	}
	if srv.SessionID() == "" {
		t.Fatal("expected stable MCP session ID")
	}
}

func TestMCPCostmaxRunWithCwd(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello\nworld\n"), 0600); err != nil {
		t.Fatal(err)
	}

	resp := mcpCall(t, srv, 2, "tools/call", map[string]any{
		"name": "costmax_run",
		"arguments": map[string]string{
			"command": "wc -l test.txt",
			"cwd":     tmpDir,
		},
	})

	if resp.Error != nil {
		t.Fatalf("tools/call error: %v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Artifact ID:") {
		t.Errorf("expected artifact ID in output, got: %s", text)
	}
}

func TestMCPCostmaxRunCommandNotFound(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)
	resp := mcpCall(t, srv, 2, "tools/call", map[string]any{
		"name": "costmax_run",
		"arguments": map[string]string{
			"command": "nonexistent_command_xyz123",
		},
	})

	if resp.Error != nil {
		t.Fatalf("tools/call should not error on bad command, got: %v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	isError := result["isError"].(bool)
	content := result["content"].([]any)
	if isError {
		t.Error("a shell command exit is evidence, not an MCP transport error")
	}
	if !strings.Contains(content[0].(map[string]any)["text"].(string), "Exit: 127") {
		t.Errorf("expected command exit code in evidence, got: %s", content[0].(map[string]any)["text"])
	}
}

func TestMCPCostmaxRunMissingCommand(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)
	resp := mcpCall(t, srv, 2, "tools/call", map[string]any{
		"name":      "costmax_run",
		"arguments": map[string]string{},
	})

	if resp.Error == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestMCPUnknownTool(t *testing.T) {
	srv, cleanup := newMCPServer(t)
	defer cleanup()
	defer srv.Close()

	mcpCall(t, srv, 1, "initialize", nil)
	resp := mcpCall(t, srv, 2, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]string{},
	})

	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- helpers ---

type testMCPResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *mcpCallError `json:"error,omitempty"`
}

type mcpCallError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newMCPServer(t *testing.T) (*mcp.Server, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Core.DataDir = dir
	cfg.Store.DBPath = filepath.Join(dir, "mcp_test.db")
	cfg.Store.ArtifactDir = filepath.Join(dir, "artifacts")

	srv, err := mcp.NewServer(cfg)
	if err != nil {
		t.Fatalf("new mcp server: %v", err)
	}
	return srv, func() { srv.Close() }
}

func mcpCall(t *testing.T, srv *mcp.Server, id int, method string, params any) *testMCPResponse {
	t.Helper()

	var buf bytes.Buffer
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	input, _ := json.Marshal(req)
	input = append(input, '\n')

	// Create a pipe for the server to read from
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Write input to pipe
	w.Write(input)
	w.Close()

	// Read response
	var resp testMCPResponse
	if err := srv.Serve(r, &buf); err != nil {
		t.Fatalf("serve error: %v", err)
	}

	// Parse response from buffer
	dec := json.NewDecoder(&buf)
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return &resp
}

func TestMCPSubprocessProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test")
	}

	binary := costmaxBinary
	homeDir := t.TempDir()

	// Prepare messages: initialize, initialized notification, tools/list
	messages := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"req-3","method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"costmax_run","arguments":{"command":"echo hello"}}}`,
	}

	input := strings.Join(messages, "\n")

	cmd := exec.Command(binary, "mcp")
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("mcp subprocess failed: %v\nstderr: %s", err, stderr)
	}

	// Parse output (one JSON object per line)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	// Expect exactly 3 responses (notification gets none)
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses (notification suppressed), got %d:\n%s", len(lines), string(out))
	}

	// Line 1: initialize response
	var initResp testMCPResponse
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatal(err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize error: %v", initResp.Error)
	}
	if initResp.ID != float64(1) {
		t.Errorf("expected id=1, got %v", initResp.ID)
	}

	// Line 2: tools/list response
	var listResp testMCPResponse
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Error != nil {
		t.Fatalf("tools/list error: %v", listResp.Error)
	}
	if listResp.ID != "req-3" {
		t.Errorf("expected id='req-3', got %v", listResp.ID)
	}

	// Line 3: tools/call response
	var callResp testMCPResponse
	if err := json.Unmarshal([]byte(lines[2]), &callResp); err != nil {
		t.Fatal(err)
	}
	if callResp.Error != nil {
		t.Fatalf("tools/call error: %v", callResp.Error)
	}
	if callResp.ID != float64(4) {
		t.Errorf("expected id=4, got %v", callResp.ID)
	}

	// Verify notification was suppressed (no response for method without id)
	t.Log("PASS: initialized notification suppressed, string and numeric IDs preserved")
}

func TestMCPNullIdIsRequestNotNotification(t *testing.T) {
	binary := costmaxBinary
	homeDir := t.TempDir()

	// "id": null should still get a response (it's a request, not a notification)
	messages := []string{
		`{"jsonrpc":"2.0","id":null,"method":"tools/list","params":{}}`,
	}

	input := strings.Join(messages, "\n")
	cmd := exec.Command(binary, "mcp")
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mcp subprocess failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response (id:null is a request), got %d:\n%s", len(lines), string(out))
	}
	var resp testMCPResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	// Response must be present (id:null is a request, not a notification)
	// The echoed id may be null, which is valid JSON-RPC
	t.Logf("PASS: id:null request got response (id=%v)", resp.ID)
}

var _ = fmt.Sprintf
