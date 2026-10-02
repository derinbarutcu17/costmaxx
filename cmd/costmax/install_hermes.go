package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// hermesConfigPath returns the Hermes config.yaml path, honoring HERMES_HOME
// when set and otherwise defaulting to ~/.hermes/config.yaml.
func hermesConfigPath() (string, error) {
	hermesHome := os.Getenv("HERMES_HOME")
	if hermesHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		hermesHome = filepath.Join(home, ".hermes")
	}
	return filepath.Join(hermesHome, "config.yaml"), nil
}

// hermesMCPBlock renders the costmaxx server entry placed under the top-level
// mcp_servers key. indent is the indentation of the server-name line (children
// nest two spaces deeper), matching the block style Hermes itself documents.
func hermesMCPBlock(binary, indent string) string {
	return fmt.Sprintf("%scostmaxx:\n%s  command: %s\n%s  args: [\"mcp\"]\n%s  enabled: true\n",
		indent, indent, quoteYAML(binary), indent, indent)
}

// isCostmaxHermesBlock reports whether a block of YAML text describing the
// "costmaxx" server is shaped like the entry CostMax installs itself: it runs
// a CostMax-named binary via `costmaxx mcp`. The exact executable path is not
// part of the identity so an upgrade or relocation can safely be re-installed.
func isCostmaxHermesBlock(block, binary string) bool {
	var entries map[string]struct {
		Command string   `yaml:"command"`
		Args    []string `yaml:"args"`
	}
	if err := yaml.Unmarshal([]byte(block), &entries); err != nil {
		return false
	}
	entry, ok := entries["costmaxx"]
	if !ok || len(entry.Args) != 1 || entry.Args[0] != "mcp" || entry.Command == "" {
		return false
	}
	command := entry.Command
	if command == binary {
		return true
	}
	base := strings.ToLower(filepath.Base(command))
	return base == "costmax" || base == "costmaxx"
}

// quoteYAML returns a JSON-compatible double-quoted YAML scalar. JSON escapes
// are a strict subset of YAML's double-quoted scalar escapes and correctly
// handle spaces, quotes, and backslashes in executable paths.
func quoteYAML(s string) string {
	return strconv.Quote(s)
}

// validateHermesConfig parses the complete YAML document before any mutation.
// CostMax only edits a top-level mapping with a mapping-valued mcp_servers key.
// Flow maps with entries, nulls, sequences, duplicate keys, and multi-document
// configs are refused because line-based insertion cannot preserve them safely.
// It returns the zero-based source line for mcp_servers, or -1 when absent.
func validateHermesConfig(text string) (int, error) {
	if strings.TrimSpace(text) == "" {
		return -1, nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return -1, fmt.Errorf("refusing to modify invalid Hermes YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return -1, fmt.Errorf("refusing to modify Hermes config with multiple YAML documents")
		}
		return -1, fmt.Errorf("refusing to modify invalid Hermes YAML: %w", err)
	}
	if len(document.Content) == 0 {
		return -1, nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return -1, fmt.Errorf("refusing to modify Hermes config: top level must be a YAML mapping")
	}
	keyLine := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Value != "mcp_servers" {
			continue
		}
		if keyLine >= 0 {
			return -1, fmt.Errorf("refusing to modify Hermes config: duplicate mcp_servers keys")
		}
		value := root.Content[i+1]
		if value.Kind != yaml.MappingNode {
			return -1, fmt.Errorf("refusing to modify Hermes config: mcp_servers must be a YAML mapping (not %s)", yamlNodeKind(value.Kind))
		}
		if value.Style&yaml.FlowStyle != 0 && len(value.Content) > 0 {
			return -1, fmt.Errorf("refusing to modify Hermes config: non-empty flow-map mcp_servers is not supported; expand it to block YAML first")
		}
		seenServers := make(map[string]bool)
		for j := 0; j+1 < len(value.Content); j += 2 {
			serverKey := value.Content[j]
			if serverKey.Kind != yaml.ScalarNode || serverKey.Tag != "!!str" {
				return -1, fmt.Errorf("refusing to modify Hermes config: mcp_servers contains a non-string server key")
			}
			if seenServers[serverKey.Value] {
				return -1, fmt.Errorf("refusing to modify Hermes config: duplicate mcp_servers entry %q", serverKey.Value)
			}
			seenServers[serverKey.Value] = true
		}
		keyLine = key.Line - 1
	}
	return keyLine, nil
}

func yamlNodeKind(kind yaml.Kind) string {
	switch kind {
	case yaml.SequenceNode:
		return "a sequence"
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "an unsupported value"
	}
}

