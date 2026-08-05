package state

import "time"

type FactSource string

const (
	FactObserved      FactSource = "observed"
	FactAgentReported FactSource = "agent_reported"
	FactUserStated    FactSource = "user_stated"
	FactInferred      FactSource = "inferred"
)

type StateFact struct {
	Value       string     `json:"value"`
	Source      FactSource `json:"source"`
	EventIDs    []string   `json:"event_ids,omitempty"`
	ArtifactIDs []string   `json:"artifact_ids,omitempty"`
	Confidence  float64    `json:"confidence"`
	CreatedAt   time.Time  `json:"created_at"`
	Supersedes  string     `json:"supersedes,omitempty"`
}

type TaskState struct {
	SchemaVersion      int         `json:"schema_version"`
	TaskID             string      `json:"task_id"`
	SessionIDs         []string    `json:"session_ids"`
	Repository         string      `json:"repository,omitempty"`
	Branch             string      `json:"branch,omitempty"`
	Objective          string      `json:"objective,omitempty"`
	AcceptanceCriteria []string    `json:"acceptance_criteria,omitempty"`
	Constraints        []string    `json:"constraints,omitempty"`
	Decisions          []StateFact `json:"decisions,omitempty"`
	RelevantLocations  []string    `json:"relevant_locations,omitempty"`
	Changes            []string    `json:"changes,omitempty"`
	Commands           []string    `json:"commands,omitempty"`
	TestRuns           []TestRun   `json:"test_runs,omitempty"`
	UnresolvedIssues   []string    `json:"unresolved_issues,omitempty"`
	CompletedWork      []string    `json:"completed_work,omitempty"`
	NextAction         string      `json:"next_action,omitempty"`
	EvidenceRefs       []string    `json:"evidence_refs,omitempty"`
	StateVersion       int         `json:"state_version"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

type TestRun struct {
	Command        string    `json:"command"`
	Framework      string    `json:"framework,omitempty"`
	ExitCode       int       `json:"exit_code"`
	Passed         int       `json:"passed"`
	Failed         int       `json:"failed"`
	Skipped        int       `json:"skipped,omitempty"`
	FailingTestIDs []string  `json:"failing_test_ids,omitempty"`
	ErrorLocations []string  `json:"error_locations,omitempty"`
	Duration       float64   `json:"duration,omitempty"`
	ArtifactID     string    `json:"artifact_id,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}
