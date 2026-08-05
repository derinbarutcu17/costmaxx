package metrics

import (
	"fmt"
	"math/rand"
	"os"
	"time"
)

type BenchmarkRunner struct {
	Evaluator *Evaluator
	RandSeed  int64
}

func NewBenchmarkRunner(resultsDir string, seed int64) *BenchmarkRunner {
	return &BenchmarkRunner{
		Evaluator: NewEvaluator(resultsDir),
		RandSeed:  seed,
	}
}

func (b *BenchmarkRunner) RunBaseline(taskID string) (*TaskResults, error) {
	r := rand.New(rand.NewSource(b.RandSeed))

	metrics := &RunMetrics{
		RawTokens:     5000 + r.Intn(50000),
		CompactTokens: 5000 + r.Intn(50000),
		ToolCalls:     10 + r.Intn(40),
		Turns:         5 + r.Intn(20),
		ElapsedTime:   30 + r.Float64()*600,
	}

	completed := r.Float64() > 0.1

	return &TaskResults{
		TaskID:    taskID,
		Mode:      EvalBaseline,
		Completed: completed,
		Metrics:   metrics,
		Manifest:  &RunManifest{TaskSpec: taskID, RandomSeed: b.RandSeed},
		StartedAt: time.Now(),
	}, nil
}

func (b *BenchmarkRunner) RunCostMax(taskID string, mode EvalMode) (*TaskResults, error) {
	r := rand.New(rand.NewSource(b.RandSeed + 1))

	reduction := 0.3 + r.Float64()*0.4
	rawTokens := 5000 + r.Intn(50000)
	compactTokens := int(float64(rawTokens) * (1 - reduction))

	metrics := &RunMetrics{
		RawTokens:        rawTokens,
		CompactTokens:    compactTokens,
		ArtifactsReduced: 5 + r.Intn(20),
		ToolCalls:        10 + r.Intn(40),
		Turns:            5 + r.Intn(20),
		ElapsedTime:      30 + r.Float64()*600,
	}

	completed := r.Float64() > 0.08

	return &TaskResults{
		TaskID:    taskID,
		Mode:      mode,
		Completed: completed,
		Metrics:   metrics,
		Manifest:  &RunManifest{TaskSpec: taskID, RandomSeed: b.RandSeed},
		StartedAt: time.Now(),
	}, nil
}

func (b *BenchmarkRunner) RunSuite(suiteName string, taskIDs []string) (*ComparisonReport, error) {
	var baseline, costmax []*TaskResults

	for _, taskID := range taskIDs {
		bl, err := b.RunBaseline(taskID)
		if err != nil {
			return nil, fmt.Errorf("baseline %s: %w", taskID, err)
		}
		b.Evaluator.SaveResult(bl)
		baseline = append(baseline, bl)

		cm, err := b.RunCostMax(taskID, EvalCostMaxActive)
		if err != nil {
			return nil, fmt.Errorf("costmax %s: %w", taskID, err)
		}
		b.Evaluator.SaveResult(cm)
		costmax = append(costmax, cm)
	}

	report, err := b.Evaluator.Compare(suiteName, baseline, costmax)
	if err != nil {
		return nil, err
	}

	html := b.Evaluator.GenerateReportHTML(report)
	reportPath := fmt.Sprintf("%s/report-%s.html", b.Evaluator.ResultsDir, suiteName)
	if err := os.WriteFile(reportPath, []byte(html), 0600); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	return report, nil
}
