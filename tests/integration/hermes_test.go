package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesInstallFreshConfig(t *testing.T) {
	hermesHome := t.TempDir()
	configPath := filepath.Join(hermesHome, "config.yaml")

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, args...)
		cmd.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("costmax %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	if out := run("install", "--target", "hermes"); !strings.Contains(out, "installed") {
		t.Fatalf("install output = %q", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "mcp_servers:") || !strings.Contains(text, "costmaxx:") {
		t.Fatalf("fresh config missing mcp_servers/costmaxx:\n%s", text)
	}
	if !strings.Contains(text, `args: ["mcp"]`) || !strings.Contains(text, "command:") {
		t.Fatalf("fresh config missing costmaxx server shape:\n%s", text)
	}
	uninstall := exec.Command(costmaxBinary, "uninstall", "--target", "hermes")
	uninstall.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	if out, err := uninstall.CombinedOutput(); err != nil {
		t.Fatalf("fresh config uninstall failed: %v\n%s", err, out)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mcp_servers: {}") {
		t.Fatalf("uninstall should leave an empty mapping, got:\n%s", data)
	}
}

func TestHermesInstallHandlesQuotedTopLevelKey(t *testing.T) {
	hermesHome := t.TempDir()
	configPath := filepath.Join(hermesHome, "config.yaml")
	fixture := "model: gpt-4o\n\n\"mcp_servers\":\n  \"github\":\n    command: \"npx\"\n    args: [\"-y\", \"@modelcontextprotocol/server-github\"]\n"
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	install := exec.Command(costmaxBinary, "install", "--target", "hermes")
	install.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	out, err := install.CombinedOutput()
	if err != nil {
		t.Fatalf("quoted mcp_servers key should merge: %v\n%s", err, out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "mcp_servers") != 1 || !strings.Contains(text, "github") || !strings.Contains(text, "costmaxx:") {
		t.Fatalf("quoted-key merge lost content or duplicated the key:\n%s", text)
	}
}

func TestHermesInstallRefusesUnsupportedYAMLShapes(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "null", text: "mcp_servers: null\n"},
		{name: "sequence", text: "mcp_servers:\n  - github\n"},
		{name: "non-empty flow map", text: "mcp_servers: {github: {command: npx}}\n"},
		{name: "duplicate server", text: "mcp_servers:\n  github:\n    command: npx\n  github:\n    command: node\n"},
		{name: "invalid yaml", text: "mcp_servers: [\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hermesHome := t.TempDir()
			configPath := filepath.Join(hermesHome, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			install := exec.Command(costmaxBinary, "install", "--target", "hermes")
			install.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
			out, err := install.CombinedOutput()
			if err == nil {
				t.Fatalf("unsupported YAML shape was accepted:\n%s", out)
			}
			data, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(data) != tc.text {
				t.Fatalf("refusal modified config:\n%s", data)
			}
			if backups, _ := filepath.Glob(configPath + ".costmaxx.bak.*"); len(backups) != 0 {
				t.Fatalf("refusal created a backup: %v", backups)
			}
		})
	}
}

func TestHermesInstallMergesUnrelatedServers(t *testing.T) {
	hermesHome := t.TempDir()
	configPath := filepath.Join(hermesHome, "config.yaml")
	fixture := `model: gpt-4o

mcp_servers:
  github:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
  # a local helper
  bln-grammar:
    command: node
    args: ["/path/to/index.mjs"]
    enabled: true

network:
  proxy: null
`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(costmaxBinary, args...)
		cmd.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("costmax %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	if out := run("install", "--target", "hermes"); !strings.Contains(out, "installed") {
		t.Fatalf("install output = %q", out)
	}
	data, _ := os.ReadFile(configPath)
	text := string(data)
	for _, want := range []string{"github", "bln-grammar", "costmaxx:", "model: gpt-4o", "network:", "proxy: null"} {
		if !strings.Contains(text, want) {
			t.Errorf("merge dropped %q:\n%s", want, text)
		}
	}
	// Unrelated content order is preserved; costmaxx sits under mcp_servers.
	ms := strings.Index(text, "mcp_servers:")
	cx := strings.Index(text, "costmaxx:")
	net := strings.Index(text, "network:")
	if !(ms < cx && cx < net) {
		t.Errorf("costmaxx not placed inside mcp_servers before network:\n%s", text)
	}

	// Idempotent second install.
	if out := run("install", "--target", "hermes"); !strings.Contains(out, "already installed") {
		t.Fatalf("second install output = %q", out)
	}
	data, _ = os.ReadFile(configPath)
	if strings.Count(string(data), "costmaxx:") != 1 {
		t.Errorf("install not idempotent:\n%s", data)
	}

	// Uninstall preserves everything else.
	if out := run("uninstall", "--target", "hermes"); !strings.Contains(out, "uninstalled") {
		t.Fatalf("uninstall output = %q", out)
	}
	data, _ = os.ReadFile(configPath)
	text = string(data)
	if strings.Contains(text, "costmaxx") {
		t.Fatalf("uninstall retained costmaxx:\n%s", text)
	}
	for _, want := range []string{"github", "bln-grammar", "model: gpt-4o", "network:", "proxy: null"} {
		if !strings.Contains(text, want) {
			t.Errorf("uninstall dropped %q:\n%s", want, text)
		}
	}
	if backups, _ := filepath.Glob(configPath + ".costmaxx.bak.*"); len(backups) != 2 {
		t.Fatalf("install and uninstall should retain two distinct backups, found %v", backups)
	}
}

