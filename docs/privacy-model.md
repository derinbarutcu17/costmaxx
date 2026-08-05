# Privacy Model

## Principles

- Local storage by default
- No telemetry
- No remote summarization
- No private code/logs uploaded
- Explicit opt-in for analytics
- Human-readable retention
- Complete deletion command
- Repo-specific exclusion patterns
- Secret redaction before exported reports

## Secret Redaction

Redacted patterns:
- API keys and tokens
- Passwords and credentials
- JWT tokens
- URLs with embedded credentials
- Email addresses
- IP addresses

## File Security

- User-only directory permissions (0700/0600)
- Atomic writes
- Content digests (SHA-256)
- Canonicalized paths
- Symlink checks
- Size limits
- SQLite WAL mode with controlled locking

## Fail-Open Policy

When CostMax cannot confidently process, return original output unchanged.

Conditions: unknown hook schema, storage failure, reducer crash, corrupt config, unrecognized encoding, artifact verification failure, timeout, DB lock beyond retry budget.
