# Contributing to CostMax

Thanks for considering a contribution. CostMax is small on purpose: local-first,
deterministic, and honest about what it proves.

## Development setup

Requires Go 1.22+.

```bash
go build -buildvcs=false -o /tmp/costmaxx ./cmd/costmax/
go test -buildvcs=false -count=1 ./...
```

## Verification suite

CostMax's claims come from an evaluation harness, not screenshots. Run the
deterministic checks before submitting:

```bash
python3 scripts/run-codex-eval.py --self-test      # validates the 20 fixtures
python3 scripts/run-codex-eval.py --fixture-smoke  # runs fixture commands locally
bash scripts/verify-active-path.sh /tmp/costmaxx   # artifact/reduction/retrieval proof
```

The full live evaluation (`--live`) spends Codex API usage; run it only when
you are changing reduction behavior, and always audit the result with
`scripts/verify-live-results.py`. See [docs/EVALUATION_PROTOCOL.md](docs/EVALUATION_PROTOCOL.md).

Note: `results/` (live transcripts, multi-GB) is gitignored. Verification is
reproducible via the scripts and docs, not by committing evidence.

## Pull requests

- One change per PR
- Include tests (table-driven where appropriate)
- Update docs if behavior changes
- Add a CHANGELOG.md entry
- Run `go vet ./...` before submitting

## Code style

- Go standard formatting (`gofmt`)
- No unused exports
- Errors are wrapped with context (`fmt.Errorf("...: %w", err)`)
- No new dependencies for what stdlib can do
- If you touch the honesty posture (claims, docs), keep it: no claim beyond
  the evidence, ever

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
