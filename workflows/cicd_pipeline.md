# Workflow: CI/CD Pipeline for Electron App

## Objective
Produce a fresh, testable build of the Garmin Data Exporter Electron app by running the full clean → install → test → build → launch cycle. Use this whenever the user says "rebuild", "ship a fresh DMG", "run the pipeline", or after any non-trivial change to `electron-app/`.

## Required Inputs
None. The tool infers everything from the repo layout.

Optional context from the user:
- **Stage subset**: "just run tests", "skip install", "build but don't open" — translates to `--skip-*` flags or `--stages=...`.
- **Target platform**: currently macOS only (DMG + `.app`). If the user asks for Windows/Linux, stop and flag it — the tool does not handle that yet.

## Tool
`tools/ci_pipeline.py`

### Stage contract

| Stage   | What it does                                                   | Failure means                          |
| ------- | -------------------------------------------------------------- | -------------------------------------- |
| clean   | `rm -rf electron-app/dist/` (old DMGs, blockmaps, unpacked)    | FS permissions issue — usually fatal   |
| install | `npm install` in `electron-app/`                               | network / registry / lockfile mismatch |
| test    | `node --test <file>` for each `**/__tests__/*.test.js`         | real test regression — do NOT proceed  |
| build   | `npm run dist` (electron-builder, mac arm64 + x64 DMG)         | codesign / icon / asarUnpack issue     |
| open    | Launch the `.app` matching host arch (`mac-arm64/` on Apple Silicon, `mac/` on Intel). `xattr -cr` to clear all xattrs, then `codesign --force --deep --sign -` to ad-hoc re-sign (rewrites CodeResources), `codesign --verify`, then `open -n -a`. | electron-builder skipped its own signing pass (no Developer ID) AND the ad-hoc re-sign also failed — inspect the `codesign` output. |

### Invocation patterns

```bash
# Full pipeline
uv run tools/ci_pipeline.py

# Fast inner loop — tests only
uv run tools/ci_pipeline.py --stages test

# Rebuild without reinstalling deps
uv run tools/ci_pipeline.py --skip-install

# CI-style: clean, install, test, build, but don't launch
uv run tools/ci_pipeline.py --no-open
```

## Expected Outputs
- `electron-app/dist/Garmin Data Exporter-<ver>-arm64.dmg`
- `electron-app/dist/Garmin Data Exporter-<ver>.dmg` (x64)
- `electron-app/dist/mac-arm64/Garmin Data Exporter.app/`
- `electron-app/dist/mac/Garmin Data Exporter.app/`
- On success, exit 0 and the newest `.app` opens in Finder (unless `--no-open`).

## Edge Cases & Known Quirks

- **Linker-only signatures silently block launch.** If electron-builder can't find a "Developer ID Application" cert, it skips its own signing pass and leaves the executable with only the compiler/linker's ad-hoc signature (`flags=0x20002(adhoc,linker-signed)` in `codesign -dv`) and an *empty* `CodeResources`. `spctl --assess` rejects this with `code has no resources but signature indicates they must be present`, and Launch Services refuses to exec the bundle — even though `open -n -a` returns 0. The pipeline's `open` stage handles this by ad-hoc re-signing (`codesign --force --deep --sign -`), which rewrites `CodeResources` and makes the signature internally consistent. `spctl` will still reject (no Apple notarization), but Launch Services honors the valid internal signature.
- **Mixed test styles.** Some tests use `node:test` (`util/__tests__/redact.test.js`), others are plain scripts with a `main()` at the bottom (`garmin/__tests__/client.test.js`). Running `node --test <file>` handles both correctly — it picks up `node:test` registrations AND executes top-level code.
- **electron-builder signing prompts.** First build after a macOS update may prompt for keychain access. If the pipeline appears to hang during `build`, check Finder for a system dialog.
- **`asarUnpack` for sql-wasm.** `package.json` unpacks `node_modules/sql.js/dist/sql-wasm.wasm`. If tests pass but the built `.app` crashes on load, suspect this path first.
- **node_modules freshness.** `--skip-install` is fine between successive runs but NOT after a `package.json` / lockfile change. If you skipped and got weird build errors, drop the flag and retry.
- **Quarantine flag.** `Remove Quarantine.command` ships inside the DMG. Not run by the pipeline itself — only matters for end-users opening the DMG on a new machine.

## Self-Improvement Log
When a run fails, record the diagnosis here before moving on. Keep entries short (date + symptom + fix).

- 2026-04-22: `open` stage launched the x64 bundle on an arm64 host because both bundles shared an mtime and the sort was order-dependent. Fixed by detecting `platform.machine()` and preferring `dist/mac-arm64/*.app` on Apple Silicon, with fallback to the other arch.
- 2026-04-22: `open` returned exit 0 but the app never appeared — first guess was Gatekeeper quarantine. Fix attempt: strip `com.apple.quarantine` via `xattr -dr` before `open -n -a`. This did NOT resolve the issue.
- 2026-04-22: Deeper diagnosis with `codesign -dv` and `spctl --assess` revealed the real cause: electron-builder skipped signing (no Developer ID cert), leaving a linker-only signature with empty `CodeResources`. `spctl` rejection: "code has no resources but signature indicates they must be present". Real fix: `xattr -cr` (clear all xattrs) + `codesign --force --deep --sign -` (ad-hoc re-sign) before `open -n -a`. Verified: `codesign --verify` now reports "valid on disk / satisfies its Designated Requirement" and the app launches.
