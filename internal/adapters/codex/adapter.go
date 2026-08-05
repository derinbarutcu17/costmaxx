package codex

import (
	"fmt"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/protocol"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/metrics"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
	"github.com/derinbarutcu17/costmaxx/internal/state"
)

type Adapter struct {
	cfg        *config.Config
	artStore   *artifacts.Store
	db         DB
	projector  *state.Projector
	classifier *events.Classifier
	reducers   *reducers.Registry
	metrics    *metrics.Engine
	redactor   *privacy.Redactor
	taskState  *state.TaskState
	sessionID  string
}

type DB interface {
	InsertEvent(*events.HarnessEvent) error
	GetSessionEvents(string) ([]events.HarnessEvent, error)
	SaveTaskState(string, *state.TaskState) error
	LoadTaskState(string) (*state.TaskState, error)
	InsertArtifact(*artifacts.EvidenceArtifact) error
	GetArtifact(string) (*artifacts.EvidenceArtifact, error)
	InsertReduction(*artifacts.ReductionRecord) error
	InsertSessionMetrics(string, int, int, int, int) error
	Close() error
}

func New(cfg *config.Config, artStore *artifacts.Store, db DB) *Adapter {
	return &Adapter{
		cfg:        cfg,
		artStore:   artStore,
		db:         db,
		projector:  state.NewProjector(),
		classifier: events.NewClassifier(),
		reducers:   reducers.NewRegistry(cfg),
		metrics:    metrics.NewEngine(),
		redactor:   privacy.NewRedactor(),
	}
}

func (a *Adapter) Name() string             { return "codex" }
func (a *Adapter) Version() string          { return "1.0.0" }
func (a *Adapter) SessionID() string        { return a.sessionID }
func (a *Adapter) Metrics() *metrics.Engine { return a.metrics }
func (a *Adapter) DBEvents(sessionID string) ([]events.HarnessEvent, error) {
	return a.db.GetSessionEvents(sessionID)
}

func (a *Adapter) GetArtifact(artifactID string) (*artifacts.EvidenceArtifact, error) {
	return a.db.GetArtifact(artifactID)
}

func (a *Adapter) GetArtifactStore() *artifacts.Store {
	return a.artStore
}

func (a *Adapter) Capabilities() protocol.CapabilitySet {
	return protocol.CapabilitySet{
		protocol.CapObserveToolOutput:    true,
		protocol.CapReplaceToolOutput:    false,
		protocol.CapInjectSessionContext: true,
		protocol.CapInjectPromptContext:  false,
		protocol.CapObserveCompaction:    true,
		protocol.CapRegisterLocalTools:   false,
	}
}

func (a *Adapter) Normalize(event any) (*events.HarnessEvent, error) {
	switch e := event.(type) {
	case *events.HarnessEvent:
		return e, nil
	default:
		return nil, fmt.Errorf("unsupported event type: %T", event)
	}
}

func (a *Adapter) Translate(decision *events.AdapterDecision) (any, error) {
	return decision, nil
}

func (a *Adapter) Install() error {
	return fmt.Errorf("installer not available; configure hooks manually (see packages/codex-plugin/hooks/hooks.json)")
}

func (a *Adapter) Uninstall() error {
	return fmt.Errorf("uninstaller not available; hooks must be removed manually")
}

func (a *Adapter) Doctor() (map[string]string, error) {
	return map[string]string{
		"core_binary": "OK",
		"mode":        a.cfg.Mode,
		"session":     a.sessionID,
	}, nil
}

func (a *Adapter) shouldReplace(category events.OutputCategory, reduction *artifacts.ReductionRecord) bool {
	conf, ok := a.cfg.Reduce.Confidence[string(category)]
	if !ok {
		conf = 0.5
	}
	return reduction.CompactBytes < reduction.OriginalBytes/2 && conf >= 0.7
}

func passthrough(reason string) *events.AdapterDecision {
	return &events.AdapterDecision{
		Action:   events.ActionPassthrough,
		Warnings: []string{reason},
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
