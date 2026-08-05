package unit

import (
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/codex"
	"github.com/derinbarutcu17/costmaxx/internal/adapters/protocol"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/state"
)

type capDB struct{}

func (capDB) InsertEvent(*events.HarnessEvent) error                  { return nil }
func (capDB) GetSessionEvents(string) ([]events.HarnessEvent, error)  { return nil, nil }
func (capDB) SaveTaskState(string, *state.TaskState) error            { return nil }
func (capDB) LoadTaskState(string) (*state.TaskState, error)          { return nil, nil }
func (capDB) InsertArtifact(*artifacts.EvidenceArtifact) error        { return nil }
func (capDB) GetArtifact(string) (*artifacts.EvidenceArtifact, error) { return nil, nil }
func (capDB) InsertReduction(*artifacts.ReductionRecord) error        { return nil }
func (capDB) InsertSessionMetrics(string, int, int, int, int) error   { return nil }
func (capDB) Close() error                                            { return nil }

func TestCapabilitySet(t *testing.T) {
	cfg := config.Default()
	s, err := artifacts.NewStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a := codex.New(cfg, s, capDB{})
	caps := a.Capabilities()

	tests := []struct {
		name string
		cap  protocol.Capability
		want bool
	}{
		{"observe tool output", protocol.CapObserveToolOutput, true},
		{"inject session context", protocol.CapInjectSessionContext, true},
		{"observe compaction", protocol.CapObserveCompaction, true},
		{"replace tool output", protocol.CapReplaceToolOutput, false},
		{"inject prompt context", protocol.CapInjectPromptContext, false},
		{"register local tools", protocol.CapRegisterLocalTools, false},
	}

	for _, tt := range tests {
		got := caps[tt.cap]
		if got != tt.want {
			t.Errorf("Capability %s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
