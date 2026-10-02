# Installing CostMax

CostMax is one binary, multiple integrations. Everything is local; every write
to an existing config happens with a backup first.

## Prereqs

- A `costmaxx` binary on `PATH`. From source (the package dir is `costmax`;
  install it under the `costmaxx` name the rest of the docs use):
  `go install github.com/derinbarutcu17/costmaxx/cmd/costmax@latest && mv "$(go env GOPATH)/bin/costmax" "$(go env GOPATH)/bin/costmaxx"`,
  or grab a release binary from the
  [Releases](https://github.com/derinbarutcu17/costmaxx/releases) page.
- Storage defaults to `~/.costmax/` (SQLite metadata + content-addressed
  zstd artifacts), created with user-only permissions.

## Codex

```bash
costmaxx install
costmaxx doctor
```

`costmaxx install` writes this block to `~/.codex/config.toml`
(backing up the file first — the backup is placed beside the config):

```toml
[mcp_servers.costmaxx]
command = "/path/to/costmaxx"
args = ["mcp"]
```

`costmax_run` is then available to Codex as an MCP tool.

### Optional: observe-only hooks

`packages/codex-plugin/hooks/hooks.json` installs read-only lifecycle hooks
(SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PreCompact, Stop,
SessionEnd). They record evidence and session state but never replace tool
output. If you want them:

1. Copy the file: `cp packages/codex-plugin/hooks/hooks.json ~/.codex/hooks.json`
2. Restart Codex. Verify with `costmaxx status` and `costmaxx report`.

### Verify

```bash
costmaxx doctor    # binary, config, storage, MCP handshake
costmaxx status    # process-local metrics
```

### Uninstall

```bash
costmaxx uninstall   # removes only the costmaxx MCP block it added
```

Hooks, if installed, are removed by deleting `~/.codex/hooks.json`.

## opencode

```bash
costmaxx install --target opencode
```

Registers the MCP server block in your opencode config (`opencode.jsonc`,
backed up first). The tool appears as `costmaxx_costmax_run`.

### Scope and safety of `costmaxx install`

`costmaxx install` and `costmaxx uninstall` modify **exactly one config file**
— the named MCP config entry for Codex (`~/.codex/config.toml`), opencode
(`opencode.jsonc`), or Hermes (`config.yaml`). When an existing file is
changed, install/uninstall also creates one timestamped backup beside it:

- the existing file is backed up first (`*.costmaxx.bak.<UTC-nanoseconds>`);
- an existing non-CostMax entry with the same name is **never overwritten or
  removed** — the command refuses and explains why;
- hooks, plugins, and any other config are never installed or modified;
- uninstall removes only the entry CostMax added.

### Auto-compression plugin: not shipped

A companion opencode TypeScript plugin that auto-compresses bash tool results
(`~/.config/opencode/plugins/costmaxx.ts`, threshold `COSTMAX_COMPRESS_THRESHOLD`
or `[reduce] threshold`, kill switch `COSTMAX_DISABLE=1`) is **documented but
not shipped in this repository** — no plugin file exists under `packages/` or
the repo root. Until you install such a plugin yourself, opencode compresses
only when the model calls `costmaxx_costmax_run` (or you pipe output through
`costmaxx artifact add`). A third-party plugin is external to CostMax and is
not covered by this repo's evidence-integrity guarantees.

### Verify

```bash
costmaxx doctor   # checks artifact store, binary, codex + opencode + hermes config, handshake
```

### Uninstall

1. Remove the MCP block: `costmaxx uninstall --target opencode`
2. If you installed the (external, non-shipped) auto-compression plugin
   yourself, remove it the same way you added it:
   `rm ~/.config/opencode/plugins/costmaxx.ts`

## Gemini CLI

```bash
gemini mcp add costmaxx /path/to/costmaxx mcp --scope user --trust
```

`--scope user` makes it available across projects; `--trust` skips the
per-project approval prompt. The `/path/to/costmaxx` must be absolute.

### Verify

```bash
gemini mcp list | grep costmaxx
```

### Uninstall

```bash
gemini mcp remove costmaxx --scope user
```

## Hermes

```bash
costmaxx install --target hermes
```

Registers a `costmaxx` server under the top-level `mcp_servers:` key in Hermes
config (`config.yaml`, `HERMES_HOME` if set, else `~/.hermes/config.yaml`;
backed up first). On the next Hermes restart the stdio MCP server is launched.
Hermes' tools list names it `costmaxx:costmax_run`; the model-facing namespace
is `mcp__costmaxx__costmax_run`.

Hermes is **MCP client support**, not a bespoke adapter: no adapter code runs
in Hermes, and MCP use is opt-in — Hermes only compresses when the model calls
`mcp__costmaxx__costmax_run` (or the equivalent `costmaxx:costmax_run` handle)
explicitly. Hermes has no Codex-style lifecycle hooks.

### Verify

```bash
costmaxx doctor   # hermes_mcp_config is reported; it is informational, not required
```

### Uninstall

```bash
costmaxx uninstall --target hermes   # removes only the costmaxx mcp_servers entry it added
```

## Maintenance

### Daily garbage collection (launchd)

A daily gc keeps the store bounded — it removes artifacts and metadata
consistently (files + SQLite rows together). Pattern: a wrapper script plus
a launchd plist.

`~/Library/LaunchAgents/com.costmaxx.gc.sh`:

```bash
#!/bin/bash
exec /usr/local/bin/costmaxx gc --older-than=168h >> ~/.costmax/logs/gc.log 2>&1
```

`~/Library/LaunchAgents/com.costmaxx.gc.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.costmaxx.gc</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>/Users/YOU/Library/LaunchAgents/com.costmaxx.gc.sh</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key><integer>3</integer>
    <key>Minute</key><integer>0</integer>
  </dict>
  <key>StandardOutPath</key><string>/Users/YOU/.costmax/logs/gc.log</string>
  <key>StandardErrorPath</key><string>/Users/YOU/.costmax/logs/gc.log</string>
</dict>
</plist>
```

Load it:

```bash
launchctl load ~/Library/LaunchAgents/com.costmaxx.gc.plist
```

### Weekly savings snapshot

`costmaxx savings` reports aggregate savings across sessions. The daily
launchd job (or any cron equivalent) can append one to
`~/.costmax/logs/snapshots.log` — run it weekly and watch the trend:

```bash
costmaxx savings --since 168h >> ~/.costmax/logs/snapshots.log
```

See [docs/HONEST-NUMBERS.md](HONEST-NUMBERS.md) for what the number means
and how to interpret it.

## Doctor, end to end

```bash
costmaxx doctor                       # all targets (codex + opencode + hermes) + storage + handshake
gemini mcp list | grep costmaxx       # Gemini (if installed)
```

Then prove the pipeline with a real command:

```bash
costmaxx artifact add < .bash_profile
# → cmx://artifact/<id>
costmaxx artifact retrieve <id>
# → the original bytes, verbatim
```

If retrieval returns the exact input, storage, addressing, and compression
all work; anything the agent reads after that is the reduction layer, whose
output the post-render guard checks on every call.
