package claude

import (
	"github.com/derinbarutcu17/costmaxx/internal/adapters/protocol"
	"github.com/derinbarutcu17/costmaxx/internal/events"
)

type Adapter struct {
	version string
}

func New() *Adapter {
	return &Adapter{version: "1.0.0"}
}

func (a *Adapter) Name() string    { return "claude-code" }
func (a *Adapter) Version() string { return a.version }

func (a *Adapter) Capabilities() protocol.CapabilitySet {
	return protocol.CapabilitySet{
		protocol.CapObserveToolOutput: true,
		protocol.CapReplaceToolOutput: true,
	}
}

func (a *Adapter) Normalize(event any) (*events.HarnessEvent, error) {
	switch e := event.(type) {
	case *events.HarnessEvent:
		return e, nil
	default:
		return nil, nil
	}
}

func (a *Adapter) Translate(decision *events.AdapterDecision) (any, error) {
	return map[string]any{
		"type":    "tool_result",
		"content": decision.ReplacementContent,
		"replace": decision.Action == events.ActionReplace,
	}, nil
}

func (a *Adapter) Install() error   { return nil }
func (a *Adapter) Uninstall() error { return nil }

func (a *Adapter) Doctor() (map[string]string, error) {
	return map[string]string{"claude_code_adapter": "OK", "version": a.version}, nil
}
