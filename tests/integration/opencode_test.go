package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stripLineComments removes // line comments from JSONC text while leaving
// string literals intact.
func stripLineComments(s string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// stripTrailingCommas removes trailing commas before closing braces.
func stripTrailingCommas(s string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// validJSONC reports whether s parses as strict JSON once comments and
// trailing commas are stripped.
func validJSONC(s string) bool {
	return json.Valid([]byte(stripTrailingCommas(stripLineComments(s))))
}

func TestOpenCodeInstallUninstall(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	opencodeDir := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(opencodeDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(opencodeDir, "opencode.jsonc")
	fixture := `{
  "$schema": "https://opencode.ai/config.json", // schema reference
  "permission": {
    "edit": true,
  },
  "mcp": {}
}
`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+xdg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("costmax %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	if out := run("install", "--target", "opencode"); !strings.Contains(out, "installed") {
		t.Fatalf("install output = %q", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "\"costmaxx\"") || !strings.Contains(text, "\"mcp\"") {
		t.Fatalf("install did not add costmaxx under mcp:\n%s", text)
	}
	if !validJSONC(text) {
		t.Fatalf("installed opencode config does not parse as strict JSON:\n%s", text)
	}
	if backups, err := filepath.Glob(configPath + ".costmaxx.bak.*"); err != nil || len(backups) != 1 {
		t.Fatalf("backup count = %d, err=%v", len(backups), err)
	}

	if out := run("install", "--target", "opencode"); !strings.Contains(out, "already installed") {
		t.Fatalf("second install output = %q", out)
	}

	// Doctor's opencode_mcp_config check is informational: it must report OK
	// here even though doctor still fails overall because the isolated HOME
	// has no Codex config.
	doctor := exec.Command(costmaxBinary, "doctor")
	doctor.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+xdg)
	doctorOut, _ := doctor.CombinedOutput()
	if !strings.Contains(string(doctorOut), "opencode_mcp_config") || !strings.Contains(string(doctorOut), "✓") {
		t.Fatalf("doctor does not report opencode_mcp_config OK:\n%s", doctorOut)
	}

	if out := run("uninstall", "--target", "opencode"); !strings.Contains(out, "uninstalled") {
		t.Fatalf("uninstall output = %q", out)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text = string(data)
	if strings.Contains(text, "costmaxx") || strings.Contains(text, "\"mcp\"") {
		t.Fatalf("uninstall retained CostMax or the emptied mcp key:\n%s", text)
	}
	if !strings.Contains(text, "$schema") || !strings.Contains(text, "permission") || !strings.Contains(text, "edit") {
		t.Fatalf("uninstall changed unrelated fixture content:\n%s", text)
	}
	if !validJSONC(text) {
		t.Fatalf("uninstalled opencode config does not parse as strict JSON:\n%s", text)
	}
}

func TestOpenCodeInstallMergeExistingMCP(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	opencodeDir := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(opencodeDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(opencodeDir, "opencode.jsonc")
	fixture := `{
  "mcp": {
    "github": {
      "type": "remote",
      "url": "https://example.com/mcp"
    }
  }
}
`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+xdg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("costmax %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	if out := run("install", "--target", "opencode"); !strings.Contains(out, "installed") {
		t.Fatalf("install output = %q", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "\"costmaxx\"") || !strings.Contains(text, "\"github\"") {
		t.Fatalf("install did not merge costmaxx next to unrelated mcp servers:\n%s", text)
	}
	if !validJSONC(text) {
		t.Fatalf("merged opencode config does not parse as strict JSON:\n%s", text)
	}

	if out := run("uninstall", "--target", "opencode"); !strings.Contains(out, "uninstalled") {
		t.Fatalf("uninstall output = %q", out)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text = string(data)
	if strings.Contains(text, "costmaxx") {
		t.Fatalf("uninstall retained CostMax entry:\n%s", text)
	}
	if !strings.Contains(text, "\"github\"") || !strings.Contains(text, "https://example.com/mcp") {
		t.Fatalf("uninstall removed the unrelated mcp server:\n%s", text)
	}
	if !validJSONC(text) {
		t.Fatalf("uninstalled opencode config does not parse as strict JSON:\n%s", text)
	}
}

func TestArtifactAddRoundTrip(t *testing.T) {
	home := t.TempDir()
	run := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, args...)
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("costmax %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	input := "line one\nline two\n"
	out := run(input, "artifact", "add", "--command", "printf test")
	if !strings.Contains(out, "cmx://artifact/") {
		t.Fatalf("envelope missing artifact URI:\n%s", out)
	}
	if !strings.Contains(out, "line one") || !strings.Contains(out, "line two") {
		t.Fatalf("envelope missing the command output text:\n%s", out)
	}
	if !strings.Contains(out, "Command: printf test") {
		t.Fatalf("envelope missing the command:\n%s", out)
	}

	m := regexp.MustCompile(`cmx://artifact/([0-9a-f-]+)`).FindStringSubmatch(out)
	if len(m) != 2 {
		t.Fatalf("could not extract artifact ID from:\n%s", out)
	}
	artifactID := m[1]

	retrieved := run("", "artifact", "retrieve", artifactID)
	if retrieved != input {
		t.Fatalf("retrieved artifact = %q, want %q", retrieved, input)
	}
}

// Regression: block comments (/* */) inside values previously corrupted the
// scanner — the block-comment state was never reset at the closing */, so
// everything after the first /* was treated as a comment and the resulting
// config failed validation.
func TestOpenCodeInstallWithBlockComments(t *testing.T) {
	xdg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(xdg, "opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `// header
{"mcp": { /* inline */ }, "a": 1, // trail
/* mid */ "b": 2}
// footer`
	if err := os.WriteFile(filepath.Join(xdg, "opencode", "opencode.jsonc"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}

	out := runWithEnvX([]string{"install", "--target", "opencode"}, map[string]string{"XDG_CONFIG_HOME": xdg})
	if !strings.Contains(out, "installed") {
		t.Fatalf("install failed: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(xdg, "opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"costmaxx"`) {
		t.Fatalf("costmaxx block missing:\n%s", text)
	}
	// Comments outside the modified object and unrelated keys must survive.
	// (A comment inside the mcp object being rewritten is not guaranteed.)
	for _, want := range []string{"// header", "/* mid */", `"a": 1`, `"b": 2`, "// trail"} {
		if !strings.Contains(text, want) {
			t.Errorf("fixture content %q lost after install:\n%s", want, text)
		}
	}

	out = runWithEnvX([]string{"uninstall", "--target", "opencode"}, map[string]string{"XDG_CONFIG_HOME": xdg})
	if !strings.Contains(out, "uninstalled") {
		t.Fatalf("uninstall failed: %s", out)
	}
	data, _ = os.ReadFile(filepath.Join(xdg, "opencode", "opencode.jsonc"))
	if strings.Contains(string(data), "costmaxx") {
		t.Errorf("costmaxx block survived uninstall:\n%s", data)
	}
}

func runWithEnv(args ...string) string {
	return ""
}

func runWithEnvX(args []string, env map[string]string) string {
	cmd := exec.Command(costmaxBinary, args...)
	cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(env["XDG_CONFIG_HOME"]), "XDG_CONFIG_HOME="+env["XDG_CONFIG_HOME"])
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out) + " (err: " + err.Error() + ")"
	}
	return string(out)
}
