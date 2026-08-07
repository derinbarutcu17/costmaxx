package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var installTarget string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the CostMax MCP entry into Codex",
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			path   string
			status string
			err    error
		)
		switch installTarget {
		case "codex":
			path, status, err = installCodexMCP()
		case "opencode":
			path, status, err = installOpenCodeMCP()
		default:
			return fmt.Errorf("unknown target %q (expected codex or opencode)", installTarget)
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", status, path)
		return nil
	},
}

var uninstallTarget string

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove only the CostMax MCP entry from Codex",
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			path   string
			status string
			err    error
		)
		switch uninstallTarget {
		case "codex":
			path, status, err = uninstallCodexMCP()
		case "opencode":
			path, status, err = uninstallOpenCodeMCP()
		default:
			return fmt.Errorf("unknown target %q (expected codex or opencode)", uninstallTarget)
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", status, path)
		return nil
	},
}

func codexConfigPath() (string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "config.toml"), nil
}

func costmaxMCPBlock(binary string) string {
	return fmt.Sprintf("[mcp_servers.costmaxx]\ncommand = %q\nargs = [\"mcp\"]\nrequired = true\ndefault_tools_approval_mode = \"approve\"\n", binary)
}

func tableRange(text, header string) (int, int, bool) {
	start := -1
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.TrimSpace(line) == header {
			start = offset
			break
		}
		offset += len(line)
	}
	if start < 0 {
		return 0, 0, false
	}
	end := len(text)
	offset = start + len(header)
	for _, line := range strings.SplitAfter(text[offset:], "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			end = offset
			break
		}
		offset += len(line)
	}
	return start, end, true
}

func isCostmaxMCPBlock(block string) bool {
	return strings.Contains(block, `args = ["mcp"]`) && strings.Contains(strings.ToLower(block), "costmax")
}

func installCodexMCP() (string, string, error) {
	configPath, err := codexConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate Codex config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read Codex config: %w", err)
	}
	text := string(data)
	if start, end, found := tableRange(text, "[mcp_servers.costmaxx]"); found {
		if !isCostmaxMCPBlock(text[start:end]) {
			return "", "", fmt.Errorf("refusing to overwrite existing [mcp_servers.costmaxx] entry")
		}
		return configPath, "already installed", nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return "", "", fmt.Errorf("create Codex config directory: %w", err)
	}
	if len(data) > 0 {
		backup := fmt.Sprintf("%s.costmaxx.bak.%s", configPath, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.WriteFile(backup, data, 0600); err != nil {
			return "", "", fmt.Errorf("back up Codex config: %w", err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find CostMax binary: %w", err)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		text += "\n"
	}
	if err := os.WriteFile(configPath, []byte(text+costmaxMCPBlock(binary)), 0600); err != nil {
		return "", "", fmt.Errorf("write Codex config: %w", err)
	}
	return configPath, "installed", nil
}

func uninstallCodexMCP() (string, string, error) {
	configPath, err := codexConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate Codex config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return configPath, "not installed", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read Codex config: %w", err)
	}
	text := string(data)
	start, end, found := tableRange(text, "[mcp_servers.costmaxx]")
	if !found {
		return configPath, "not installed", nil
	}
	if !isCostmaxMCPBlock(text[start:end]) {
		return "", "", fmt.Errorf("refusing to remove a non-CostMax [mcp_servers.costmaxx] entry")
	}
	if err := os.WriteFile(configPath, []byte(strings.TrimLeft(text[:start]+text[end:], "\n")), 0600); err != nil {
		return "", "", fmt.Errorf("update Codex config: %w", err)
	}
	return configPath, "uninstalled", nil
}
