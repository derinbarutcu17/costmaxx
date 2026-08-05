package protocol

import (
	"github.com/derinbarutcu17/costmaxx/internal/events"
)

type Capability string

const (
	CapObserveToolOutput    Capability = "can_observe_tool_output"
	CapReplaceToolOutput    Capability = "can_replace_tool_output"
	CapInjectSessionContext Capability = "can_inject_session_context"
	CapInjectPromptContext  Capability = "can_inject_prompt_context"
	CapObserveCompaction    Capability = "can_observe_compaction"
	CapSelectFullRequestCtx Capability = "can_select_full_request_context"
	CapReportActualTokens   Capability = "can_report_actual_tokens"
	CapRegisterLocalTools   Capability = "can_register_local_tools"
)

type CapabilitySet map[Capability]bool

type Adapter interface {
	Name() string
	Version() string
	Capabilities() CapabilitySet
	Normalize(event any) (*events.HarnessEvent, error)
	Translate(decision *events.AdapterDecision) (any, error)
	Install() error
	Uninstall() error
	Doctor() (map[string]string, error)
}
