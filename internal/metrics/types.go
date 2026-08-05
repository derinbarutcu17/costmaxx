package metrics

import "time"

type Quality string

const (
	QualityExact           Quality = "exact"
	QualityHarnessReported Quality = "harness_reported"
	QualityEstimated       Quality = "estimated"
	QualityUnavailable     Quality = "unavailable"
)

type UsageMetric struct {
	Name      string    `json:"name"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Source    string    `json:"source"`
	Quality   Quality   `json:"quality"`
	Timestamp time.Time `json:"timestamp"`
}

type RunMetrics struct {
	RawBytes           int                `json:"raw_bytes"`
	CompactBytes       int                `json:"compact_bytes"`
	RawTokens          int                `json:"raw_tokens"`
	CompactTokens      int                `json:"compact_tokens"`
	ArtifactsReduced   int                `json:"artifacts_reduced"`
	EvidenceRehydrated int                `json:"evidence_rehydrated"`
	ToolCalls          int                `json:"tool_calls"`
	Turns              int                `json:"turns"`
	Retries            int                `json:"retries"`
	TestOutcomes       map[string]int     `json:"test_outcomes,omitempty"`
	ElapsedTime        float64            `json:"elapsed_time"`
	HookLatencyMs      map[string]float64 `json:"hook_latency_ms,omitempty"`
	HookFailures       int                `json:"hook_failures"`
	TaskResult         string             `json:"task_result,omitempty"`
	Usage              []UsageMetric      `json:"usage,omitempty"`
}
