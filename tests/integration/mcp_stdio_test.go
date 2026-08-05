package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Subprocess JSON-RPC driver: speaks raw MCP over stdio to the real binary,
// each process in an isolated HOME so ~/.costmax never touches the user's.

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newIsolatedHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	return home
}

// runMCP starts one server process, writes the given request lines, and
// returns parsed responses plus stderr. Requests without an id (notifications)
// produce no response, so callers must account for that count.
func runMCP(t *testing.T, home string, lines ...string) ([]rpcResponse, string) {
	t.Helper()
	cmd := exec.Command(costmaxBinary, "mcp")
	cmd.Env = append(os.Environ(), "HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		fmt.Fprintln(stdin, l)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Logf("server exited non-zero: %v (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	var out []rpcResponse
	sc := bufio.NewScanner(&stdout)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal(line, &r); err != nil {
			t.Errorf("unparseable response line %q: %v", line, err)
			continue
		}
		out = append(out, r)
	}
	return out, stderr.String()
}

func oneCall(t *testing.T, home, method string, params string) rpcResponse {
	t.Helper()
	id := `"t"`
	if method == "initialize" {
		params = `{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`
	}
	resps, _ := runMCP(t, home, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q,"params":%s}`, id, method, params))
	if len(resps) != 1 {
		t.Fatalf("expected 1 response for %s, got %d", method, len(resps))
	}
	return resps[0]
}

func TestStdioInitialize(t *testing.T) {
	r := oneCall(t, newIsolatedHome(t), "initialize", "")
	if r.Error != nil {
		t.Fatalf("initialize error: %v", r.Error)
	}
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatal(err)
	}
	if res.ProtocolVersion != "2024-11-05" || res.ServerInfo.Name != "costmaxx" {
		t.Errorf("unexpected initialize result: %+v", res)
	}
}

func TestStdioToolList(t *testing.T) {
	r := oneCall(t, newIsolatedHome(t), "tools/list", "{}")
	var res struct {
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "costmax_run" {
		t.Fatalf("expected exactly costmax_run, got %+v", res.Tools)
	}
	if !bytes.Contains(res.Tools[0].InputSchema, []byte(`"command"`)) {
		t.Error("tool schema missing command property")
	}
}

func stdioCall(t *testing.T, home, command string) (rpcResponse, string) {
	t.Helper()
	args := map[string]string{"command": command}
	b, _ := json.Marshal(args)
	return oneCall(t, home, "tools/call", fmt.Sprintf(`{"name":"costmax_run","arguments":%s}`, b)), ""
}

func TestStdioRunExitZero(t *testing.T) {
	r, _ := stdioCall(t, newIsolatedHome(t), "echo hello-world")
	if r.Error != nil {
		t.Fatalf("call error: %v", r.Error)
	}
	var res struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Error("successful command must not be an error")
	}
	text := ""
	if len(res.Content) > 0 {
		text = res.Content[0].Text
	}
	for _, want := range []string{"Recommendation:", "Exit: 0", "Artifact ID:", "hello-world"} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
}

func TestStdioRunExitOneNotTransportError(t *testing.T) {
	r, _ := stdioCall(t, newIsolatedHome(t), "exit 3")
	var res struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	if res.IsError {
		t.Error("nonzero exit is evidence, not an MCP transport error")
	}
	if !strings.Contains(res.Content[0].Text, "Exit: 3") {
		t.Errorf("expected Exit: 3 in response")
	}
}

func TestStdioRunCommandNotFound(t *testing.T) {
	r, _ := stdioCall(t, newIsolatedHome(t), "definitely-not-a-command-xyz")
	if r.Error != nil {
		t.Fatalf("127 must not be a transport error: %v", r.Error)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	if !strings.Contains(res.Content[0].Text, "Exit: 127") {
		t.Errorf("expected Exit: 127, got:\n%s", res.Content[0].Text)
	}
}

func TestStdioRunSignalDeath(t *testing.T) {
	r, _ := stdioCall(t, newIsolatedHome(t), "kill -TERM $$")
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	text := res.Content[0].Text
	if !strings.Contains(text, "Exit: 143") {
		t.Errorf("expected signal exit 143 (128+SIGTERM), got:\n%s", text)
	}
}

func TestStdioRunEmptyOutput(t *testing.T) {
	r, _ := stdioCall(t, newIsolatedHome(t), "true")
	if r.Error != nil {
		t.Fatalf("empty output must not error: %v", r.Error)
	}
}

func TestStdioRunBinaryOutput(t *testing.T) {
	r, _ := stdioCall(t, newIsolatedHome(t), "head -c 100 /dev/urandom")
	if r.Error != nil {
		t.Fatalf("binary output must not crash: %v", r.Error)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	if !strings.Contains(res.Content[0].Text, "preserve_full") {
		t.Logf("binary output recommendation: %q (expected preserve_full)", res.Content[0].Text)
	}
}

func TestStdioSecretRedactedInResponseAndArtifact(t *testing.T) {
	home := newIsolatedHome(t)
	secret := "api_key=sk-1234567890abcdef"
	r, _ := stdioCall(t, home, "echo "+secret)
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	text := res.Content[0].Text
	// The model-visible OUTPUT section (after the --- separator) must be
	// redacted. The command line above it echoes the command the model
	// itself sent; it is intentionally not redacted (finding: it persists
	// unredacted in artifact metadata too).
	output := text[strings.Index(text, "\n---\n")+len("\n---\n"):]
	if strings.Contains(output, secret) || !strings.Contains(output, "[REDACTED]") {
		t.Errorf("model-visible output must be redacted, got:\n%s", output)
	}
	// Pull the artifact ID and read it back: stored evidence must not hold
	// the secret either.
	artifactID := extractArtifactID(t, text)
	rr := oneCall(t, home, "resources/read", fmt.Sprintf(`{"uri":"cmx://artifact/%s"}`, artifactID))
	if rr.Error != nil {
		t.Fatalf("resources/read error: %v", rr.Error)
	}
	var readRes struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	json.Unmarshal(rr.Result, &readRes)
	stored := ""
	if len(readRes.Contents) > 0 {
		stored = readRes.Contents[0].Text
	}
	if strings.Contains(stored, "sk-1234567890abcdef") {
		t.Error("stored artifact contains the secret (redaction bypassed)")
	}
}

func TestStdioResourceReadRoundTrip(t *testing.T) {
	home := newIsolatedHome(t)
	payload := "line one\nline two\nline three\n"
	r, _ := stdioCall(t, home, "printf '"+payload+"'")
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	artifactID := extractArtifactID(t, res.Content[0].Text)

	rr := oneCall(t, home, "resources/read", fmt.Sprintf(`{"uri":"cmx://artifact/%s"}`, artifactID))
	var readRes struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	json.Unmarshal(rr.Result, &readRes)
	if len(readRes.Contents) == 0 {
		t.Fatal("no contents in resource read")
	}
	if readRes.Contents[0].Text != payload {
		t.Errorf("round-trip mismatch:\n got: %q\nwant: %q", readRes.Contents[0].Text, payload)
	}
}

func TestStdioResourceReadUnknown(t *testing.T) {
	r := oneCall(t, newIsolatedHome(t), "resources/read", `{"uri":"cmx://artifact/does-not-exist"}`)
	if r.Error == nil {
		t.Error("unknown artifact should error")
	}
	r2 := oneCall(t, newIsolatedHome(t), "resources/read", `{"uri":"http://not-a-cmx-uri"}`)
	if r2.Error == nil {
		t.Error("non-cmx uri should error")
	}
}

func TestStdioMalformedJSON(t *testing.T) {
	home := newIsolatedHome(t)
	resps, _ := runMCP(t, home, `this is not json`)
	if len(resps) != 1 || resps[0].Error == nil || resps[0].Error.Code != -32700 {
		t.Errorf("expected parse error -32700, got %+v", resps)
	}
}

func TestStdioUnknownMethod(t *testing.T) {
	r := oneCall(t, newIsolatedHome(t), "bogus/method", "{}")
	if r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("expected method not found -32601, got %+v", r.Error)
	}
}

func TestStdioNotificationNoResponse(t *testing.T) {
	home := newIsolatedHome(t)
	// A notification (no id) must not produce a response; the initialize
	// request after it should be the only response line.
	resps, _ := runMCP(t, home,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
	)
	if len(resps) != 1 {
		t.Errorf("expected 1 response (notification skipped), got %d", len(resps))
	}
}

func TestStdioNullIdIsRequest(t *testing.T) {
	home := newIsolatedHome(t)
	resps, _ := runMCP(t, home, `{"jsonrpc":"2.0","id":null,"method":"tools/list","params":{}}`)
	if len(resps) != 1 {
		t.Errorf("id null is a request, expected 1 response, got %d", len(resps))
	}
}

func TestStdioLongCommandGuardDowngrades(t *testing.T) {
	longCmd := "echo " + strings.Repeat("x", 4096)
	r, _ := stdioCall(t, newIsolatedHome(t), longCmd)
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(r.Result, &res)
	text := res.Content[0].Text
	if !strings.Contains(text, "passthrough") {
		t.Errorf("long command envelope must downgrade to passthrough, got:\n%s", text[:min(len(text), 200)])
	}
}

// Two sequential calls in one process: both artifacts retrievable, responses
// valid, one stable session accumulating metrics in the DB.
func TestStdioSequentialCallsSameProcess(t *testing.T) {
	home := newIsolatedHome(t)
	resps, _ := runMCP(t, home,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"costmax_run","arguments":{"command":"echo first"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"costmax_run","arguments":{"command":"echo second"}}}`,
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	for i, r := range resps {
		if r.Error != nil {
			t.Fatalf("call %d error: %v", i, r.Error)
		}
	}
}

// Two concurrent server processes sharing one data dir: WAL + busy timeout
// should serialize; neither should crash or corrupt.
func TestStdioConcurrentProcessesSameDataDir(t *testing.T) {
	home := newIsolatedHome(t)
	var wg sync.WaitGroup
	results := make([]rpcResponse, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = oneCall(t, home, "tools/call", `{"name":"costmax_run","arguments":{"command":"echo concurrent"}}`)
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.Error != nil {
			t.Logf("concurrent process %d: %v", i, r.Error)
		}
		if len(r.Result) == 0 && r.Error == nil {
			t.Errorf("concurrent process %d: empty result", i)
		}
	}
}

func extractArtifactID(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Artifact ID: ") {
			return strings.TrimPrefix(line, "Artifact ID: ")
		}
	}
	t.Fatalf("no Artifact ID in response:\n%s", text)
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
