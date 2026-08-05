package metrics

import "time"

type RunManifest struct {
	RepoCommit      string    `json:"repo_commit"`
	TaskSpec        string    `json:"task_spec"`
	HiddenTestVer   string    `json:"hidden_test_version"`
	ScorerVersion   string    `json:"scorer_version"`
	Model           string    `json:"model"`
	HarnessVersion  string    `json:"harness_version"`
	ConfigDigest    string    `json:"config_digest"`
	Environment     string    `json:"environment"`
	RandomSeed      int64     `json:"random_seed"`
	StartState      string    `json:"start_state"`
	EndState        string    `json:"end_state"`
	UsageSource     string    `json:"usage_source"`
	Verification    string    `json:"verification"`
	ArtifactDigests []string  `json:"artifact_digests"`
	CreatedAt       time.Time `json:"created_at"`
}

type ComparisonReport struct {
	SuiteName             string    `json:"suite_name"`
	BaselineCompleted     int       `json:"baseline_completed"`
	CostMaxCompleted      int       `json:"costmax_completed"`
	BaselineTotal         int       `json:"baseline_total"`
	CostMaxTotal          int       `json:"costmax_total"`
	BaselineMedianTokens  float64   `json:"baseline_median_tokens"`
	CostMaxMedianTokens   float64   `json:"costmax_median_tokens"`
	TokenReductionPercent float64   `json:"token_reduction_percent"`
	BaselineMedianTurns   float64   `json:"baseline_median_turns"`
	CostMaxMedianTurns    float64   `json:"costmax_median_turns"`
	BaselineMedianElapsed float64   `json:"baseline_median_elapsed"`
	CostMaxMedianElapsed  float64   `json:"costmax_median_elapsed"`
	Conclusion            string    `json:"conclusion"`
	GeneratedAt           time.Time `json:"generated_at"`
}
