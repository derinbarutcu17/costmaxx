package events

import "time"

type EventType string

const (
	EventSessionStart     EventType = "session_start"
	EventUserPromptSubmit EventType = "user_prompt_submit"
	EventPreToolUse       EventType = "pre_tool_use"
	EventPostToolUse      EventType = "post_tool_use"
	EventPreCompact       EventType = "pre_compact"
	EventPostCompact      EventType = "post_compact"
	EventStop             EventType = "stop"
	EventSessionEnd       EventType = "session_end"
)

type HarnessEvent struct {
	EventID           string            `json:"event_id"`
	Timestamp         time.Time         `json:"timestamp"`
	Harness           string            `json:"harness"`
	HarnessVersion    string            `json:"harness_version"`
	AdapterVersion    string            `json:"adapter_version"`
	SessionID         string            `json:"session_id"`
	Repository        string            `json:"repository,omitempty"`
	EventType         EventType         `json:"event_type"`
	ToolName          string            `json:"tool_name,omitempty"`
	ToolInput         map[string]any    `json:"tool_input,omitempty"`
	ToolOutput        string            `json:"tool_output,omitempty"`
	ExecutionMetadata map[string]string `json:"execution_metadata,omitempty"`
	CapabilityFlags   map[string]bool   `json:"capability_flags,omitempty"`
}

type AdapterAction string

const (
	ActionPassthrough AdapterAction = "passthrough"
	ActionReplace     AdapterAction = "replace"
	ActionInject      AdapterAction = "inject"
	ActionBlock       AdapterAction = "block"
)

type AdapterDecision struct {
	Action             AdapterAction      `json:"action"`
	ReplacementContent string             `json:"replacement_content,omitempty"`
	AdditionalContext  string             `json:"additional_context,omitempty"`
	ArtifactReferences []string           `json:"artifact_references,omitempty"`
	Warnings           []string           `json:"warnings,omitempty"`
	Metrics            map[string]float64 `json:"metrics,omitempty"`
}

type OutputCategory string

const (
	OutputTest     OutputCategory = "test"
	OutputBuild    OutputCategory = "build"
	OutputTerminal OutputCategory = "terminal"
	OutputDiff     OutputCategory = "diff"
	OutputSearch   OutputCategory = "search"
	OutputLint     OutputCategory = "lint"
	OutputJSON     OutputCategory = "json"
	OutputGeneric  OutputCategory = "generic"
	OutputBinary   OutputCategory = "binary"
)
