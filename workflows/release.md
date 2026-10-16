# Workflow: Ship a Release

## Objective
Cut a new version of Garmin Export (`tauri-app/`): bump the version in four files, merge the working branch into `main`, build a fresh universal DMG at the new version in a git worktree, verify it, tag the commit, and publish a GitHub release with the one DMG attached. Use this workflow when the user says "cut a release", "ship it" or "release vX.Y.Z", or after `cicd_pipeline.md` gave a known good build and the branch is ready to land.

## Required Inputs
- **Version kind**: `patch`, `minor` or `major`, or an explicit version (`4.5.0`). If the user does not say, ask: "`patch`, `minor`, `major`, or an explicit X.Y.Z?"
- **Source branch**: the branch with the work. Default: the branch that is checked out.
- **Rust toolchain**: a rustup with `aarch64-apple-darwin` and `x86_64-apple-darwin`, through `--rustup-home` and `--cargo-home` or `RUSTUP_HOME` and `CARGO_HOME`. The tool passes them to `ci_pipeline.py`.

## Optional Inputs
- **Release notes**: one line (`--notes`) or a markdown file (`--notes-file`). Without either, the tool writes notes from `git log origin/main..<source>`, names the universal DMG and explains the first open.
- **Pre-release**: `--prerelease` publishes it as a GitHub pre-release.
- **Skip merge**: the work is already on `main`. Pass `--skip-merge`.
- **Skip build**: `tauri-app/dist/Garmin Export_<X.Y.Z>_universal.dmg` already exists from a `cicd_pipeline` run at that version. Pass `--skip-build`. The verify stage still runs.

## Tool
`tools/release.py`

### Stage contract

| Stage     | What it does | Failure means |
| --------- | ------------ | ------------- |
| preflight | Clean working tree, `gh auth status`, no tag `vX.Y.Z` locally or on origin and no release `vX.Y.Z`, `tauri-app/package.json` present. With `--skip-build`, the one DMG must exist. | Commit first, run `gh auth login`, or pick a new version. |
| bump      | Sets `version` in `tauri-app/package.json` and `tauri-app/src-tauri/tauri.conf.json` (checked with `json.loads`, replaced with a narrow `re.subn` that only matches the current version) and the first `version = "..."` line of `tauri-app/src-tauri/Cargo.toml`. Then `cargo update --workspace --offline` in `src-tauri/` refreshes `Cargo.lock`. Commits the four files as `chore(release): bump to vX.Y.Z`. | A file does not hold the current version (drift), or cargo has no offline index. |
| merge     | Push the source branch, fetch `main`, check out `main`, `pull --ff-only`, `merge --ff-only <source>`, push. Skipped on `main` or with `--skip-merge`; the bump commit is still pushed. | `main` has commits the branch does not have. Rebase first. |
| build     | `uv run tools/ci_pipeline.py --no-open` (clean, install, test, build in a worktree). | A test regression or a build error. See its log. |
| verify    | `hdiutil verify` on the DMG; `lipo -archs` on every file in `Garmin Export.app/Contents/MacOS` (each must have `arm64` and `x86_64`); `PlistBuddy` reads `CFBundleShortVersionString`, which must equal `X.Y.Z`; `codesign --verify --deep --strict` on the `.app`. | A broken DMG, a thin binary, a wrong version or a broken signature. |
| tag       | `git tag -a vX.Y.Z -m "Release vX.Y.Z"` and push the tag. | The tag is already on origin. |
| release   | Writes the notes to `.tmp/release-notes-vX.Y.Z.md`, then `gh release create vX.Y.Z "tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg" --title vX.Y.Z --notes-file .tmp/release-notes-vX.Y.Z.md`, plus `--prerelease` when asked. | `gh` refused, or the DMG is missing. |

`--dry-run` prints every command and changes nothing. The read only checks of preflight (`git status`, `git tag`, `git ls-remote`, `gh auth status`, `gh release view`) still run.

Exit codes: 0 success (the release URL is the last line of stdout), 1 stage failure, 2 bad arguments or a failed preflight.

### Invocation patterns