func TestHermesInstallRefusesForeignEntry(t *testing.T) {
	hermesHome := t.TempDir()
	configPath := filepath.Join(hermesHome, "config.yaml")
	foreign := "mcp_servers:\n  costmaxx:\n    command: \"/some/other/tool\"\n    args: [\"mcp\"]\n"
	if err := os.WriteFile(configPath, []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}

	install := exec.Command(costmaxBinary, "install", "--target", "hermes")
	install.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	out, err := install.CombinedOutput()
	if err == nil {
		t.Fatalf("install must refuse to overwrite a foreign costmaxx entry (exit 0):\n%s", out)
	}
	if !strings.Contains(string(out), "refusing") {
		t.Errorf("install error should mention the refusal:\n%s", out)
	}
	data, _ := os.ReadFile(configPath)
	if string(data) != foreign {
		t.Fatalf("install modified the foreign config:\n%s", data)
	}
	if backups, _ := filepath.Glob(configPath + ".costmaxx.bak.*"); len(backups) != 0 {
		t.Errorf("a refusal must not create a backup, found %v", backups)
	}

	// Uninstall must also refuse to remove a foreign entry.
	uninstall := exec.Command(costmaxBinary, "uninstall", "--target", "hermes")
	uninstall.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	uout, uerr := uninstall.CombinedOutput()
	if uerr == nil {
		t.Fatalf("uninstall must refuse to remove a foreign costmaxx entry (exit 0):\n%s", uout)
	}
	data, _ = os.ReadFile(configPath)
	if string(data) != foreign {
		t.Fatalf("uninstall modified the foreign config:\n%s", data)
	}
}

func TestHermesInstallRecognizesCostmaxBinaryAlias(t *testing.T) {
	hermesHome := t.TempDir()
	configPath := filepath.Join(hermesHome, "config.yaml")
	// A prior install may have recorded a PATH-resolved command rather than
	// today's absolute executable path. It is still CostMax-owned and must be
	// safe to re-install after a binary upgrade or relocation.
	fixture := "mcp_servers:\n  costmaxx:\n    command: \"costmaxx\"\n    args: [\"mcp\"]\n    enabled: true\n"
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}

	install := exec.Command(costmaxBinary, "install", "--target", "hermes")
	install.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	out, err := install.CombinedOutput()
	if err != nil {
		t.Fatalf("alias-owned entry should be idempotent: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "already installed") {
		t.Fatalf("expected already installed, got %s", out)
	}
	if backups, _ := filepath.Glob(configPath + ".costmaxx.bak.*"); len(backups) != 0 {
		t.Fatalf("idempotent install should not create a backup, found %v", backups)
	}
}

func TestHermesInstallHonorsHERMES_HOME(t *testing.T) {
	hermesHome := t.TempDir()
	alt := t.TempDir() // should be ignored when HERMES_HOME is set
	configPath := filepath.Join(hermesHome, "config.yaml")

	cmd := exec.Command(costmaxBinary, "install", "--target", "hermes")
	cmd.Env = append(os.Environ(), "HERMES_HOME="+hermesHome, "HOME="+alt)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config not written to HERMES_HOME (%s): %v", configPath, err)
	}
	if _, err := os.Stat(filepath.Join(alt, ".hermes", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install wrote outside HERMES_HOME")
	}
}

func TestHermesDoctorReportsInformational(t *testing.T) {
	hermesHome := t.TempDir()
	// No codex config either, so doctor still fails overall; hermes_mcp_config
	// must be reported and informational (does not add to the failure set).
	cmd := exec.Command(costmaxBinary, "install", "--target", "hermes")
	cmd.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}

	doctor := exec.Command(costmaxBinary, "doctor")
	doctor.Env = append(os.Environ(), "HOME="+hermesHome, "HERMES_HOME="+hermesHome)
	dout, _ := doctor.CombinedOutput()
	if !strings.Contains(string(dout), "hermes_mcp_config") || !strings.Contains(string(dout), "✓") {
		t.Fatalf("doctor does not report hermes_mcp_config OK:\n%s", dout)
	}

	// When Hermes is not installed at all, doctor reports it but does not
	// require it (informational).
	emptyHome := t.TempDir()
	doctor2 := exec.Command(costmaxBinary, "doctor")
	doctor2.Env = append(os.Environ(), "HOME="+emptyHome, "HERMES_HOME="+filepath.Join(emptyHome, ".hermes"))
	d2out, _ := doctor2.CombinedOutput()
	if !strings.Contains(string(d2out), "hermes_mcp_config") || !strings.Contains(string(d2out), "not installed") {
		t.Fatalf("doctor should report hermes_mcp_config as not installed:\n%s", d2out)
	}
}