// lineIndent returns the leading whitespace run of a line.
func lineIndent(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// topLevelKeyIndex returns the index of the first top-level YAML mapping key
// named key (a line at column 0 whose trimmed content starts with key:), or -1.
func topLevelKeyIndex(lines []string, key string) int {
	for i, line := range lines {
		if lineIndent(line) != "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if yamlTopLevelKeyName(trimmed) == key {
			return i
		}
	}
	return -1
}

func yamlTopLevelKeyName(line string) string {
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	if strings.HasPrefix(line, "\"") {
		if end := strings.Index(line[1:], "\":"); end >= 0 {
			return line[1 : 1+end]
		}
	}
	if strings.HasPrefix(line, "'") {
		if end := strings.Index(line[1:], "':"); end >= 0 {
			return line[1 : 1+end]
		}
	}
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return ""
	}
	return strings.TrimSpace(line[:colon])
}

// yamlBlockEnd returns the index one past the last line belonging to the
// mapping value that starts at lines[keyIdx] (whose key is a top-level line):
// every following line that is blank or indented is part of the block.
func yamlBlockEnd(lines []string, keyIdx int) int {
	i := keyIdx + 1
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if lineIndent(lines[i]) == "" {
			break
		}
	}
	return i
}

// normalizeEmptyHermesMCPMap keeps an empty top-level mcp_servers value a
// valid mapping after uninstall. A bare `mcp_servers:` is YAML null, which
// would make the next doctor/install run refuse the otherwise valid config.
func normalizeEmptyHermesMCPMap(lines []string, keyIdx int) {
	end := yamlBlockEnd(lines, keyIdx)
	for i := keyIdx + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return
		}
	}
	line := lines[keyIdx]
	comment := ""
	if hash := strings.Index(line, "#"); hash >= 0 {
		comment = line[hash:]
		line = line[:hash]
	}
	line = strings.TrimRight(line, " \t")
	if strings.HasSuffix(line, ":") {
		line += " {}"
	}
	if comment != "" {
		line += " " + strings.TrimSpace(comment)
	}
	lines[keyIdx] = line
}

// serverIndentInBlock returns the indentation of the first child entry in a
// mapping block (the server-name level), defaulting to 2 spaces.
func serverIndentInBlock(lines []string, start, end int) string {
	for i := start; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		ind := lineIndent(lines[i])
		if ind != "" {
			return ind
		}
	}
	return "  "
}

// serverEntryRangeInBlock locates a server entry named name inside the block
// lines[start:end]. It returns the range [entryStart, entryEnd) of the entry's
// lines (name line plus its nested sub-keys), or ok=false when absent. Only
// entries at the block's first-level (server-name) indentation are considered.
func serverEntryRangeInBlock(lines []string, start, end int, name string, serverIndent string) (int, int, bool) {
	n := len(serverIndent)
	for i := start; i < end; i++ {
		ind := lineIndent(lines[i])
		if ind != serverIndent {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if yamlTopLevelKeyName(trimmed) != name {
			continue
		}
		// Consume nested sub-keys (deeper indentation) belonging to this entry.
		j := i + 1
		for ; j < end; j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if len(lineIndent(lines[j])) <= n {
				break
			}
		}
		return i, j, true
	}
	return 0, 0, false
}

