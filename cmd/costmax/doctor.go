package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/derinbarutcu17/costmaxx/internal/mcp"
)

// optionalChecks are doctor checks reported for information but not treated
// as setup failures when missing. opencode_mcp_config is optional because
// CostMax is fully functional with only the Codex MCP entry installed.
var optionalChecks = map[string]bool{"opencode_mcp_config": true}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verify CostMax's Codex MCP setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		results := doctorResults()
		failed := false
		keys := make([]string, 0, len(results))
		for key := range results {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := results[key]
			status := "✓"
			if val != "OK" {
				if !optionalChecks[key] {
					failed = true
				}
				status = "✗"
			}
			fmt.Printf("%-25s %s  %s\n", key, status, val)
		}
		if failed {
			fmt.Printf("\nRemediation: run %q, then rerun %q.\n", "costmax install", "costmax doctor")
			return fmt.Errorf("CostMax doctor found setup problems")
		}
		return nil
	},
}

func doctorResults() map[string]string {
	results := map[string]string{}
	if binary, err := os.Executable(); err != nil {
		results["binary"] = err.Error()
	} else if info, err := os.Stat(binary); err != nil || info.Mode()&0111 == 0 {
		results["binary"] = "not executable"
	} else {
		results["binary"] = "OK"
	}

	configPath, err := codexConfigPath()
	if err != nil {
		results["codex_mcp_config"] = err.Error()
	} else if data, readErr := os.ReadFile(configPath); readErr != nil {
		results["codex_mcp_config"] = "not installed"
	} else if start, end, found := tableRange(string(data), "[mcp_servers.costmaxx]"); !found || !isCostmaxMCPBlock(string(data)[start:end]) {
		results["codex_mcp_config"] = "not installed"
	} else {
		results["codex_mcp_config"] = "OK"
	}

	if ocPath, err := opencodeConfigPath(); err != nil {
		results["opencode_mcp_config"] = err.Error()
	} else if data, readErr := os.ReadFile(ocPath); readErr != nil {
		results["opencode_mcp_config"] = "not installed"
	} else {
		text := string(data)
		if _, vs, ve, found := findJSONCKey(text, "mcp", 1); !found {
			results["opencode_mcp_config"] = "not installed"
		} else if ks, _, vend, found := findJSONCKey(text[vs:ve], "costmaxx", 1); !found || !isCostmaxOpenCodeBlock(text[vs+ks:vs+vend]) {
			results["opencode_mcp_config"] = "not installed"
		} else {
			results["opencode_mcp_config"] = "OK"
		}
	}

	probe, err := os.CreateTemp(cfg.Store.ArtifactDir, ".costmax-doctor-*")
	if err != nil {
		results["artifact_store"] = err.Error()
	} else {
		probe.Close()
		os.Remove(probe.Name())
		results["artifact_store"] = "OK"
	}

	server, err := mcp.NewServer(cfg)
	if err != nil {
		results["mcp_handshake"] = err.Error()
		return results
	}
	defer server.Close()
	var out bytes.Buffer
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"costmax-doctor","version":"1"}}}` + "\n"
	if err := server.Serve(strings.NewReader(request), &out); err != nil || !strings.Contains(out.String(), `"serverInfo"`) {
		results["mcp_handshake"] = "failed"
	} else {
		results["mcp_handshake"] = "OK"
	}
	return results
}
