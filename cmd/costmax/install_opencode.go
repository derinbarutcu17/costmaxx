package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func opencodeConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "opencode.jsonc"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "opencode.jsonc"), nil
}

// isCostmaxOpenCodeBlock reports whether a JSONC block for the "costmaxx"
// key is shaped like the entry CostMax installs itself.
func isCostmaxOpenCodeBlock(block string) bool {
	if !strings.Contains(block, "\"type\"") || !strings.Contains(block, "\"local\"") {
		return false
	}
	if !strings.Contains(block, "\"mcp\"") {
		return false
	}
	return strings.Contains(strings.ToLower(block), "costmax")
}

// annotateJSONC marks each byte of a JSONC document as being inside a string
// literal, line comment, or block comment, and records the brace depth at
// that byte (outside strings/comments).
func annotateJSONC(text string) (inString, inLine, inBlock []bool, depth []int) {
	n := len(text)
	inString = make([]bool, n)
	inLine = make([]bool, n)
	inBlock = make([]bool, n)
	depth = make([]int, n)
	var s, lc, bc bool
	d := 0
	for i := 0; i < n; i++ {
		c := text[i]
		switch {
		case lc:
			inLine[i] = true
			if c == '\n' {
				lc = false
			}
		case bc:
			inBlock[i] = true
			if c == '*' && i+1 < n && text[i+1] == '/' {
				inBlock[i+1] = true
				i++
			}
		case s:
			inString[i] = true
			if c == '\\' && i+1 < n {
				inString[i+1] = true
				i++
			} else if c == '"' {
				s = false
			}
		case c == '"':
			s = true
			inString[i] = true
		case c == '/':
			if i+1 < n && text[i+1] == '/' {
				lc = true
				inLine[i] = true
				inLine[i+1] = true
				i++
			} else if i+1 < n && text[i+1] == '*' {
				bc = true
				inBlock[i] = true
				inBlock[i+1] = true
				i++
			}
		case c == '{':
			d++
		case c == '}':
			if d > 0 {
				d--
			}
		}
		depth[i] = d
	}
	return
}

// skipWSComments advances i past whitespace and // and /* */ comments.
func skipWSComments(text string, i, n int) int {
	for i < n {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '/' && i+1 < n {
			if text[i+1] == '/' {
				i += 2
				for i < n && text[i] != '\n' {
					i++
				}
				continue
			}
			if text[i+1] == '*' {
				i += 2
				for i+1 < n && !(text[i] == '*' && text[i+1] == '/') {
					i++
				}
				i += 2
				continue
			}
		}
		break
	}
	return i
}

