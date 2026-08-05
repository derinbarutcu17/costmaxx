package store

import (
	"encoding/json"

	"github.com/derinbarutcu17/costmaxx/internal/state"
)

func mapJSON(v any) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseJSONstate(s string) (*state.TaskState, error) {
	var ts state.TaskState
	if err := json.Unmarshal([]byte(s), &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}
