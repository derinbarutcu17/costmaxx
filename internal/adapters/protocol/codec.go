package protocol

import (
	"encoding/json"
	"fmt"
)

func MarshalDecision(d *AdapterDecision) ([]byte, error) {
	return json.Marshal(d)
}

func UnmarshalDecision(data []byte) (*AdapterDecision, error) {
	var d AdapterDecision
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("unmarshal decision: %w", err)
	}
	return &d, nil
}

// AdapterDecision is defined here to avoid circular imports with events package.
type AdapterDecision struct {
	Action             string             `json:"action"`
	ReplacementContent string             `json:"replacement_content,omitempty"`
	AdditionalContext  string             `json:"additional_context,omitempty"`
	ArtifactReferences []string           `json:"artifact_references,omitempty"`
	Warnings           []string           `json:"warnings,omitempty"`
	Metrics            map[string]float64 `json:"metrics,omitempty"`
}
