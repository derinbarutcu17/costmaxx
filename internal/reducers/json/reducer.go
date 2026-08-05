package json

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/shared"
)

type Reducer struct{}

func New() *Reducer                { return &Reducer{} }
func (r *Reducer) Name() string    { return "json" }
func (r *Reducer) Version() string { return "1.0.0" }

func (r *Reducer) CanHandle(category string, command string, exitCode int, size int64) float64 {
	if category == "json" {
		return 0.85
	}
	return 0
}

func (r *Reducer) Reduce(input string, meta artifacts.ReducerMetadata) (*artifacts.ReductionRecord, error) {
	cleaned := shared.Preprocess(input)

	var compact strings.Builder
	compact.WriteString(fmt.Sprintf("Command: %s\n", meta.Command))
	compact.WriteString(fmt.Sprintf("Exit: %d\n\n", meta.ExitCode))

	var parsed any
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		compact.WriteString("<invalid JSON>\n")
		compact.WriteString(truncate(cleaned, 2000))
	} else {
		keys, values := summarize(parsed, 0)
		compact.WriteString(fmt.Sprintf("Type: %T\n", parsed))
		compact.WriteString(keys)
		if len(values) > 0 {
			compact.WriteString("\nValues:\n")
			compact.WriteString(values)
		}
		if len(cleaned) > 2000 {
			compact.WriteString(fmt.Sprintf("\nRaw JSON: %d chars. ", len(cleaned)))
			compact.WriteString("Use `costmax evidence show <id>` for full payload.\n")
		}
	}

	result := compact.String()
	return &artifacts.ReductionRecord{
		ReductionID:      fmt.Sprintf("red-json-%d", len(input)),
		ArtifactID:       meta.ToolName,
		ReducerName:      r.Name(),
		ReducerVersion:   r.Version(),
		CompactContent:   result,
		OriginalBytes:    int64(len(input)),
		CompactBytes:     int64(len(result)),
		OriginalTokenEst: len(input) / 4,
		CompactTokenEst:  len(result) / 4,
		Reason:           "JSON output summarized by structure",
	}, nil
}

func summarize(v any, depth int) (keys, values string) {
	if depth > 3 {
		return "", ""
	}
	switch val := v.(type) {
	case map[string]any:
		ks := sortedKeys(val)
		keys = fmt.Sprintf("Keys: [%s]\n", strings.Join(ks, ", "))
		for _, k := range ks {
			subK, _ := summarize(val[k], depth+1)
			if subK != "" {
				keys += fmt.Sprintf("  %s: %s", k, subK)
			}
		}
	case []any:
		keys = fmt.Sprintf("Array length: %d\n", len(val))
		if len(val) > 0 {
			if _, ok := val[0].(map[string]any); ok && isHomogeneous(val) {
				first := val[0].(map[string]any)
				fks := sortedKeys(first)
				keys += fmt.Sprintf("Keys: [%s]\n", strings.Join(fks, ", "))
				g := buildGroupSummary(val, fks)
				if g != "" {
					keys += g
				}
			} else if len(val) <= 5 {
				for i, item := range val {
					_, subV := summarize(item, depth+1)
					if subV != "" {
						values += fmt.Sprintf("  [%d]: %s\n", i, subV)
					} else {
						values += fmt.Sprintf("  [%d]: %v\n", i, item)
					}
				}
			}
		}
	default:
		return "", fmt.Sprintf("%v", val)
	}
	return
}

func sortedKeys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func isHomogeneous(vals []any) bool {
	if len(vals) < 2 {
		return false
	}
	ref := sortedKeys(vals[0].(map[string]any))
	for _, item := range vals[1:] {
		m, ok := item.(map[string]any)
		if !ok {
			return false
		}
		ks := sortedKeys(m)
		if len(ks) != len(ref) {
			return false
		}
		for i := range ks {
			if ks[i] != ref[i] {
				return false
			}
		}
	}
	return true
}

func buildGroupSummary(vals []any, allKeys []string) string {
	labelField := guessLabelField(vals, allKeys)
	var out strings.Builder
	for _, k := range allKeys {
		if k == labelField || isSensitiveKey(k) {
			continue
		}
		uniqueVals := make(map[string]bool)
		valueToLabels := make(map[string][]string)
		for _, item := range vals {
			m := item.(map[string]any)
			v, ok := m[k].(string)
			if !ok {
				uniqueVals = nil
				break
			}
			uniqueVals[v] = true
			if label, ok := m[labelField].(string); ok && label != "" {
				valueToLabels[v] = append(valueToLabels[v], label)
			}
		}
		if uniqueVals == nil || len(uniqueVals) <= 1 || len(uniqueVals) > 10 {
			continue
		}
		sortedVals := sortedStringKeys(uniqueVals)
		out.WriteString(fmt.Sprintf("  %s groups:\n", k))
		for _, v := range sortedVals {
			labels := valueToLabels[v]
			sort.Strings(labels)
			count := len(labels)
			display := labels
			suffix := ""
			if len(display) > 5 {
				display = display[:5]
				suffix = fmt.Sprintf(" and %d more", count-5)
			}
			out.WriteString(fmt.Sprintf("    %s (%d): %s%s\n", v, count, strings.Join(display, ", "), suffix))
		}
	}
	return out.String()
}

func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	return lower == "email" || lower == "emails" || lower == "id" || lower == "ids" || lower == "password" || lower == "secret" || lower == "token"
}

func guessLabelField(vals []any, allKeys []string) string {
	preferred := []string{"name", "title", "label", "username", "login", "display_name", "full_name"}
	for _, p := range preferred {
		for _, k := range allKeys {
			if strings.ToLower(k) == p {
				return k
			}
		}
	}
	// Fallback: first non-sensitive string field
	if first, ok := vals[0].(map[string]any); ok {
		for _, k := range allKeys {
			if isSensitiveKey(k) {
				continue
			}
			if _, ok := first[k].(string); ok {
				return k
			}
		}
	}
	return allKeys[0]
}

func sortedStringKeys(m map[string]bool) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...\n[truncated]"
}
