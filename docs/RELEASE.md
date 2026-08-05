# CostMax release automation — recommended setup (v0.1.0)

Everything below is drop-in. Two files to add (`.goreleaser.yaml`, `.github/workflows/release.yml`), one optional CI step, a 4-line version-variable wiring, then a 3-command release runbook.

---

## 1. GoReleaser vs hand-rolled GitHub Actions — use GoReleaser

| Concern | GoReleaser (recommended) | Hand-rolled Actions |
|---|---|---|
| 5-platform build matrix | declarative `goos`/`goarch` | env-var loop (already in `scripts/build-release.sh`) |
| Checksums manifest | built-in `checksums.txt` | already in script |
| Changelog generation | from `git log` since last tag, built-in | hand-roll with `git log`/`sed` or maintain a CHANGELOG.md |
| Attach to GitHub Release | built-in (uses `GITHUB_TOKEN`) | `softprops/action-gh-release` |
| Artifact naming | templates | hardcoded |
| Version injection (`-X main.version=…`) | automatic from tag | manual |
| Homebrew tap / Docker / signing later | ~8 config lines each | a new workflow per thing |
| New tooling to learn | one YAML file | none |

**Evidence from real projects** (a Go CLI with a single static binary is squarely in goreleaser territory):

- **gh CLI (cli/cli)** — ships a `.goreleaser.yml` and runs `goreleaser/goreleaser-action@v7` from `.github/workflows/deployment.yml`. Distributed via Homebrew (homebrew-core), scoop, winget, apt — **no curl\|sh**.
- **fzf (junegunn/fzf)** — runs goreleaser from `.github/workflows/release.yml` (`release --clean --release-notes tmp/release-note`). They hand-maintain `CHANGELOG.md` and feed it to goreleaser; the git-log changelog is enough for us.
- **lazygit (jesseduffield/lazygit)** — goreleaser v2, pinned exactly like the config below (`distribution: goreleaser`, `version: v2`, `args: release --clean`).
- **Hand-rolled end of the spectrum**: jq (jqlang/jq) has no release workflow — releases are cut via scripts; uv (astral-sh/uv) has a custom multi-job pipeline, but it's Rust with a huge artifact matrix and its own installers — atypical for a Go CLI.

**Why goreleaser wins here specifically**: all five of your requirements (matrix, checksums, changelog, naming, attach-on-tag) map 1:1 to built-in config, the exact same pipeline shape is used by gh/fzf/lazygit so you're on the boring standard path, and the next things a Go CLI needs (homebrew formula, signing) are config lines, not new pipelines. The "simple" workflow only stays simple while you never want anything beyond binaries.

**`scripts/build-release.sh` stays** for local builds — goreleaser only runs in CI. Keep the platform list in both files in sync (commented in both).

---

## 2. Files to add

### 2a. `.goreleaser.yaml` (repo root)

```yaml
# GoReleaser v2 config. Platforms/artifact names mirror scripts/build-release.sh.
version: 2

project_name: costmaxx

builds:
  - id: costmaxx
    main: ./cmd/costmax
    binary: costmaxx
    env:
      - CGO_ENABLED=0            # static binaries (deps are pure Go incl. modernc sqlite)
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64            # same 5 platforms as scripts/build-release.sh
    ldflags:
      - -s -w
      - -X main.version={{ .Version }}
      - -X main.commit={{ .Commit }}
      - -X main.date={{ .Date }}

# Upload raw binaries directly (no tar.gz/zip) so the existing README links
# (releases/latest/download/costmaxx-darwin-arm64) keep working.
archives:
  - id: default
    formats: [binary]
    name_template: "{{ .ProjectName }}-{{ .Os }}-{{ .Arch }}"   # windows gets .exe appended

checksum:
  name_template: "checksums.txt"
  algorithm: sha256

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"
      - "^chore:"
      - "^Merge"
```

Notes:
- No `release:` section needed — defaults are right: release name = tag, `prerelease: auto` (a tag like `v0.2.0-rc.1` is automatically marked pre-release and excluded from `latest`).
- Changelog = `git log` since the previous tag, excludes noise commits. On the very first release (no previous tag) goreleaser lists all commits; if you ever want GitHub-API-based notes instead, add `changelog: use: github`.

### 2b. `.github/workflows/release.yml` (new file)

```yaml
name: release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0   # full history needed for changelog generation

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: v2
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Notes:
- The default `GITHUB_TOKEN` is sufficient (creates the release + uploads assets for this repo). fzf/gh use PATs only because they do macOS notarization / signing — not needed at this stage.
- No test step needed: the tagged commit already passed `ci.yml` on main. Tag only commits that are on main.
- `--clean` clears `dist/` before building (v2 name for the old `--rm-dist`).

### 2c. Optional: validate config in CI

Append to `.github/workflows/ci.yml` (after the existing steps) so config typos break PRs, not releases:

```yaml
      - name: Validate release config
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: v2
          args: check
```

### 2d. Version wiring (required for the ldflags to do anything)

The binary currently has no version variable, so `-X main.version=…` is a no-op until this exists:

```go
// cmd/costmax/main.go — add near the top:
var version = "dev" // overwritten at build time via -ldflags "-X main.version=..."

