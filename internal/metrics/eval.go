package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

type EvalMode string

const (
	EvalBaseline       EvalMode = "baseline"
	EvalCostMaxObserve EvalMode = "costmax-observe"
	EvalCostMaxActive  EvalMode = "costmax-active"
)

type TaskResults struct {
	TaskID     string       `json:"task_id"`
	Mode       EvalMode     `json:"mode"`
	Completed  bool         `json:"completed"`
	Metrics    *RunMetrics  `json:"metrics"`
	Manifest   *RunManifest `json:"manifest"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
}

type Evaluator struct {
	ResultsDir string
}

func NewEvaluator(resultsDir string) *Evaluator {
	os.MkdirAll(resultsDir, 0700)
	return &Evaluator{ResultsDir: resultsDir}
}

func (e *Evaluator) SaveResult(result *TaskResults) error {
	result.FinishedAt = time.Now()
	if result.TaskID == "" {
		result.TaskID = uuid.New().String()
	}
	result.Manifest.CreatedAt = time.Now()

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	path := filepath.Join(e.ResultsDir, fmt.Sprintf("%s-%s.json", result.Mode, result.TaskID[:8]))
	return os.WriteFile(path, data, 0600)
}

func (e *Evaluator) Compare(suiteName string, baselineResults, costmaxResults []*TaskResults) (*ComparisonReport, error) {
	report := &ComparisonReport{
		SuiteName:   suiteName,
		GeneratedAt: time.Now(),
	}

	for _, r := range baselineResults {
		report.BaselineTotal++
		if r.Completed {
			report.BaselineCompleted++
		}
	}
	for _, r := range costmaxResults {
		report.CostMaxTotal++
		if r.Completed {
			report.CostMaxCompleted++
		}
	}

	report.BaselineMedianTokens = medianTokens(baselineResults)
	report.CostMaxMedianTokens = medianTokens(costmaxResults)
	report.BaselineMedianTurns = medianTurns(baselineResults)
	report.CostMaxMedianTurns = medianTurns(costmaxResults)
	report.BaselineMedianElapsed = medianElapsed(baselineResults)
	report.CostMaxMedianElapsed = medianElapsed(costmaxResults)

	if report.BaselineMedianTokens > 0 {
		report.TokenReductionPercent = (report.BaselineMedianTokens - report.CostMaxMedianTokens) / report.BaselineMedianTokens * 100
	}

	diff := report.BaselineCompleted - report.CostMaxCompleted
	if diff <= 1 {
		report.Conclusion = "No significant completion decline detected in this sample."
	} else {
		report.Conclusion = fmt.Sprintf("Completion difference: %d tasks. Sample may be insufficient for universal claim.", diff)
	}

	return report, nil
}

func (e *Evaluator) LoadResults(dir string) ([]*TaskResults, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var results []*TaskResults
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var r TaskResults
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		results = append(results, &r)
	}
	return results, nil
}

func (e *Evaluator) GenerateReportHTML(report *ComparisonReport) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>CostMax Benchmark: %s</title>
<style>body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;max-width:800px;margin:40px auto;padding:0 20px}
h1{color:#333}.stat{margin:20px 0;padding:15px;background:#f5f5f5;border-radius:8px}
.stat-value{font-size:24px;font-weight:bold;color:#111}.label{color:#666;font-size:14px}
.metric{display:inline-block;margin:10px 20px 10px 0}
.positive{color:#22c55e}.neutral{color:#3b82f6}.warn{color:#f59e0b}
footer{margin-top:40px;color:#999;font-size:12px}</style></head>
<body>
<h1>CostMax Benchmark Report</h1>
<p>Suite: <strong>%s</strong></p>
<p>Generated: %s</p>

<h2>Completion</h2>
<div class="stat">
  <div class="metric"><div class="label">Baseline</div><div class="stat-value">%d/%d</div></div>
  <div class="metric"><div class="label">CostMax</div><div class="stat-value">%d/%d</div></div>
</div>

<h2>Context Reduction</h2>
<div class="stat">
  <div class="metric"><div class="label">Baseline median tokens</div><div class="stat-value">%.0f</div></div>
  <div class="metric"><div class="label">CostMax median tokens</div><div class="stat-value %s">%.0f</div></div>
  <div class="metric"><div class="label">Reduction</div><div class="stat-value %s">%.1f%%</div></div>
</div>

<h2>Efficiency</h2>
<div class="stat">
  <div class="metric"><div class="label">Baseline median turns</div><div class="stat-value">%.1f</div></div>
  <div class="metric"><div class="label">CostMax median turns</div><div class="stat-value">%.1f</div></div>
  <div class="metric"><div class="label">Baseline elapsed</div><div class="stat-value">%.1fs</div></div>
  <div class="metric"><div class="label">CostMax elapsed</div><div class="stat-value">%.1fs</div></div>
</div>

<h2>Conclusion</h2>
<div class="stat">%s</div>

<footer>CostMax Benchmark Runner | %s</footer>
</body></html>`,
		report.SuiteName,
		report.SuiteName,
		report.GeneratedAt.Format(time.RFC3339),
		report.BaselineCompleted, report.BaselineTotal,
		report.CostMaxCompleted, report.CostMaxTotal,
		report.BaselineMedianTokens,
		classForReduction(report.TokenReductionPercent),
		report.CostMaxMedianTokens,
		classForReduction(report.TokenReductionPercent),
		report.TokenReductionPercent,
		report.BaselineMedianTurns,
		report.CostMaxMedianTurns,
		report.BaselineMedianElapsed,
		report.CostMaxMedianElapsed,
		report.Conclusion,
		time.Now().Format(time.RFC3339),
	)
}

func medianTokens(results []*TaskResults) float64 {
	var vals []float64
	for _, r := range results {
		if r.Metrics != nil {
			vals = append(vals, float64(r.Metrics.RawTokens))
		}
	}
	return median(vals)
}

func medianTurns(results []*TaskResults) float64 {
	var vals []float64
	for _, r := range results {
		if r.Metrics != nil {
			vals = append(vals, float64(r.Metrics.Turns))
		}
	}
	return median(vals)
}

func medianElapsed(results []*TaskResults) float64 {
	var vals []float64
	for _, r := range results {
		if r.Metrics != nil {
			vals = append(vals, r.Metrics.ElapsedTime)
		}
	}
	return median(vals)
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sort.Float64s(vals)
	mid := len(vals) / 2
	if len(vals)%2 == 0 {
		return (vals[mid-1] + vals[mid]) / 2
	}
	return vals[mid]
}

func classForReduction(pct float64) string {
	if pct >= 30 {
		return "positive"
	}
	if pct >= 10 {
		return "neutral"
	}
	return "warn"
}
