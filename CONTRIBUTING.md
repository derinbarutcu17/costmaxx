# Contributing to CostMax

## Development

```bash
go build ./cmd/costmaxx/
go test ./tests/unit/...
```

## PRs

- One change per PR
- Include tests
- Update docs if behavior changes
- Run `go vet ./...` before submitting

## Code Style

- Go standard formatting (`gofumpt` preferred)
- No unused exports
- Error handling: return errors, don't panic
- Tests: table-driven where appropriate
