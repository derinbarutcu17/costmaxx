package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Spec-framing driver: speaks MCP spec stdio (Content-Length headers) to the
// real binary, exactly as opencode and other spec clients do.

func frame(msg string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(msg), msg)
}

// runMCPSpec starts one server process (default spec framing), writes the
// given messages, and returns parsed responses plus stderr.
func runMCPSpec(t *testing.T, home string, msgs ...string) ([]rpcResponse, string) {
	t.Helper()
	cmd := exec.Command(costmaxBinary, "mcp", "--spec-framing")
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
	for _, m := range msgs {
		if _, err := stdin.Write([]byte(frame(m))); err != nil {
			t.Fatal(err)
		}
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Logf("server exited non-zero: %v (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	var out []rpcResponse
	br := bufio.NewReader(&stdout)
	for {
		var msg []byte
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				msg = nil
				break
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
				var n int
				if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &n); err != nil {
					continue
				}
				blank, err := br.ReadString('\n')
				if err != nil {
					break
				}
				if strings.TrimRight(blank, "\r\n") != "" {
					break
				}
				body := make([]byte, n)
				if _, err := io.ReadFull(br, body); err != nil {
					break
				}
				msg = body
				break
			}
		}
		if msg == nil {
			break
		}
		var r rpcResponse
		if err := json.Unmarshal(msg, &r); err != nil {
			t.Errorf("unparseable response %q: %v", msg, err)
			continue
		}
		out = append(out, r)
	}
	return out, stderr.String()
}

func specCall(t *testing.T, home, method, params string) rpcResponse {
	t.Helper()
	id := `"t"`
	if method == "initialize" {
		params = `{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`
	}
	resps, _ := runMCPSpec(t, home, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q,"params":%s}`, id, method, params))
	if len(resps) != 1 {
		t.Fatalf("expected 1 response for %s, got %d", method, len(resps))
	}
	return resps[0]
}

func TestSpecFramingInitialize(t *testing.T) {
	r := specCall(t, newIsolatedHome(t), "initialize", "")
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

func TestSpecFramingRunAndRead(t *testing.T) {
	home := newIsolatedHome(t)
	r := specCall(t, home, "tools/call", `{"name":"costmax_run","arguments":{"command":"echo spec-framing-works"}}`)
	if r.Error != nil {
		t.Fatalf("call error: %v", r.Error)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].Text
	if !strings.Contains(text, "spec-framing-works") {
		t.Errorf("response missing echoed output:\n%s", text)
	}
	artifactID := extractArtifactID(t, text)
	rr := specCall(t, home, "resources/read", fmt.Sprintf(`{"uri":"cmx://artifact/%s"}`, artifactID))
	if rr.Error != nil {
		t.Fatalf("resources/read error: %v", rr.Error)
	}
	var readRes struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	json.Unmarshal(rr.Result, &readRes)
	if len(readRes.Contents) == 0 || !strings.Contains(readRes.Contents[0].Text, "spec-framing-works") {
		t.Errorf("artifact round-trip mismatch: %+v", readRes.Contents)
	}
}

func TestSpecFramingMalformedHeader(t *testing.T) {
	home := newIsolatedHome(t)
	cmd := exec.Command(costmaxBinary, "mcp", "--spec-framing")
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
	stdin.Write([]byte("Content-Length: not-a-number\r\n\r\n{}"))
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Logf("server exited: %v", err)
	}
	if !strings.Contains(stderr.String(), "Content-Length") && !strings.Contains(stderr.String(), "malformed") {
		t.Logf("stderr: %s", stderr.String())
	}
}
