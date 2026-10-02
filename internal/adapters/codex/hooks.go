package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/state"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

type HookInput struct {
	SessionID      string  `json:"session_id"`
	HookEventName  string  `json:"hook_event_name"`
	Cwd            string  `json:"cwd,omitempty"`
	TranscriptPath *string `json:"transcript_path,omitempty"`
	Model          string  `json:"model,omitempty"`
	TurnID         string  `json:"turn_id,omitempty"`
	PermissionMode string  `json:"permission_mode,omitempty"`

	// SessionStart
	Source string `json:"source,omitempty"`

	// UserPromptSubmit
	Prompt string `json:"prompt,omitempty"`

	// PreToolUse / PostToolUse
	ToolName     string          `json:"tool_name,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse json.RawMessage `json:"tool_response,omitempty"`

	// PreCompact / PostCompact
	Trigger string `json:"trigger,omitempty"`

	// Stop
	StopHookActive   bool   `json:"stop_hook_active,omitempty"`
	LastAssistantMsg string `json:"last_assistant_message,omitempty"`

	// SessionEnd
	Reason string `json:"reason,omitempty"`
}

type HookOutput struct {
	Continue           bool            `json:"continue,omitempty"`
	StopReason         string          `json:"stopReason,omitempty"`
	SystemMessage      string          `json:"systemMessage,omitempty"`
	SuppressOutput     bool            `json:"suppressOutput,omitempty"`
	Decision           string          `json:"decision,omitempty"`
	Reason             string          `json:"reason,omitempty"`
	HookSpecificOutput *SpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type SpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

type bashResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

type bashInput struct {
	Command string `json:"command"`
}

func parseTestCounts(output string, run *state.TestRun) error {
	re := regexp.MustCompile(`(\d+)\s+(passed|failed|skipped)`)
	matches := re.FindAllStringSubmatch(output, -1)
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "passed":
			run.Passed += n
		case "failed":
			run.Failed += n
		case "skipped":
			run.Skipped += n
		}
	}
	return nil
}

func noopOutput() *HookOutput {
	return &HookOutput{Continue: true}
}

// btoi converts a bool to 0/1 for session_metrics column arguments.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func contextOutput(eventName, context string) *HookOutput {
	return &HookOutput{
		Continue: true,
		HookSpecificOutput: &SpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: context,
		},
	}
}

func (a *Adapter) HandleHook(r io.Reader) *HookOutput {
	var in HookInput
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		// Fail open: a malformed hook payload must never crash the client. It
		// is still counted as a fail-open event in the ledger so error rates
		// are not silently hidden. A malformed payload carries no stable
		// identity, so each event gets its own row rather than a shared key.
		a.recordFailOpen("unknown", "malformed_payload", err)
		return noopOutput()
	}

	a.sessionID = in.SessionID

	switch in.HookEventName {
	case "SessionStart":
		return a.handleSessionStart(in)
	case "UserPromptSubmit":
		return a.handleUserPrompt(in)
	case "PreToolUse":
		return a.handlePreToolUse(in)
	case "PostToolUse":
		return a.handlePostToolUse(in)
	case "PreCompact":
		return a.handlePreCompact(in)
	case "PostCompact":
		return a.handlePostCompact(in)
	case "Stop":
		return a.handleStop(in)
	case "SessionEnd":
		return a.handleSessionEnd(in)
	default:
		return noopOutput()
	}
}

func (a *Adapter) handleSessionStart(in HookInput) *HookOutput {
	ts, err := a.db.LoadTaskState(in.SessionID)
	if err != nil || ts == nil {
		ts = a.projector.StartTask()
	}
	ts.SessionIDs = append(ts.SessionIDs, in.SessionID)
	if in.Cwd != "" {
		ts.Repository = in.Cwd
	}
	a.taskState = ts
	a.taskSessionID = in.SessionID
	a.saveState(in.SessionID, ts)

	ctx := fmt.Sprintf("[CostMax] Session %s", in.SessionID)
	if len(ts.UnresolvedIssues) > 0 {
		ctx += fmt.Sprintf(" | %d unresolved", len(ts.UnresolvedIssues))
	}

	return contextOutput("SessionStart", ctx)
}

func (a *Adapter) loadOrSkip(sessionID string) *state.TaskState {
	// The in-memory state is valid only for the session it was loaded for. A
	// different session_id in the next payload must be resolved from the DB
	// (or recognized as a brand-new session with no state), never silently
	// served the previous session's objective/repository/state.
	if a.taskState != nil && a.taskSessionID == sessionID {
		return a.taskState
	}
	ts, err := a.db.LoadTaskState(sessionID)
	if err == nil && ts != nil {
		a.taskState = ts
		a.taskSessionID = sessionID
		return ts
	}
	return nil
}

func (a *Adapter) handleUserPrompt(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts == nil {
		return noopOutput()
	}
	if in.Prompt != "" && ts.Objective == "" {
		ts.Objective = truncate(in.Prompt, 200)
	}
	a.saveState(in.SessionID, ts)

	evt := &events.HarnessEvent{
		EventID:    fmt.Sprintf("hook-%s-%d", in.SessionID, time.Now().UnixNano()),
		Timestamp:  time.Now(),
		Harness:    "codex",
		SessionID:  in.SessionID,
		EventType:  events.EventUserPromptSubmit,
		ToolName:   "user_prompt",
		ToolOutput: in.Prompt,
	}
	if err := a.db.InsertEvent(evt); err != nil {
		a.recordFailOpen(in.SessionID, "user_prompt_event", err)
	}
	return noopOutput()
}

func (a *Adapter) handlePreToolUse(in HookInput) *HookOutput {
	return noopOutput()
}

func (a *Adapter) handlePostToolUse(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts == nil {
		return noopOutput()
	}

	command := ""
	if in.ToolInput != nil {
		var bi bashInput
		if err := json.Unmarshal(in.ToolInput, &bi); err == nil {
			command = bi.Command
		}
	}
	// Commands are persisted in artifact metadata, the ledger, and the events
	// table; apply the same secret redaction policy so a secret inside a
	// command string is never stored in the clear.
	command = a.redactor.Redact(command)

	// The ledger must record the real working directory the tool ran in. The
	// PostToolUse payload carries cwd when Codex provides it; otherwise the
	// session's repository (recorded at SessionStart) is the fallback.
	cwd := a.redactor.Redact(in.Cwd)
	if cwd == "" && ts.Repository != "" {
		cwd = a.redactor.Redact(ts.Repository)
	}

	output := ""
	exitCode := 0
	if in.ToolResponse != nil {
		// Try object form: {"output": "...", "exit_code": 1}
		var br bashResponse
		if err := json.Unmarshal(in.ToolResponse, &br); err == nil {
			output = br.Output
			exitCode = br.ExitCode
		} else {
			// String form: plain stdout text (Codex Bash response)
			var s string
			if err := json.Unmarshal(in.ToolResponse, &s); err == nil {
				output = s
			}
		}
	}

	if a.redactor.ContainsSecrets(output) {
		output = a.redactor.RedactOutput(output)
	}

	// Idempotency pre-check: a duplicate PostToolUse (same session, same
	// tool_use_id) is answered without writing a second artifact file, so one
	// logical tool call produces exactly one ledger/artifact/reduction row and
	// the tool-call/reduction metrics never inflate. A payload without a
	// tool_use_id falls back to a fresh per-call reference below, so distinct
	// calls are never collapsed under a shared key.
	callRef := in.ToolUseID
	if callRef != "" {
		key := store.LedgerIdempotencyKey("codex_hook", in.SessionID, callRef)
		prior, err := a.db.GetLedgerByIdempotencyKey(key)
		if err != nil {
			a.recordFailOpen(in.SessionID, "post_tool_use_precheck", err)
			return noopOutput()
		}
		if prior != nil && prior.Outcome != store.LedgerOutcomeError {
			// Duplicate: the first PostToolUse already recorded this call.
			a.saveState(in.SessionID, ts)
			return noopOutput()
		}
	}

	category := a.classifier.Classify(in.ToolName, command, output, exitCode, int64(len(output)))

	// Empty output is a legitimate, observable call: a command that produced
	// no stdout still ran and must count in reports. There is nothing to
	// store and nothing to reduce, so the ledger row carries zero bytes and
	// zero claimed saving. Hooks are observe-only, so the model saw exactly
	// what the tool produced (nothing).
	if len(output) == 0 {
		ref := callRef
		if ref == "" {
			// No caller-supplied identity: a fresh per-call reference. A
			// nanosecond timestamp can collide between two accepted no-id
			// empty calls (same session, same nanosecond), collapsing two real
			// calls into one ledger row and inventing an undercount; a fresh
			// UUID is collision-resistant under concurrency.
			ref = uuid.New().String()
		}
		callID := "hook-empty-" + uuid.New().String()
		entry := &store.LedgerEntry{
			CallID:               callID,
			IdempotencyKey:       store.LedgerIdempotencyKey("codex_hook", in.SessionID, ref),
			EventTimestamp:       time.Now(),
			SessionID:            in.SessionID,
			Harness:              "codex_hook",
			Command:              command,
			Cwd:                  cwd,
			Category:             string(category),
			RawBytes:             0,
			ModelVisibleBytes:    0,
			RawTokenEst:          0,
			ModelVisibleTokenEst: 0,
			Recommendation:       "observe",
			FinalDecision:        "observe",
			ExitCode:             exitCode,
			HookStatus:           "observe",
			Outcome:              store.LedgerOutcomePassthrough,
		}
		inserted, err := a.db.InsertLedger(entry)
		if err != nil {
			a.recordFailOpen(in.SessionID, "post_tool_use_record_empty", err)
			return noopOutput()
		}
		if inserted {
			// In-process and persisted metrics increment only for a genuinely
			// new call, never for a duplicate retry.
			a.metrics.RecordToolCall()
			if err := a.db.InsertSessionMetrics(in.SessionID, 0, 0, 0, 1); err != nil {
				a.recordFailOpen(in.SessionID, "post_tool_use_metrics_empty", err)
			}
			// The event identity derives from the per-call UUID, never a bare
			// nanosecond timestamp: two accepted no-id calls must produce two
			// distinct event rows, and a collision must not fabricate a
			// fail-open error row.
			evt := &events.HarnessEvent{
				EventID:    "hook-" + callID,
				Timestamp:  time.Now(),
				Harness:    "codex",
				SessionID:  in.SessionID,
				EventType:  events.EventPostToolUse,
				ToolName:   in.ToolName,
				ToolOutput: "",
				ExecutionMetadata: map[string]string{
					"exit_code": fmt.Sprintf("%d", exitCode),
					"command":   command,
				},
			}
			if err := a.db.InsertEvent(evt); err != nil {
				a.recordFailOpen(in.SessionID, "post_tool_use_event_empty", err)
			} else {
				a.projector.ApplyEvent(ts, evt)
			}
		}
		a.saveState(in.SessionID, ts)
		return noopOutput()
	}

	artifact, storeErr := a.artStore.Store([]byte(output), in.SessionID, command, cwd, exitCode)
	if storeErr != nil {
		a.recordFailOpen(in.SessionID, "post_tool_use_store", storeErr)
		return noopOutput()
	}

	reducer := a.reducers.Select(category, command, exitCode, int64(len(output)))
	reduced := false
	var reduction *artifacts.ReductionRecord
	if reducer != nil {
		red, redErr := reducer.Reduce(output, artifacts.ReducerMetadata{
			Command:  command,
			ExitCode: exitCode,
			Category: string(category),
			ToolName: in.ToolName,
			Size:     int64(len(output)),
		})
		if redErr == nil {
			reduction = red
			red.ArtifactID = artifact.ArtifactID
			// Live artifacts can reduce to the same byte length more than once;
			// use a per-artifact ID so the reduction_records primary key never
			// collides and the ledger row can always resolve its reduction.
			red.ReductionID = "red-" + artifact.ArtifactID
			reduced = true
			// Populate structured test run from reducer facts
			if len(reduction.StructuredFacts) > 0 || category == "test" {
				run := state.TestRun{
					Command:        command,
					ExitCode:       exitCode,
					FailingTestIDs: reduction.StructuredFacts,
					ArtifactID:     artifact.ArtifactID,
					Timestamp:      time.Now(),
				}
				if err := parseTestCounts(output, &run); err == nil {
					a.projector.MarkTestRun(ts, run)
				}
			}
		}
	}

	// Hooks are observe-only: the model always sees the unmodified output, so
	// model-visible bytes equal raw bytes and a reduction record is never
	// "applied". The ledger still records the call so adoption reports can
	// count codex_hook observations honestly.
	outcome := store.LedgerOutcomePassthrough
	if reduced {
		outcome = store.LedgerOutcomeReductionAttempted
	}
	if callRef == "" {
		callRef = artifact.ArtifactID
	}
	rawTokens := int64(len(output) / 4)
	entry := &store.LedgerEntry{
		CallID:               "hook-" + artifact.ArtifactID,
		IdempotencyKey:       store.LedgerIdempotencyKey("codex_hook", in.SessionID, callRef),
		EventTimestamp:       time.Now(),
		SessionID:            in.SessionID,
		Harness:              "codex_hook",
		Command:              command,
		Cwd:                  cwd,
		Category:             string(category),
		ReducerName:          reducerName(reduction, reducer),
		ReducerVersion:       reducerVersion(reduction, reducer),
		RawBytes:             int64(len(output)),
		ModelVisibleBytes:    int64(len(output)),
		RawTokenEst:          rawTokens,
		ModelVisibleTokenEst: rawTokens,
		Recommendation:       "observe",
		FinalDecision:        "observe",
		ReductionAttempted:   reduced,
		ReductionApplied:     false,
		ArtifactID:           artifact.ArtifactID,
		ReductionID:          reductionIDStr(reduction),
		ExitCode:             exitCode,
		HookStatus:           "observe",
		Outcome:              outcome,
	}

	// The artifact, reduction, and ledger rows are written in one transaction;
	// a duplicate is ignored, never partially persisted, and never double
	// counted. Both the in-process metrics and the session_metrics compat row
	// are touched only for a genuinely new call, with per-call deltas — a
	// cumulative snapshot would inflate the counters on every event.
	inserted, err := a.db.RecordCall(artifact, reduction, entry)
	if err != nil {
		a.recordFailOpen(in.SessionID, "post_tool_use_record", err)
		return noopOutput()
	}
	if inserted {
		a.metrics.RecordToolCall()
		if reduced && reduction != nil {
			a.metrics.RecordReduction(
				int(reduction.OriginalBytes), int(reduction.CompactBytes),
				reduction.OriginalTokenEst, reduction.CompactTokenEst,
			)
		}
		if err := a.db.InsertSessionMetrics(in.SessionID, int(rawTokens), int(rawTokens), btoi(reduced), 1); err != nil {
			a.recordFailOpen(in.SessionID, "post_tool_use_metrics", err)
		}

		evt := &events.HarnessEvent{
			EventID:    fmt.Sprintf("hook-%s-%d", in.SessionID, time.Now().UnixNano()),
			Timestamp:  time.Now(),
			Harness:    "codex",
			SessionID:  in.SessionID,
			EventType:  events.EventPostToolUse,
			ToolName:   in.ToolName,
			ToolOutput: output,
			ExecutionMetadata: map[string]string{
				"exit_code":   fmt.Sprintf("%d", exitCode),
				"command":     command,
				"artifact_id": artifact.ArtifactID,
			},
		}
		if err := a.db.InsertEvent(evt); err != nil {
			a.recordFailOpen(in.SessionID, "post_tool_use_event", err)
		} else {
			a.projector.ApplyEvent(ts, evt)
		}
	}

	a.saveState(in.SessionID, ts)
	return noopOutput()
}

// recordFailOpen persists an error ledger row so a hook DB failure is
// observable instead of silently swallowed. It is best-effort: when the DB
// itself is the failing component, nothing more can be done.
func (a *Adapter) recordFailOpen(sessionID, stage string, cause error) {
	status := stage
	if cause != nil {
		if msg := truncate(cause.Error(), 200); msg != "" {
			status = stage + ": " + msg
		}
	}
	nanos := time.Now().UnixNano()
	_, _ = a.db.InsertLedger(&store.LedgerEntry{
		CallID:         fmt.Sprintf("hook-failopen-%d", nanos),
		IdempotencyKey: store.LedgerIdempotencyKey("codex_hook", sessionID, fmt.Sprintf("failopen-%d", nanos)),
		EventTimestamp: time.Now(),
		SessionID:      sessionID,
		Harness:        "codex_hook",
		HookStatus:     "fail_open",
		ErrorStatus:    status,
		Outcome:        store.LedgerOutcomeError,
	})
}

// saveState persists task state, recording a fail-open ledger row when the
// DB write fails so the error is observable rather than silently dropped.
func (a *Adapter) saveState(sessionID string, ts *state.TaskState) {
	if err := a.db.SaveTaskState(sessionID, ts); err != nil {
		a.recordFailOpen(sessionID, "save_task_state", err)
	}
}

func reducerName(r *artifacts.ReductionRecord, reducer artifacts.Reducer) string {
	if r != nil {
		return r.ReducerName
	}
	if reducer != nil {
		return reducer.Name()
	}
	return ""
}

func reducerVersion(r *artifacts.ReductionRecord, reducer artifacts.Reducer) string {
	if r != nil {
		return r.ReducerVersion
	}
	if reducer != nil {
		return reducer.Version()
	}
	return ""
}

func reductionIDStr(r *artifacts.ReductionRecord) string {
	if r == nil {
		return ""
	}
	return r.ReductionID
}

func (a *Adapter) handlePreCompact(in HookInput) *HookOutput {
	if ts := a.loadOrSkip(in.SessionID); ts != nil {
		a.saveState(in.SessionID, ts)
	}
	return noopOutput()
}

func (a *Adapter) handlePostCompact(in HookInput) *HookOutput {
	// Persist state so SessionStart can load it on resume.
	// PostCompact cannot deliver context to the model (Codex limitation).
	if ts := a.loadOrSkip(in.SessionID); ts != nil {
		a.saveState(in.SessionID, ts)
	}
	return noopOutput()
}

func (a *Adapter) handleStop(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts != nil {
		if in.LastAssistantMsg != "" {
			ts.NextAction = truncate(in.LastAssistantMsg, 200)
		}
		a.saveState(in.SessionID, ts)
	}
	return noopOutput()
}

func (a *Adapter) handleSessionEnd(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts != nil {
		a.saveState(in.SessionID, ts)
	}
	return noopOutput()
}
