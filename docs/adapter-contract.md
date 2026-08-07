# CostMax Adapter Contract (experimental)

Every adapter converts harness-specific input into a stable CostMax event envelope and receives a standardized decision response.

## HarnessEvent

See `schemas/harness-event.schema.json`

## Codex Hook Protocol

CostMax currently only implements the Codex lifecycle hook protocol.
Hooks receive JSON on stdin and return JSON on stdout. See:

- `internal/adapters/codex/hooks.go` for the HookInput/HookOutput types
- `packages/codex-plugin/hooks/hooks.json` for the hook configuration schema

## Adapter Interface (for future harnesses)

```go
type Adapter interface {
    Name() string
    Version() string
    Capabilities() CapabilitySet
    Normalize(event any) (*HarnessEvent, error)
    Translate(decision *AdapterDecision) (any, error)
    Install() error
    Uninstall() error
    Doctor() (map[string]string, error)
}
```

## Capability Set (current)

- `can_observe_tool_output` — observe tool results without modifying
- `can_inject_session_context` — inject state block at SessionStart (not PostCompact — Codex limitation)
- `can_observe_compaction` — receive pre/post compaction events

## Supported Harnesses (current)

| Harness | Adapter | Status |
|---------|---------|--------|
| Codex CLI | `internal/adapters/codex/` | Observe mode only |

Hermes and Claude Code adapters were deleted; Codex is the only adapter.
opencode is supported via the MCP server and plugin (`costmaxx install --target opencode`), not an adapter.
