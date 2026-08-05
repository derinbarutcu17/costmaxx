# Reduction Format

Every reduction preserves: command, exit code, and artifact reference.

## Test Output

```
Command: npm test
Exit: 1
Tests: 142 passed, 3 failed

Failures (3):
- auth/session.test.ts:88
- auth/refresh.test.ts:132
- middleware/auth.test.ts:47

Duration: 18.4s

Raw evidence: cmx://artifact/01JZ...
```

## Build Output

```
Command: tsc --noEmit
Exit: 2

Errors: 3, Warnings: 5

Errors (3):
  src/auth/session.ts:45: error TS2322: Type 'string' is not assignable to type 'number'
  ...

--- Last 20 lines of build output ---
...
```

## Terminal Output

```
Command: ./script.sh
Exit: 0

--- First 20 lines ---
...
--- Last 15 lines ---
...
```

## Diff Output

```
Command: git diff
Exit: 0

Files changed: 3
Insertions: 45, Deletions: 12

Files:
  src/auth/session.ts
  src/auth/refresh.ts
  src/middleware/auth.ts
```

## Search Output

```
Command: rg "TODO"
Exit: 0

Matches: 24, Files: 8

src/auth/session.ts:45:   // TODO: implement refresh
...
```

## Confidence Thresholds

| Category | Default |
|----------|---------|
| test | 0.9 |
| build | 0.85 |
| lint | 0.85 |
| diff | 0.8 |
| search | 0.8 |
| json | 0.9 |
| terminal | 0.7 |
| generic | 0.6 |
