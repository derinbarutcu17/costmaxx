# CostMax (experimental active path)

CostMax provides a `costmax_run` MCP tool that executes local shell commands,
stores raw output as compressed evidence, and returns a compact summary
to reduce model-visible token usage. The full output is retrievable via the
`cmx://artifact/{id}` resource URI using MCP `resources/read`.

**When to use costmax_run:**
- The command may produce verbose output (>1000 chars)
- You need the result but not every line of output
- You want raw evidence preserved for later retrieval

**When NOT to use costmax_run:**
- The command is interactive or requires stdin
- You need the exact full output visible immediately

**Important:**
- costmax_run runs arbitrary shell commands with full process permissions
  (network, filesystem, and process access — no sandbox)
- Raw output is stored locally with SHA-256 integrity verification
- Output that matches secret patterns (API keys, tokens) is redacted before
  storage — retrieved evidence may differ from original output
- The compact summary may omit details; use `resources/read` with the
  artifact URI `cmx://artifact/{id}` to retrieve the full output
