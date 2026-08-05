# Benchmark Methodology

## Three Layers

**Layer A — Controlled compression tasks:** Synthetic tasks where decisive fact appears in large output.

**Layer B — Fresh repository tasks:** 20-50 tasks across pinned repos with newly written bugs, mutated existing bugs, hidden tests, clear acceptance criteria.

**Layer C — Audited external tasks:** Manually audited subset of external benchmarks.

## Experimental Method

Per task:
1. Reset to same commit
2. Same harness/model/config/permissions/time budget
3. Randomize baseline/CostMax order
4. Multiple repetitions
5. Preserve manifests
6. Score with tests/verifier
7. Separate unsuccessful from infrastructure failures

## Compared Metrics

- Completion rate
- Regression-test rate
- Input/cached-input/output tokens
- Model-visible context
- Wall time
- Agent turns
- Tool calls
- Evidence rehydrations
- Hook overhead

## Statistical Claim (Public Gate)

- **Lower bound:** >=30% median reduction in eligible model-visible context
- **Quality:** Completion-rate difference within 5pp non-inferiority margin
- **Reliability:** No unrecoverable evidence loss
