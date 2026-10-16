# Workflow: Ship a Release

## Objective
Cut a new version of Garmin Export (`tauri-app/`): bump the version in four files in one local commit, build a fresh universal DMG from that commit in a git worktree, verify the DMG, fast forward `main`, push, tag, and publish a GitHub release with the one DMG attached. Nothing reaches origin until the DMG is built and verified. Use this workflow when the user says "cut a release", "ship it" or "release vX.Y.Z", or after `cicd_pipeline.md` gave a known good build and the branch is ready to land.

## Required Inputs
- **Version kind**: `patch`, `minor` or `major`, or an explicit version (`4.5.0`). If the user does not say, ask: "`patch`, `minor`, `major`, or an explicit X.Y.Z?"
- **Source branch**: the branch with the work. Default: the branch that is checked out.
- **Rust toolchain**: a rustup with `aarch64-apple-darwin` and `x86_64-apple-darwin`, through `--rustup-home` and `--cargo-home` or `RUSTUP_HOME` and `CARGO_HOME`. `--cargo-home` also puts `$CARGO_HOME/bin` first on `PATH`, for `cargo update` and for `ci_pipeline.py`.

## Optional Inputs
- **Release notes**: one line (`--notes`) or a markdown file (`--notes-file`). Without either, the tool writes notes from `git log origin/main..<source>`, names the universal DMG and explains the first open.
- **Pre-release**: `--prerelease` publishes it as a GitHub pre-release.
- **Skip merge**: the release comes from the current branch and `main` is not moved. Pass `--skip-merge`. On `main` the merge is skipped on its own.

There is no `--skip-build`: the DMG must carry the bumped version, and only the tool's own bump commit has it, so every release builds.

## Tool
`tools/release.py`

### Stage contract

The stages run in this order. Build and verify come before any merge, push or tag.

| Stage     | What it does | Failure means |
| --------- | ------------ | ------------- |
| preflight | `tauri-app/package.json` present, `cargo` on `PATH`, clean working tree, `gh auth status`, source branch checked out, no tag `vX.Y.Z` locally or on origin and no release `vX.Y.Z`. Unless the merge is skipped: `git fetch origin main`, `origin/main` and `main` must be ancestors of HEAD (so the merge will fast forward), and the checkout that has `main` (from `git worktree list --porcelain`) must be clean. | Commit first, run `gh auth login`, rebase onto `origin/main`, clean the main checkout, or pick a new version. |
| bump      | Sets `version` in `tauri-app/package.json` and `tauri-app/src-tauri/tauri.conf.json` (checked with `json.loads`, replaced with a narrow `re.subn` that only matches the current version) and the first `version = "..."` line of `tauri-app/src-tauri/Cargo.toml`. `cargo update --workspace --offline` in `src-tauri/` refreshes `Cargo.lock`. Commits the four files as `chore(release): bump to vX.Y.Z`, locally only. | A file does not hold the current version (drift), or cargo has no offline index. |
| build     | `uv run tools/ci_pipeline.py --no-open`: clean, install, test, and the universal build in a fresh worktree of HEAD, which is the bump commit. | A test regression or a build error. See its log. |
| verify    | `hdiutil verify` on `tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg`, then `hdiutil attach -nobrowse -readonly` on a temporary mount point and, on the `.app` inside the DMG: `lipo -archs` on every file in `Contents/MacOS` (each must have `arm64` and `x86_64`), `PlistBuddy` `CFBundleShortVersionString` equal to `X.Y.Z`, `codesign --verify --deep --strict`. `hdiutil detach` always runs. | A broken DMG, a thin binary, a wrong version or a broken signature. |
| merge     | Fast forwards `main` to the source branch without a checkout: `git -C <main checkout> merge --ff-only <source>`, or `git fetch . <source>:main` when no checkout has `main`. Skipped on `main` or with `--skip-merge`. | Someone moved `main` after preflight. |
| push      | `git push --atomic origin <source> main` (only `<source>` when the merge is skipped), then `git tag -a vX.Y.Z -m "Release vX.Y.Z"` and `git push origin vX.Y.Z`. | Push refused, or the tag appeared on origin meanwhile. |
| release   | Writes the notes to `.tmp/release-notes-vX.Y.Z.md`, then `gh release create vX.Y.Z "tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg" --title vX.Y.Z --notes-file .tmp/release-notes-vX.Y.Z.md`, plus `--prerelease` when asked. | `gh` refused. |

When build or verify fails, origin is untouched. The tool prints how to undo the local bump: `git reset --hard HEAD^` on the source branch.

`--dry-run` prints every command in this order and changes no tracked file, branch, tag or remote. It still runs the read only checks (`git status`, `git tag --list`, `git ls-remote`, `git merge-base --is-ancestor`, `git worktree list`, `gh auth status`, `gh release view`), runs `ci_pipeline.py --no-open --dry-run` so the build commands are printed too, and writes the notes to `.tmp/release-notes-vX.Y.Z.md` (gitignored scratch) so the printed `gh release create` command works when pasted.

