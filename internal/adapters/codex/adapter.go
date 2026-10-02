package codex

import (
	"fmt"
	"time"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/protocol"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/metrics"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
	"github.com/derinbarutcu17/costmaxx/internal/state"
	"github.com/derinbarutcu17/costmaxx/internal/store"
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
	// taskSessionID is the session the in-memory taskState belongs to. A
	// long-lived adapter can serve several sessions; an event for a different
	// session must never reuse another session's in-memory state (objective,
	// repository, unresolved issues) — it is reloaded from the DB instead.
	taskSessionID string
	sessionID     string
}

type DB interface {
	InsertEvent(*events.HarnessEvent) error
	GetSessionEvents(string) ([]events.HarnessEvent, error)
	SaveTaskState(string, *state.TaskState) error
	LoadTaskState(string) (*state.TaskState, error)
	GetArtifact(string) (*artifacts.EvidenceArtifact, error)
	InsertSessionMetrics(string, int, int, int, int) error
	InsertLedger(*store.LedgerEntry) (bool, error)
	// RecordCall persists an artifact, its optional reduction record, and the
	// ledger row in one transaction (idempotency-keyed), so a hook can never
	// leave partial metadata behind or double-count a duplicate PostToolUse.
	// It is the ONLY persistence path for hook evidence; the store's standalone
	// InsertArtifact/InsertReduction are not exposed through the adapter.
	RecordCall(*artifacts.EvidenceArtifact, *artifacts.ReductionRecord, *store.LedgerEntry) (bool, error)
	// GetLedgerByIdempotencyKey pre-checks a duplicate PostToolUse before a
	// second artifact file is written.
	GetLedgerByIdempotencyKey(string) (*store.LedgerEntry, error)
	// Read-only inspection methods used by the hook regression tests and the
	// doctor/report tooling.
	LedgerRows(time.Time, time.Time) ([]store.LedgerEntry, error)
	ArtifactCount() (int, error)
	ReductionCount() (int, error)
	GetSessionMetrics(string) (int, int, int, int, error)
	Close() error
}

// Test/inspection wrappers: expose the concrete store's read paths through
// the adapter so integration tests can assert exactly one row per logical
// hook call and inspect what the ledger actually persisted.
func (a *Adapter) CountLedgerRows() (int, error) {
	rows, err := a.db.LedgerRows(time.Time{}, time.Time{})
	return len(rows), err
}

func (a *Adapter) CountArtifacts() (int, error)  { return a.db.ArtifactCount() }
func (a *Adapter) CountReductions() (int, error) { return a.db.ReductionCount() }
func (a *Adapter) SessionMetrics(sessionID string) (int, int, int, int, error) {
	return a.db.GetSessionMetrics(sessionID)
}
func (a *Adapter) LedgerRows() ([]store.LedgerEntry, error) {
	return a.db.LedgerRows(time.Time{}, time.Time{})
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