```bash
# Typical minor release from the branch you are on
uv run tools/release.py --bump minor --rustup-home "$RUST/rustup" --cargo-home "$RUST/cargo"

# Patch release with one line of notes
uv run tools/release.py --bump patch --notes "Fixes the date picker on the first launch."

# Explicit version, notes from a file, as a pre-release
uv run tools/release.py --version 5.0.0 --notes-file NOTES.md --prerelease

# Already on main: bump, tag and ship
uv run tools/release.py --version 4.5.1 --skip-merge

# The DMG was already built by ci_pipeline.py at this version
uv run tools/release.py --version 4.5.0 --skip-build

# Print every command without running anything
uv run tools/release.py --bump minor --dry-run
```

## Expected Outputs
- `version` bumped in `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/Cargo.toml` and `tauri-app/src-tauri/Cargo.lock`, in one commit.
- That commit on `<source>` and on `main` (fast forward, same SHA), both pushed.
- Annotated tag `vX.Y.Z` on that commit, pushed.
- `tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg` and `tauri-app/dist/Garmin Export.app`, verified.
- GitHub release `vX.Y.Z` with the DMG attached. GitHub stores the asset as `Garmin.Export_X.Y.Z_universal.dmg` (a space becomes a dot), as it did for v4.4.0. The release URL is the tool's last line.

## Edge Cases & Known Quirks

- **Non fast forward merge.** If `main` has commits the branch does not have, `merge --ff-only` fails. Stop. Rebase the branch onto `origin/main` and run again. Never make a merge commit inside the release flow.
- **`gh` auth.** Preflight runs `gh auth status`. If it fails, the user must run `gh auth login` themselves, because it is interactive.
- **Existing tag or release.** Preflight stops when the tag or the release already exists. To release again, the user deletes them by hand (`gh release delete vX.Y.Z && git push origin :refs/tags/vX.Y.Z`). The tool never deletes them.
- **The agent may not be allowed to publish.** The permission classifier can refuse `git push`, `git tag` pushes or `gh release create`. Every command is printed before it runs, and the notes are already in `.tmp/release-notes-vX.Y.Z.md`, so the user can paste the printed command as is. Do not retry a refused command.
- **The worktree build is cold.** The build stage creates a fresh worktree, so `bun install` and the universal Rust build take several minutes. Run the tool in the background.
- **Two rust targets.** The universal build needs `aarch64-apple-darwin` and `x86_64-apple-darwin`. Homebrew's Rust has no `x86_64-apple-darwin`, so v4.0.0 and later were built with an isolated rustup through `RUSTUP_HOME` and `CARGO_HOME`. Pass them with `--rustup-home` and `--cargo-home`; `ci_pipeline.py` stops before the build when a target is missing.
- **Ad hoc signature.** `tauri.conf.json` sets `signingIdentity: "-"`. There is no Developer ID and no notarization, so Gatekeeper asks on the first open: right click the app, choose Open, and confirm. The default release notes say so. This is expected; do not stop to ask.
- **`--skip-build` after a bump.** The verify stage compares the `.app` version with `X.Y.Z`. A DMG built before the bump has the old version and fails verify. Build at the new version first.
- **`Cargo.lock` needs the cargo cache.** `cargo update --workspace --offline` only rewrites the `garmin-export` entry and needs no network, but it reads the local registry index. On a machine that never built the app it can fail; run one `cargo fetch` in `src-tauri/` and try again.
- **Notes with backticks or dollar signs.** The notes go through a file, never through a shell, so backticks, `$` and newlines are safe.
- **Mid flight abort.** If the tool fails after bump and before release, the version commit is real. Recovery: `git reset --hard HEAD^` on both branches (if they were pushed, `git push --force-with-lease`, and confirm with the user first), delete any partial tag, then run again. The tool has no cleanup logic, because destructive steps need a person's judgment.

## Self-Improvement Log
When a release fails, record the diagnosis here before moving on. Keep entries short (date, symptom, fix).

- 2026-10-16: rewritten for `tauri-app/`; the electron-builder entries live in git history before this date.
