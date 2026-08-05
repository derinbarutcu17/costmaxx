package metrics

import (
	"sync"
	"time"
)

type Engine struct {
	mu      sync.Mutex
	metrics *RunMetrics
	started time.Time
}

func NewEngine() *Engine {
	return &Engine{
		metrics: &RunMetrics{
			TestOutcomes:  make(map[string]int),
			HookLatencyMs: make(map[string]float64),
		},
		started: time.Now(),
	}
}

func (e *Engine) RecordToolCall() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.ToolCalls++
}

func (e *Engine) RecordTurn() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.Turns++
}

func (e *Engine) RecordRetry() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.Retries++
}

func (e *Engine) RecordReduction(rawBytes, compactBytes, rawTokens, compactTokens int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.RawBytes += rawBytes
	e.metrics.CompactBytes += compactBytes
	e.metrics.RawTokens += rawTokens
	e.metrics.CompactTokens += compactTokens
	e.metrics.ArtifactsReduced++
}

func (e *Engine) RecordRehydration() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.EvidenceRehydrated++
}

func (e *Engine) RecordHookLatency(hook string, ms float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.HookLatencyMs[hook] = ms
}

func (e *Engine) RecordHookFailure() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.HookFailures++
}

func (e *Engine) RecordUsage(metric UsageMetric) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.Usage = append(e.metrics.Usage, metric)
}

func (e *Engine) RecordTestOutcome(name string, passed int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.TestOutcomes[name] = passed
}

func (e *Engine) SetTaskResult(result string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.TaskResult = result
}

func (e *Engine) Snapshot() *RunMetrics {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.ElapsedTime = time.Since(e.started).Seconds()
	cp := *e.metrics
	cp.Usage = append([]UsageMetric{}, e.metrics.Usage...)
	cp.TestOutcomes = make(map[string]int)
	for k, v := range e.metrics.TestOutcomes {
		cp.TestOutcomes[k] = v
	}
	return &cp
}

func (e *Engine) ReductionPercent() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.metrics.RawTokens == 0 {
		return 0
	}
	return float64(e.metrics.RawTokens-e.metrics.CompactTokens) / float64(e.metrics.RawTokens) * 100
}
