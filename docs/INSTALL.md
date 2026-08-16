# Installing CostMax

CostMax is one binary, three integrations. Everything is local; every write
to an existing config happens with a backup first.

## Prereqs

- A `costmaxx` binary on `PATH`. From source:
  `go install github.com/derinbarutcu17/costmaxx/cmd/costmax@latest`,
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
(backing up the file first — the backup path is printed):

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

### The auto-compression plugin

A companion plugin auto-compresses bash tool results larger than a
threshold into the same `cmx://artifact/<id>` envelope. It lives at
`~/.config/opencode/plugins/costmaxx.ts` (global — applies to every
project). The README and this doc are the contract; the plugin itself is a
small TypeScript file — drop it in place if it is not already there:

```bash
mkdir -p ~/.config/opencode/plugins
# place the costmaxx.ts plugin file at ~/.config/opencode/plugins/costmaxx.ts
```

Behavior:

- Threshold: default 20,000 chars of tool output. Override with env
  `COSTMAX_COMPRESS_THRESHOLD`, or set `[reduce] threshold` in
  `~/.costmax/config.toml` — env wins.
- Kill switch: `COSTMAX_DISABLE=1` disables compression entirely.
- Nothing is discarded: the full raw output is stored locally, retrievable
  with `costmaxx artifact retrieve <id>` or the MCP resource.
- The artifact store is shared with Codex sessions.

### Verify

```bash
costmaxx doctor   # checks artifact store, binary, codex + opencode config, handshake
```

### Uninstall

1. Remove the MCP block: `costmaxx uninstall --target opencode`
2. Remove the plugin: `rm ~/.config/opencode/plugins/costmaxx.ts`

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
costmaxx doctor                       # all targets (codex + opencode) + storage + handshake
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