Exit codes: 0 success (the release URL is the last line of stdout). 1 a bump, build, verify, merge, push or release step failed; after a build or verify failure origin is untouched. 2 refused before any change: bad arguments, an invalid version in `tauri-app/package.json`, or a preflight refusal (the message goes to stderr).

### Invocation patterns

```bash
# Typical minor release from the branch you are on
uv run tools/release.py --bump minor --rustup-home "$RUST/rustup" --cargo-home "$RUST/cargo"

# Patch release with one line of notes
uv run tools/release.py --bump patch --notes "Fixes the date picker on the first launch."

# Explicit version, notes from a file, as a pre-release
uv run tools/release.py --version 5.0.0 --notes-file NOTES.md --prerelease

# Release from the current branch without moving main
uv run tools/release.py --version 4.5.1 --skip-merge

# Print every command without running anything
uv run tools/release.py --bump minor --dry-run
```

## Expected Outputs
- `version` bumped in `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/Cargo.toml` and `tauri-app/src-tauri/Cargo.lock`, in one commit.
- `tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg` (verified from the inside) and `tauri-app/dist/Garmin Export.app`.
- That commit on `<source>` and on `main` (fast forward, same SHA), both pushed.
- Annotated tag `vX.Y.Z` on that commit, pushed.
- GitHub release `vX.Y.Z` with the DMG attached. GitHub stores the asset as `Garmin.Export_X.Y.Z_universal.dmg` (a space becomes a dot), as it did for v4.4.0. The release URL is the tool's last line.

## Edge Cases & Known Quirks

- **Non fast forward merge.** If `main` or `origin/main` has commits the branch does not have, preflight stops before the bump. Rebase the branch onto `origin/main` and run again. Never make a merge commit inside the release flow.
- **The main checkout.** The merge never runs `git checkout`, so it works from a feature worktree and never switches the branch under the user's checkout. It runs `merge --ff-only` inside the checkout that has `main`, which updates that checkout's files to the new commit. Preflight refuses when that checkout has uncommitted changes.
- **`gh` auth.** Preflight runs `gh auth status`. If it fails, the user must run `gh auth login` themselves, because it is interactive.
- **Existing tag or release.** Preflight stops when the tag or the release already exists. To release again, the user deletes them by hand (`gh release delete vX.Y.Z && git push origin :refs/tags/vX.Y.Z`). The tool never deletes them.
- **The agent may not be allowed to publish.** The permission classifier can refuse `git push`, the tag push or `gh release create`, or the whole `uv run tools/release.py` call. Every command is printed before it runs, and the notes file is written before `gh release create`, also in a dry run. Then the user runs the printed commands, or the whole `release.py` command, themselves. Do not retry a refused command.
- **The worktree build is cold.** The build stage creates a fresh worktree, so `bun install` and the universal Rust build take minutes. Run the tool in the background.
- **Two rust targets.** The universal build needs `aarch64-apple-darwin` and `x86_64-apple-darwin`. Homebrew's Rust has no `x86_64-apple-darwin`, so v4.0.0 and later were built with an isolated rustup through `RUSTUP_HOME` and `CARGO_HOME`. Pass them with `--rustup-home` and `--cargo-home`; `ci_pipeline.py` stops before the build when a target is missing.
- **Ad hoc signature.** `tauri.conf.json` sets `signingIdentity: "-"`. There is no Developer ID and no notarization, so Gatekeeper asks on the first open: right click the app, choose Open, and confirm. The default release notes say so. This is expected; do not stop to ask.
- **`Cargo.lock` needs the cargo cache.** `cargo update --workspace --offline` only rewrites the `garmin-export` entry and needs no network, but it reads the local registry index. On a machine that never built the app it can fail after the three other files changed; run `git checkout -- tauri-app`, then one `cargo fetch` in `src-tauri/`, and try again.
- **Notes with backticks or dollar signs.** The notes go through a file, never through a shell, so backticks, `$` and newlines are safe.
- **Abort after the push.** If the tag push or `gh release create` fails, the bump commit is already on origin. Recovery: fix the cause and run the remaining printed commands by hand. Rewriting `main` on origin (`git push --force-with-lease`) needs the user's confirmation first. The tool has no cleanup logic, because destructive steps need a person's judgment.

## Self-Improvement Log
When a release fails, record the diagnosis here before moving on. Keep entries short (date, symptom, fix).

- 2026-10-16: rewritten for `tauri-app/`; the electron-builder entries live in git history before this date.
- 2026-10-16: review of the rewrite. Pushes ran before build and verify, so a failed build left the bump on `origin/main`; the order is now bump, build, verify, merge, push, tag, release. Verify checks the `.app` inside the mounted DMG. The merge no longer checks out `main`. `--skip-build` could never succeed (a DMG at the new version only exists after the bump), so it is gone.
