# Workflow: Ship a Release

## Objective
Cut a new version of the Garmin Data Exporter: bump version, merge the working branch into `main`, rebuild fresh DMGs at the new version, tag the commit, and publish a GitHub release with both architecture DMGs attached. Use this whenever the user says "cut a release", "ship it", "release vX.Y.Z", or after the `cicd_pipeline.md` workflow has produced a known-good build and the feature branch is ready to land.

## Required Inputs
- **Version kind**: `patch` / `minor` / `major` — OR an explicit version (`3.2.0`). If the user does not specify, ask: "`patch`, `minor`, `major`, or an explicit X.Y.Z?"
- **Source branch**: the branch holding the feature work. Default: whatever branch is currently checked out.

## Optional Inputs
- **Release notes**: a one-line summary or a path to a markdown file. If neither is provided, the tool auto-generates notes from `git log origin/main..<source>`.
- **Skip merge**: when the work has already landed on `main` and this is just a retro-tag + release. Pass `--skip-merge`.
- **Skip build**: when DMGs at the target version already exist in `electron-app/dist/` from a recent `cicd_pipeline` run. Pass `--skip-build`.

## Tool
`tools/release.py`

### Stage contract

| Stage     | What it does                                                                                          | Failure means                                                   |
| --------- | ----------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| preflight | Clean working tree, `gh auth status`, no pre-existing tag `vX.Y.Z` locally / on origin / as a release | User must commit/stash, `gh auth login`, or pick a new version  |
| bump      | Rewrite `"version"` in `electron-app/package.json`, commit as `chore(release): bump to vX.Y.Z`        | Regex miss — someone reformatted `package.json`                 |
| merge     | Push source branch, fetch `main`, checkout `main`, `pull --ff-only`, `merge --ff-only <source>`, push | `main` has commits not on the feature branch — rebase first     |
| build     | Shells out to `tools/ci_pipeline.py --no-open` (clean → install → test → build)                       | Test regression or electron-builder failure — see its log       |
| tag       | `git tag -a vX.Y.Z -m "Release vX.Y.Z"` and push the tag                                              | Tag already on origin — someone else shipped this version       |
| release   | `gh release create vX.Y.Z` with both DMGs attached and the computed notes body                        | `gh` not authed, or DMG file names don't match expected pattern |

### Invocation patterns

```bash
# Typical minor release from the feature branch you're on now
uv run tools/release.py --bump minor

# Patch release with explicit notes
uv run tools/release.py --bump patch --notes "Fixes cache warmup race on first launch."

# Explicit version, notes from a file
uv run tools/release.py --version 4.0.0 --notes-file RELEASE_NOTES.md

# Already on main — just tag and ship
uv run tools/release.py --version 3.2.1 --skip-merge

# DMGs were already built by ci_pipeline.py — just tag, release, attach
uv run tools/release.py --version 3.2.0 --skip-build

# Dry run — print every shell command without executing anything
uv run tools/release.py --bump minor --dry-run
```

## Expected Outputs
- `electron-app/package.json` `version` bumped.
- One new commit on `<source>` and on `main` (fast-forward, same SHA).
- Both branches pushed to `origin`.
- Annotated tag `vX.Y.Z` on `main`, pushed to `origin`.
- `electron-app/dist/Garmin Data Exporter-<X.Y.Z>-arm64.dmg` and `electron-app/dist/Garmin Data Exporter-<X.Y.Z>.dmg` present.
- GitHub release `vX.Y.Z` published, both DMGs attached. Release URL is printed to stdout as the tool's final line.

## Edge Cases & Known Quirks

- **Non-fast-forward merge.** If `main` has commits the feature branch doesn't have, `merge --ff-only` fails. Stop. Rebase the feature branch onto `origin/main` and rerun — do NOT attempt a merge commit inside the release flow.
- **Unsigned DMGs.** Same as `cicd_pipeline`: electron-builder skips signing without a Developer ID cert. The release ships unsigned DMGs. The release body reminds end-users to run `Remove Quarantine.command` inside the DMG. This is expected — do not pause to ask.
- **`gh` auth.** The tool checks `gh auth status` in preflight. If it fails, the user must run `gh auth login` themselves — it is interactive and can't be done by the agent.
- **Existing tag/release.** If a tag or release for this version already exists on origin, the tool aborts in preflight. To re-release, the user must delete the existing tag and release manually (`gh release delete vX.Y.Z && git push origin :refs/tags/vX.Y.Z`) — the tool will not do this destructively.
- **`package.json` version regex.** The bump stage edits the first top-level `"version": "X.Y.Z"` match. If someone adds a second semver-shaped string literally matching that pattern earlier in the file, the substitution could hit the wrong line. Preflight reads the current version via `json.loads` so a format-breaking edit fails loudly before bump.
- **Notes with backticks or dollar signs.** Notes are passed via `gh release create --notes <body>` as a single argument, not through the shell. Backticks, `$`, and newlines are safe. For multi-paragraph bodies, prefer `--notes-file`.
- **Mid-flight abort.** If the tool fails after `bump` but before `release`, the version commit is real. Recovery: `git reset --hard HEAD^` on both branches (if pushed, `git push --force-with-lease` — confirm with user first), delete any partial tag, then rerun. No cleanup logic is baked into the tool — destructive operations need human judgment.

## Self-Improvement Log
When a release fails, record the diagnosis here before moving on. Keep entries short (date + symptom + fix).

- 2026-04-22: Initial codification of the release flow. Pattern lifted from the v3.2.0 release, which was done by hand: bump → merge → push → `npm run dist` → `gh release create` with DMGs.
