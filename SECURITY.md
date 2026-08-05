# Security Policy

CostMax executes arbitrary shell commands with your process permissions and
stores local evidence. Security matters here.

## Supported Versions

Only the latest release is actively patched. If you are on an older
version, upgrade before reporting.

| Version | Supported |
|---------|-----------|
| Latest release | ✅ |
| Older releases | ❌ |

## Reporting a Vulnerability

Do **not** open a public issue for a vulnerability. Use GitHub's private
advisory process:

https://github.com/derinbarutcu17/costmaxx/security/advisories/new

Please include:

- What you found and where (component, file, command)
- A minimal reproduction (command, input, environment)
- Impact: what could an attacker do with this?
- Suggested fix if you have one

You will receive an acknowledgement within 7 days. We will coordinate a
fix and disclosure timeline with you.

## Scope

- `costmax_run` command execution paths
- Artifact storage and retrieval (path traversal, symlink handling)
- SQLite metadata handling
- Secret redaction gaps in stored evidence
- MCP protocol handling (malformed input, resource URIs)