func installHermesMCP() (string, string, error) {
	configPath, err := hermesConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate Hermes config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read Hermes config: %w", err)
	}

	binary, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find CostMax binary: %w", err)
	}

	text := string(data)
	var newText string
	if strings.TrimSpace(text) == "" {
		// Fresh (or empty) config: create it with just the mcp_servers block.
		newText = "mcp_servers:\n" + hermesMCPBlock(binary, "  ")
	} else {
		newText, err = insertHermesMCPBlock(text, binary)
		if err != nil {
			return "", "", err
		}
		if newText == text {
			return configPath, "already installed", nil
		}
	}
	if len(data) > 0 {
		if _, err := backupConfig(configPath, data); err != nil {
			return "", "", fmt.Errorf("back up Hermes config: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return "", "", fmt.Errorf("create Hermes config directory: %w", err)
	}
	if err := writeConfigAtomic(configPath, []byte(newText)); err != nil {
		return "", "", fmt.Errorf("write Hermes config: %w", err)
	}
	return configPath, "installed", nil
}

// insertHermesMCPBlock returns the new config text with the costmaxx server
// merged under the top-level mcp_servers key, or an error when the block
// already exists in a foreign (non-CostMax) shape. It returns the original
// text unchanged when costmaxx is already installed.
func insertHermesMCPBlock(text string, binary string) (string, error) {
	lines := strings.Split(text, "\n")
	keyIdx, err := validateHermesConfig(text)
	if err != nil {
		return "", err
	}
	if keyIdx < 0 {
		// No mcp_servers key: append it at the end, ensuring a blank line
		// separates it from any existing top-level content.
		var b strings.Builder
		b.WriteString(text)
		if text != "" && !strings.HasSuffix(text, "\n") {
			b.WriteString("\n")
		}
		if text != "" && !strings.HasSuffix(text, "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString("mcp_servers:\n")
		b.WriteString(hermesMCPBlock(binary, "  "))
		newText := b.String()
		if _, err := validateHermesConfig(newText); err != nil {
			return "", fmt.Errorf("refusing to write invalid Hermes YAML: %w", err)
		}
		return newText, nil
	}

	blockStart := keyIdx
	blockEnd := yamlBlockEnd(lines, keyIdx)
	serverIndent := serverIndentInBlock(lines, blockStart+1, blockEnd)

	// Handle inline empty form: "mcp_servers: {}".
	trimmedKey := strings.TrimSpace(lines[keyIdx])
	if strings.Contains(trimmedKey, "{}") {
		lines[keyIdx] = strings.Replace(trimmedKey, "{}", "", 1)
		if blockEnd > keyIdx+1 {
			blockEnd = keyIdx + 1
		}
	}

	if es, ee, ok := serverEntryRangeInBlock(lines, blockStart+1, blockEnd, "costmaxx", serverIndent); ok {
		entryText := strings.Join(lines[es:ee], "\n")
		if !isCostmaxHermesBlock(entryText, binary) {
			return "", fmt.Errorf("refusing to overwrite existing non-CostMax \"costmaxx\" mcp_servers entry in Hermes config")
		}
		return text, nil
	}

	// Append the costmaxx entry at the end of the mcp_servers block, adding a
	// blank separator line before it when the block already holds servers.
	insert := ""
	if blockEnd > keyIdx+1 && strings.TrimSpace(lines[blockEnd-1]) != "" {
		insert = "\n"
	}
	insert += hermesMCPBlock(binary, serverIndent)

	head := append([]string{}, lines[:blockEnd]...)
	tail := lines[blockEnd:]
	newLines := append(head, strings.Split(insert, "\n")...)
	newLines = append(newLines, tail...)
	newText := strings.Join(newLines, "\n")
	if _, err := validateHermesConfig(newText); err != nil {
		return "", fmt.Errorf("refusing to write invalid Hermes YAML: %w", err)
	}
	return newText, nil
}

func uninstallHermesMCP() (string, string, error) {
	configPath, err := hermesConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate Hermes config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return configPath, "not installed", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read Hermes config: %w", err)
	}
	text := string(data)

	lines := strings.Split(text, "\n")
	keyIdx, err := validateHermesConfig(text)
	if err != nil {
		return "", "", err
	}
	if keyIdx < 0 {
		return configPath, "not installed", nil
	}
	blockEnd := yamlBlockEnd(lines, keyIdx)
	serverIndent := serverIndentInBlock(lines, keyIdx+1, blockEnd)
	es, ee, ok := serverEntryRangeInBlock(lines, keyIdx+1, blockEnd, "costmaxx", serverIndent)
	if !ok {
		return configPath, "not installed", nil
	}
	entryText := strings.Join(lines[es:ee], "\n")
	binary, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find CostMax binary: %w", err)
	}
	if !isCostmaxHermesBlock(entryText, binary) {
		return "", "", fmt.Errorf("refusing to remove a non-CostMax \"costmaxx\" mcp_servers entry from Hermes config")
	}

	if _, err := backupConfig(configPath, data); err != nil {
		return "", "", fmt.Errorf("back up Hermes config: %w", err)
	}

	// Remove the entry lines. Drop a single preceding blank separator line so
	// the block doesn't accumulate empty gaps.
	newLines := make([]string, 0, len(lines))
	newLines = append(newLines, lines[:es]...)
	if es > keyIdx && strings.TrimSpace(lines[es-1]) == "" {
		newLines = newLines[:len(newLines)-1]
	}
	newLines = append(newLines, lines[ee:]...)
	normalizeEmptyHermesMCPMap(newLines, keyIdx)
	newText := strings.Join(newLines, "\n")
	if _, err := validateHermesConfig(newText); err != nil {
		return "", "", fmt.Errorf("refusing to write invalid Hermes YAML: %w", err)
	}

	if err := writeConfigAtomic(configPath, []byte(newText)); err != nil {
		return "", "", fmt.Errorf("update Hermes config: %w", err)
	}
	return configPath, "uninstalled", nil
}