// jsoncValueEnd returns the index one past the value that starts at v in a
// JSONC document: balanced objects/arrays, quoted strings, or a bare scalar.
func jsoncValueEnd(text string, v int) int {
	n := len(text)
	if v >= n {
		return v
	}
	switch text[v] {
	case '"':
		i := v + 1
		for i < n {
			if text[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if text[i] == '"' {
				return i + 1
			}
			i++
		}
		return n
	case '{', '[':
		close := byte('}')
		if text[v] == '[' {
			close = ']'
		}
		var s, lc, bc bool
		d := 0
		for i := v; i < n; i++ {
			ch := text[i]
			switch {
			case lc:
				if ch == '\n' {
					lc = false
				}
			case bc:
				if ch == '*' && i+1 < n && text[i+1] == '/' {
					i++
				}
			case s:
				if ch == '\\' && i+1 < n {
					i++
				} else if ch == '"' {
					s = false
				}
			case ch == '"':
				s = true
			case ch == text[v]:
				d++
			case ch == close:
				d--
				if d == 0 {
					return i + 1
				}
			}
		}
		return n
	}
	var lc, bc bool
	for i := v; i < n; i++ {
		ch := text[i]
		switch {
		case lc:
			if ch == '\n' {
				lc = false
			}
		case bc:
			if ch == '*' && i+1 < n && text[i+1] == '/' {
				i++
			}
		case ch == '/' && i+1 < n && text[i+1] == '/':
			lc = true
			i++
		case ch == '/' && i+1 < n && text[i+1] == '*':
			bc = true
			i++
		case ch == ',' || ch == '}':
			return i
		}
	}
	return n
}

// findJSONCKey locates the key at the given brace depth and returns the byte
// range of the key and of its value, or ok=false when absent. depth is the
// brace nesting level of the object holding the key (1 for top-level keys,
// 2 for keys of a top-level object value, ...).
func findJSONCKey(text, key string, depth int) (keyStart, valueStart, valueEnd int, ok bool) {
	n := len(text)
	var s, lc, bc bool
	d := 0
	for i := 0; i < n; i++ {
		c := text[i]
		switch {
		case lc:
			if c == '\n' {
				lc = false
			}
			continue
		case bc:
			if c == '*' && i+1 < n && text[i+1] == '/' {
				i++
			}
			continue
		case s:
			if c == '\\' && i+1 < n {
				i++
				continue
			}
			if c == '"' {
				s = false
			}
			continue
		case c == '/':
			if i+1 < n && text[i+1] == '/' {
				lc = true
				i++
			} else if i+1 < n && text[i+1] == '*' {
				bc = true
				i++
			}
			continue
		case c == '"':
			keyStart = i
			j := i + 1
			for j < n && text[j] != '"' {
				if text[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				j++
			}
			if j >= n {
				return 0, 0, 0, false
			}
			k := skipWSComments(text, j+1, n)
			if k < n && text[k] == ':' {
				if d == depth && text[i+1:j] == key {
					v := skipWSComments(text, k+1, n)
					return keyStart, v, jsoncValueEnd(text, v), true
				}
				i = k
			} else {
				i = j
			}
			continue
		case c == '{':
			d++
			continue
		case c == '}':
			if d > 0 {
				d--
			}
			continue
		}
	}
	return 0, 0, 0, false
}

// jsoncRootBraces returns the byte offsets of the top-level { and } of a
// JSONC object document, or ok=false when the document has no object root.
func jsoncRootBraces(text string) (open, close int, ok bool) {
	open = -1
	close = -1
	n := len(text)
	var s, lc, bc bool
	d := 0
	for i := 0; i < n; i++ {
		c := text[i]
		switch {
		case lc:
			if c == '\n' {
				lc = false
			}
		case bc:
			if c == '*' && i+1 < n && text[i+1] == '/' {
				i++
			}
		case s:
			if c == '\\' && i+1 < n {
				i++
			} else if c == '"' {
				s = false
			}
		case c == '"':
			s = true
		case c == '{':
			d++
			if d == 1 && open < 0 {
				open = i
			}
		case c == '}':
			d--
			if d == 0 {
				close = i
			}
		}
	}
	if open < 0 || close < 0 {
		return 0, 0, false
	}
	return open, close, true
}

// lastSignificantCharBefore returns the index of the last byte in text[:end]
// that is not whitespace and not inside a string or comment, or -1.
func lastSignificantCharBefore(text string, end int) int {
	inString, inLine, inBlock, _ := annotateJSONC(text)
	for i := end - 1; i >= 0; i-- {
		if inString[i] || inLine[i] || inBlock[i] {
			continue
		}
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return i
	}
	return -1
}

// stripJSONC removes // line comments, /* block comments */ and trailing
// commas from JSONC text so the remainder parses as strict JSON. String
// literals (including their quotes) are copied verbatim.
func stripJSONC(text string) string {
	inString, inLine, inBlock, _ := annotateJSONC(text)
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if inLine[i] || inBlock[i] {
			continue
		}
		if text[i] == ',' && !inString[i] {
			j := i + 1
			for j < len(text) && (inLine[j] || inBlock[j] || text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
				j++
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				continue
			}
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// validateJSONC parses JSONC content as strict JSON after stripping comments
// and trailing commas.
func validateJSONC(text string) bool {
	return json.Valid([]byte(stripJSONC(text)))
}

func installOpenCodeMCP() (string, string, error) {
	configPath, err := opencodeConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate opencode config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read opencode config: %w", err)
	}
	text := string(data)

	binary, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find CostMax binary: %w", err)
	}
	// entry renders the costmaxx server object; the caller supplies the
	// indentation of the first line and continuation lines nest 2 deeper.
	entry := func(indent string) string {
		return fmt.Sprintf("\"costmaxx\": {\n%s  \"type\": \"local\",\n%s  \"command\": [%q, \"mcp\"],\n%s  \"enabled\": true\n%s}",
			indent, indent, binary, indent, indent)
	}

	var newText string
	if strings.TrimSpace(text) == "" {
		newText = "{\n  \"mcp\": {\n    " + entry("    ") + "\n  }\n}\n"
	} else {
		open, close, ok := jsoncRootBraces(text)
		if !ok {
			return "", "", fmt.Errorf("opencode config is not a JSONC object")
		}
		if _, vs, ve, vok := findJSONCKey(text, "mcp", 1); vok {
			if vs >= len(text) || text[vs] != '{' {
				return "", "", fmt.Errorf("refusing to modify a non-object \"mcp\" entry in opencode config")
			}
			if ks, _, vend, cok := findJSONCKey(text[vs:ve], "costmaxx", 1); cok {
				existing := text[vs+ks : vs+vend]
				if !isCostmaxOpenCodeBlock(existing) {
					return "", "", fmt.Errorf("refusing to overwrite existing non-CostMax \"costmaxx\" entry in opencode config")
				}
				return configPath, "already installed", nil
			}
			if last := lastSignificantCharBefore(text, ve-1); last < 0 || last == vs {
				// mcp object is empty (or holds only whitespace/comments)
				newText = text[:vs+1] + "\n    " + entry("    ") + "\n  " + text[ve-1:]
			} else if text[last] == ',' {
				newText = strings.TrimRight(text[:ve-1], " \t\n\r") + "\n    " + entry("    ") + "\n  " + text[ve-1:]
			} else {
				newText = strings.TrimRight(text[:ve-1], " \t\n\r") + ",\n    " + entry("    ") + "\n  " + text[ve-1:]
			}
		} else {
			mcpBlock := "\n  \"mcp\": {\n    " + entry("    ") + "\n  }\n"
			if last := lastSignificantCharBefore(text, close); last < 0 || last == open {
				newText = text[:close] + mcpBlock + text[close:]
			} else if text[last] == ',' {
				newText = strings.TrimRight(text[:close], " \t\n\r") + mcpBlock + text[close:]
			} else {
				newText = strings.TrimRight(text[:close], " \t\n\r") + ",\n" + mcpBlock + text[close:]
			}
		}
	}

	if len(data) > 0 {
		backup := fmt.Sprintf("%s.costmaxx.bak.%s", configPath, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.WriteFile(backup, data, 0600); err != nil {
			return "", "", fmt.Errorf("back up opencode config: %w", err)
		}
	}

	if !validateJSONC(newText) {
		if len(data) > 0 {
			_ = os.WriteFile(configPath, data, 0600)
		} else {
			_ = os.Remove(configPath)
		}
		return "", "", fmt.Errorf("refusing to write invalid opencode config (result did not parse as JSON)")
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return "", "", fmt.Errorf("create opencode config directory: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(newText), 0600); err != nil {
		return "", "", fmt.Errorf("write opencode config: %w", err)
	}
	return configPath, "installed", nil
}

func uninstallOpenCodeMCP() (string, string, error) {
	configPath, err := opencodeConfigPath()
	if err != nil {
		return "", "", fmt.Errorf("locate opencode config: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return configPath, "not installed", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read opencode config: %w", err)
	}
	text := string(data)

	_, vs, ve, mok := findJSONCKey(text, "mcp", 1)
	if !mok {
		return configPath, "not installed", nil
	}
	ks, _, ce, cok := findJSONCKey(text[vs:ve], "costmaxx", 1)
	if !cok {
		return configPath, "not installed", nil
	}
	existing := text[vs+ks : vs+ce]
	if !isCostmaxOpenCodeBlock(existing) {
		return "", "", fmt.Errorf("refusing to remove a non-CostMax \"costmaxx\" entry from opencode config")
	}

	backup := fmt.Sprintf("%s.costmaxx.bak.%s", configPath, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return "", "", fmt.Errorf("back up opencode config: %w", err)
	}

	// Remove the costmaxx key (and value) plus the comma that separated it
	// from the previous property, when present.
	keyStart := vs + ks
	keyEnd := vs + ce
	delStart := keyStart
	if last := lastSignificantCharBefore(text, keyStart); last >= 0 && text[last] == ',' && last > vs {
		delStart = last
	}
	newText := text[:delStart] + text[keyEnd:]

	// If the mcp object now holds only whitespace/comments, drop the whole
	// mcp key as well.
	if ns, nvs, nve, nok := findJSONCKey(newText, "mcp", 1); nok {
		if last := lastSignificantCharBefore(newText, nve-1); last < 0 || last == nvs {
			delStart = ns
			if last := lastSignificantCharBefore(newText, ns); last >= 0 && newText[last] == ',' {
				delStart = last
			}
			newText = newText[:delStart] + newText[nve:]
		}
	}

	if !validateJSONC(newText) {
		_ = os.WriteFile(configPath, data, 0600)
		return "", "", fmt.Errorf("refusing to write invalid opencode config (result did not parse as JSON)")
	}

	if err := os.WriteFile(configPath, []byte(newText), 0600); err != nil {
		return "", "", fmt.Errorf("update opencode config: %w", err)
	}
	return configPath, "uninstalled", nil
}