// where your root cobra.Command is built:
rootCmd.Version = version // gives: costmaxx --version
```

Optional one-line tweak so local builds also carry the version:

```bash
# scripts/build-release.sh
-ldflags="-s -w -X main.version=${VERSION}"
```

---

## 3. Homebrew tap / curl|sh — skip both at v0.1.0

**Verdict: ship only GitHub Release assets + `go install`. Revisit when someone asks "how do I install this?"**

What top projects actually do:

- **gh, bat, ripgrep, jq, lazygit, fzf — all in homebrew-core.** No custom taps for mainstream tools; users get them via `brew install gh`. The tap isn't the distribution channel for them.
- **gh has no curl\|sh** — brew/scoop/winget/apt + binary assets.
- **fzf has `install`/`install.ps1`** — but only because installation must configure shell keybindings; a raw binary can't do that. That reason doesn't apply here.
- **uv has an install script** (`astral.sh/uv/install.sh`) — it must pick the right platform wheel and they target non-Go users; also atypical.

For CostMax at v0.1.0:
- `go install github.com/derinbarutcu17/costmaxx@latest` covers Go users with zero infrastructure.
- The Releases page (raw binaries + checksums.txt) covers everyone else; the README links already work once assets exist.
- A curl\|sh script is a security-sensitive artifact (pinning, checksums, trust) with zero users yet — pure liability.
- Homebrew-core formula: a 30-minute PR later, and homebrew builds the bottles for you. One constraint: keep version injection a simple `-X main.version=` so the formula can do `go build -ldflags "-X main.version=#{version}"`. The wiring in 2d already satisfies this.
- If you later want a **custom** tap (e.g. edge builds), goreleaser generates the formula with an 8-line `brews:` section — no new pipeline.

---

## 4. Version tagging conventions + `go install` verification

**Tag must be `v0.1.0` — the `v` prefix is mandatory for Go tooling.** `golang.org/x/mod/semver` (what the go command uses): a version "must begin with a leading `v`". Per `go.dev/ref/mod`, the go command only derives module versions from valid semver tags; a `0.1.0` tag is invisible — `go install …@v0.1.0` and `@latest` simply won't find it (untagged commits get pseudo-versions instead).

- **Use annotated tags**: `git tag -a v0.1.0 -m "v0.1.0"`. (The existing local tag is lightweight — see runbook.)
- **Don't create the GitHub Release manually.** Push the tag; goreleaser creates the release and marks it `latest` (pre-release automatically if the tag contains a pre-release marker).
- **0.x means "API may change"** — correct for v0.1.0. Pre-releases like `v0.2.0-rc.1` are automatically skipped by `go install …@latest` (only fetchable as `@v0.2.0-rc.1`) — a free mechanism for test releases.

**Verification after release** (module path `github.com/derinbarutcu17/costmaxx` already matches the public repo URL exactly — verified in `go.mod`):

```bash
go install github.com/derinbarutcu17/costmaxx@v0.1.0   # exact version
go install github.com/derinbarutcu17/costmaxx@latest   # latest stable
costmaxx --version                                     # requires 2d wiring

# proxy.golang.org can lag a few minutes after tagging; bypass with:
GOPROXY=direct go install github.com/derinbarutcu17/costmaxx@v0.1.0
```

If install fails with `unknown revision`: the tag isn't pushed — `git ls-remote --tags origin`.

---

## 5. GitHub repo settings

| Setting | Action |
|---|---|
| Settings → General → Releases → "Automatically generate release notes" | **Off** — goreleaser writes the release body; leave off so it doesn't double-fill |
| Settings → General → Discussions | Optional, free — enable if you want a Q&A/announcements channel; disable anytime |
| Settings → Rules → Rulesets: protect `main`, require status check `test` (from ci.yml) | Only if you merge via PRs; skip if you push to main directly (CI already runs on main pushes) |
| Settings → Code security → Dependabot alerts + updates (enable "gomod" and "GitHub Actions") | **Yes** — one click; `modernc.org/sqlite` updates frequently |
| Tag protection rules | Skip — no force-push risk solo |

Leave everything else default. `ci.yml` needs no changes (fmt/vet/test/build/eval already run on push+PR).

---

## 6. v0.1.0 runbook

```bash
cd /Users/derin/Desktop/CODING/costmaxx

# 1. Add the files from §2, commit, push
git add .goreleaser.yaml .github/workflows/release.yml
git commit -m "ci: add goreleaser release pipeline"
git push origin main

# 2. Push the existing local tag (it's lightweight and unpushed; optionally
#    recreate it annotated first — it already points at main HEAD, 7fe6804):
#    git tag -d v0.1.0 && git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0

# 3. Watch: https://github.com/derinbarutcu17/costmaxx/actions
#    Then verify (§4 commands + checksums):
curl -L -o costmaxx https://github.com/derinbarutcu17/costmaxx/releases/latest/download/costmaxx-darwin-arm64
shasum -a 256 costmaxx   # compare against checksums.txt on the release page
```

Future releases are exactly: `git tag -a v0.2.0 -m "v0.2.0" && git push origin v0.2.0`.
