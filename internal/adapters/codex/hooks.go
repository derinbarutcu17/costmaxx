package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/state"
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
	a.db.SaveTaskState(in.SessionID, ts)

	ctx := fmt.Sprintf("[CostMax] Session %s", in.SessionID)
	if len(ts.UnresolvedIssues) > 0 {
		ctx += fmt.Sprintf(" | %d unresolved", len(ts.UnresolvedIssues))
	}

	return contextOutput("SessionStart", ctx)
}

func (a *Adapter) loadOrSkip(sessionID string) *state.TaskState {
	if a.taskState != nil {
		return a.taskState
	}
	ts, err := a.db.LoadTaskState(sessionID)
	if err == nil && ts != nil {
		a.taskState = ts
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
	a.db.SaveTaskState(in.SessionID, ts)

	evt := &events.HarnessEvent{
		EventID:    fmt.Sprintf("hook-%s-%d", in.SessionID, time.Now().UnixNano()),
		Timestamp:  time.Now(),
		Harness:    "codex",
		SessionID:  in.SessionID,
		EventType:  events.EventUserPromptSubmit,
		ToolName:   "user_prompt",
		ToolOutput: in.Prompt,
	}
	a.db.InsertEvent(evt)
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

	a.metrics.RecordToolCall()

	command := ""
	if in.ToolInput != nil {
		var bi bashInput
		if err := json.Unmarshal(in.ToolInput, &bi); err == nil {
			command = bi.Command
		}
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

	var artifactID string

	if len(output) > 0 {
		if a.redactor.ContainsSecrets(output) {
			output = a.redactor.RedactOutput(output)
		}

		artifact, storeErr := a.artStore.Store([]byte(output), in.SessionID, command, exitCode)
		if storeErr == nil {
			artifactID = artifact.ArtifactID
			a.db.InsertArtifact(artifact)

			category := a.classifier.Classify(in.ToolName, command, output, exitCode, int64(len(output)))
			reducer := a.reducers.Select(category, command, exitCode, int64(len(output)))
			if reducer != nil {
				reduction, redErr := reducer.Reduce(output, artifacts.ReducerMetadata{
					Command:  command,
					ExitCode: exitCode,
					Category: string(category),
					ToolName: in.ToolName,
					Size:     int64(len(output)),
				})
				if redErr == nil {
					reduction.ArtifactID = artifact.ArtifactID
					a.db.InsertReduction(reduction)
					a.metrics.RecordReduction(
						int(reduction.OriginalBytes), int(reduction.CompactBytes),
						reduction.OriginalTokenEst, reduction.CompactTokenEst,
					)
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

			// Persist session metrics
			ms := a.metrics.Snapshot()
			a.db.InsertSessionMetrics(in.SessionID, ms.RawTokens, ms.CompactTokens, ms.ArtifactsReduced, ms.ToolCalls)

			// Update task state from tool output
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
					"artifact_id": artifactID,
				},
			}
			a.db.InsertEvent(evt)
			a.projector.ApplyEvent(ts, evt)
		}
	}

	a.db.SaveTaskState(in.SessionID, ts)
	return noopOutput()
}

func (a *Adapter) handlePreCompact(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts != nil {
		a.db.SaveTaskState(in.SessionID, ts)
	}
	return noopOutput()
}

func (a *Adapter) handlePostCompact(in HookInput) *HookOutput {
	// Persist state so SessionStart can load it on resume.
	// PostCompact cannot deliver context to the model (Codex limitation).
	if ts := a.loadOrSkip(in.SessionID); ts != nil {
		a.db.SaveTaskState(in.SessionID, ts)
	}
	return noopOutput()
}

func (a *Adapter) handleStop(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts != nil {
		if in.LastAssistantMsg != "" {
			ts.NextAction = truncate(in.LastAssistantMsg, 200)
		}
		a.db.SaveTaskState(in.SessionID, ts)
	}
	return noopOutput()
}

func (a *Adapter) handleSessionEnd(in HookInput) *HookOutput {
	ts := a.loadOrSkip(in.SessionID)
	if ts != nil {
		a.db.SaveTaskState(in.SessionID, ts)
	}
	return noopOutput()
}
