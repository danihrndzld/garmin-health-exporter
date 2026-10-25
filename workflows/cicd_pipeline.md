# Workflow: CI/CD Pipeline for the Tauri App

## Objective
Produce a fresh, tested universal build of Garmin Export (`tauri-app/`) by running clean, install, test, build and open in order. The build runs in a separate git worktree, so it never touches the checkout the user runs the app from. Use this workflow when the user says "rebuild", "make a fresh DMG" or "run the pipeline", or after a change to `tauri-app/` that needs a real build.

## Required Inputs
None. The tool reads everything from the repo layout.

Optional context from the user:
- **Stage subset**: "just run tests", "skip install" or "build but don't open" become `--skip-*` flags or `--stages=...`.
- **Rust toolchain**: the universal build needs rustup with the `aarch64-apple-darwin` and `x86_64-apple-darwin` targets. Pass `--rustup-home` and `--cargo-home`, or set `RUSTUP_HOME` and `CARGO_HOME`. If the user has not said where that rustup lives, ask.
- **Build here**: `--here` builds in this checkout instead of a worktree. Use it only when the user says the app is closed.
- **Platform**: macOS only (universal DMG and `.app`). If the user asks for Windows or Linux, stop and say the tool does not build them.

## Tool
`tools/ci_pipeline.py`

### Stage contract

| Stage   | What it does | Failure means |
| ------- | ------------ | ------------- |
| clean   | `rm -rf tauri-app/dist tauri-app/src-tauri/binaries` and every `tauri-app/src-tauri/target/*/release/bundle`. It never removes the cargo cache. | File permissions. |
| install | `bun install` in `tauri-app/`. | Registry or lockfile problem. |
| test    | `tools/test_report.py`: test IDs, Go vet and tests (JUnit + coverage), `ui/__tests__` (JUnit), Playwright Chromium + WebKit (JUnit, flaky count), then `bun run core` and `cargo test` in `src-tauri/` (log). Every suite runs to the end; then `report.html`, `report.pdf` and `summary.html` are built in `tauri-app/reportes-pruebas/<date>-ci/`. `bun run core` builds the host Go core, because tauri-build refuses to compile without it and clean removed it. GitHub Actions (`.github/workflows/tests.yml`) runs the same script without cargo on every push and uploads the folder as the `test-report` artifact. | A real regression: the stage fails after the report is written. Open `report.html`, then the suite's `.log`. Stop. |
| build   | Checks `rustup target list --installed` for both targets. Then `git worktree add --detach .worktrees/build-<sha> HEAD`, `bun install` and `bun run release` there, copies `Garmin Export_<ver>_universal.dmg` and `Garmin Export.app` from `src-tauri/target/universal-apple-darwin/release/bundle/` to `tauri-app/dist/` (`cp` and `ditto`), and runs `git worktree remove --force`. With `--here`, the same `bun install`, `bun run release` and copy run in this checkout. | A missing rust target, or a Go, Rust or bundler error. |
| open    | `codesign --verify --deep --strict "tauri-app/dist/Garmin Export.app"`, then `open -n` on it. | A broken signature or a launch failure. |

### Flags

`--stages`, `--skip-clean`, `--skip-install`, `--skip-tests`, `--skip-build`, `--no-open`, `--here`, `--dry-run`, `--rustup-home`, `--cargo-home`. `--stages` overrides the `--skip-*` flags. `--dry-run` prints every command and runs none of them; it still reads `git status`, `git rev-parse` and `rustup target list`, which change nothing. `--cargo-home` also puts `$CARGO_HOME/bin` first on `PATH`, so the rustup `cargo` wins over Homebrew's.

Exit codes: 0 success, 1 stage failure, 2 bad arguments or no `tauri-app/`.

### Invocation patterns

```bash
# Full pipeline with an isolated rustup
uv run tools/ci_pipeline.py --rustup-home "$RUST/rustup" --cargo-home "$RUST/cargo"

# Fast inner loop: tests only
uv run tools/ci_pipeline.py --stages test

# Build a DMG without reinstalling, testing or launching
uv run tools/ci_pipeline.py --skip-install --skip-tests --no-open

# Build in this checkout (the app must be closed)
uv run tools/ci_pipeline.py --here --no-open

# See every command without running anything
uv run tools/ci_pipeline.py --dry-run
```

## Expected Outputs
- `tauri-app/dist/Garmin Export_<ver>_universal.dmg`
- `tauri-app/dist/Garmin Export.app` (universal: `arm64` and `x86_64`)
- No `.worktrees/build-<sha>` left behind, also when the build fails.
- Exit 0, and the app opens unless `--no-open` was given.

`tauri-app/dist/` is gitignored.

## Edge Cases & Known Quirks

- **The worktree build is cold.** A fresh worktree has no `node_modules` and no cargo `target/`, so `bun install` and the universal Rust build take several minutes. That is expected. Run the tool in the background.
- **The worktree builds HEAD.** Uncommitted changes are not in the build. The tool prints a warning when the tree is dirty. Commit first if the change must be in the DMG.
- **`--here` only when the app is closed.** The user often runs the app from `tauri-app/src-tauri/target/release/bundle` in this checkout. A build here rewrites `target/` under the running app. Clean only removes the `bundle/` folders, but use `--here` only when the user says the app is closed.
- **Two rust targets.** `bun run release` is `tauri build --target universal-apple-darwin`, which needs both `aarch64-apple-darwin` and `x86_64-apple-darwin`. Homebrew's Rust has no `x86_64-apple-darwin`, so v4.0.0 and later were built with an isolated rustup (`RUSTUP_HOME` and `CARGO_HOME` in a scratch folder, installed with `rustup-init`). The build stage stops before the worktree is created when a target is missing.
- **A stale build worktree.** If an earlier run was killed, `.worktrees/build-<sha>` can still exist. The build stage removes it with `git worktree remove --force` before it adds a new one.
- **Ad hoc signature.** `tauri.conf.json` sets `signingIdentity: "-"`. The app is signed ad hoc, not with a Developer ID, so `codesign --verify` passes but Gatekeeper asks on the first open on another Mac (right click, Open). The release notes say so.
- **The host Go binary for `cargo test`.** tauri-build checks that `src-tauri/binaries/garmin-export-core-<host triple>` exists. The test stage runs `bun run core` before `cargo test` for that reason.

## Self-Improvement Log
When a run fails, record the diagnosis here before moving on. Keep entries short (date, symptom, fix).

- 2026-10-16: rewritten for `tauri-app/`; the electron-builder entries live in git history before this date.
