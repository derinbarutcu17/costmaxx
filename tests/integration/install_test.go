package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDoctorUninstallCleanCodexHome(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	original := "[mcp_servers.other]\ncommand = \"other-mcp\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+codexHome)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("costmax %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	if out := run("install"); !strings.Contains(out, "installed") {
		t.Fatalf("install output = %q", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), original) || !strings.Contains(string(data), "[mcp_servers.costmaxx]") {
		t.Fatalf("install changed unrelated config or missed CostMax entry:\n%s", data)
	}
	if backups, err := filepath.Glob(configPath + ".costmaxx.bak.*"); err != nil || len(backups) != 1 {
		t.Fatalf("backup count = %d, err=%v", len(backups), err)
	}
	if out := run("install"); !strings.Contains(out, "already installed") {
		t.Fatalf("second install output = %q", out)
	}
	if codex, err := exec.LookPath("codex"); err == nil {
		cmd := exec.Command(codex, "mcp", "get", "costmaxx")
		cmd.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+codexHome)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "costmaxx") {
			t.Fatalf("Codex did not load installed MCP config: %v\n%s", err, out)
		}
	}

	doctor := run("doctor")
	for _, check := range []string{"binary", "codex_mcp_config", "artifact_store", "mcp_handshake"} {
		if !strings.Contains(doctor, check) || !strings.Contains(doctor, "✓") {
			t.Fatalf("doctor missing successful %s check:\n%s", check, doctor)
		}
	}

	// Exercise the installed binary through its real stdio MCP protocol before
	// uninstalling it. This is the local live-smoke leg of install → doctor →
	// smoke → uninstall, without spending an external Codex API call.
	smoke := exec.Command(costmaxBinary, "mcp")
	smoke.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+codexHome)
	smoke.Stdin = strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"install-smoke","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"costmax_run","arguments":{"command":"printf install-smoke-ok"}}}`,
	}, "\n"))
	smokeOut, err := smoke.CombinedOutput()
	if err != nil {
		t.Fatalf("installed MCP smoke failed: %v\n%s", err, smokeOut)
	}
	if !strings.Contains(string(smokeOut), `"serverInfo"`) || !strings.Contains(string(smokeOut), "install-smoke-ok") {
		t.Fatalf("installed MCP smoke returned unexpected output:\n%s", smokeOut)
	}

	if out := run("uninstall"); !strings.Contains(out, "uninstalled") {
		t.Fatalf("uninstall output = %q", out)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), original) || strings.Contains(string(data), "[mcp_servers.costmaxx]") {
		t.Fatalf("uninstall changed unrelated config or retained CostMax entry:\n%s", data)
	}
}
