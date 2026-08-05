package hermes

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

func (a *Adapter) Name() string    { return "hermes" }
func (a *Adapter) Version() string { return a.version }

func (a *Adapter) Capabilities() protocol.CapabilitySet {
	return protocol.CapabilitySet{
		protocol.CapObserveToolOutput:    true,
		protocol.CapInjectSessionContext: true,
		protocol.CapSelectFullRequestCtx: true,
		protocol.CapRegisterLocalTools:   true,
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
	return decision, nil
}

func (a *Adapter) Install() error   { return nil }
func (a *Adapter) Uninstall() error { return nil }

func (a *Adapter) Doctor() (map[string]string, error) {
	return map[string]string{"hermes_adapter": "OK", "version": a.version}, nil
}
