# GarminDownloader — Agent Instructions

This repo is managed with the **WAT framework** (Workflows, Agents, Tools). Probabilistic AI orchestrates; deterministic code executes. Keep the layers separate and the system stays reliable.

## WAT Layout

- [workflows/](workflows/) — markdown SOPs. Each one states objective, inputs, tools, outputs, and edge cases.
- [tools/](tools/) — Python scripts that do the actual work (API calls, builds, transforms). Run via `uv run tools/<name>.py` — every tool has a PEP 723 header declaring its Python / dependency requirements, so uv resolves and executes in one step.
- [.tmp/](.tmp/) — disposable intermediates. Gitignored. Regenerate freely.
- [.env](.env) — API keys and secrets. Gitignored. Never store secrets anywhere else.
- `credentials.json`, `token.json` — Google OAuth. Gitignored.

Everything the user needs to *see* lives in cloud services (Sheets, Slides, etc.) or in `electron-app/dist/` for app builds. Local scratch files are processing-only.

## How to Operate

1. **Match the request to a workflow.** Read the workflow end-to-end before acting.
2. **Look for an existing tool first.** Scan `tools/` before writing anything new.
3. **Run tools, don't imitate them.** Probabilistic reasoning + deterministic execution is the whole point.
4. **On failure: diagnose, fix the tool, verify, then update the workflow** so the same error can't recur silently. If the fix costs paid API credits to retest, confirm with the user first.
5. **Never overwrite a workflow without permission.** Workflows are instructions — preserve and refine them.

## Available Workflows

| Workflow | When to run |
| -------- | ----------- |
| [workflows/cicd_pipeline.md](workflows/cicd_pipeline.md) | Rebuild the Electron app end-to-end: clean dist, install, test, build DMG, launch. |
| [workflows/release.md](workflows/release.md) | Cut a new version: bump package.json, FF-merge into main, rebuild DMGs, tag, and publish a GitHub release with both DMGs attached. |

## Available Tools

| Tool | Purpose | Invocation |
| ---- | ------- | ---------- |
| [tools/ci_pipeline.py](tools/ci_pipeline.py) | Full CI/CD for `electron-app/` (clean → install → test → build → open). Arch-aware open stage. Supports `--stages`, `--skip-install`, `--skip-tests`, `--skip-build`, `--no-open`. | `uv run tools/ci_pipeline.py [flags]` |
| [tools/release.py](tools/release.py) | End-to-end release: preflight → bump → FF-merge into main → build (delegates to `ci_pipeline.py`) → tag → `gh release create` with both DMGs. Supports `--bump {patch,minor,major}` or `--version X.Y.Z`, `--notes` / `--notes-file`, `--skip-merge`, `--skip-build`, `--dry-run`. | `uv run tools/release.py --bump <kind> [flags]` |

Legacy Python scripts at the repo root (`garmin_health_export.py`, `download_activity_details.py`, `json_to_csv.py`) predate the WAT structure. They are not wrapped as workflow tools yet — leave them alone unless the user asks for migration.

## Project Context (non-WAT)

- `tauri-app/` is the Tauri 2 rewrite (v4.0.0, not yet released). Vanilla HTML/CSS/JS in `tauri-app/ui/` (no bundler); the Garmin core stays in `electron-app/garmin/` and ships as the `garmin-core` sidecar (`bun build --compile tauri-app/sidecar/cli.js`). Sidecar protocol: one JSON request line on stdin (password never in argv), JSON lines out (`log` / `progress` / `result`). The sidecar is the trust boundary: range, refresh window and `$HOME` containment are validated there. Dev: `(cd electron-app && npm install) && cd tauri-app && bun install && bun run dev` (the sidecar build reads `electron-app/node_modules/sql.js` and `electron-app/renderer/date-range.js`); build: `bun run build` (host arch); release: `bun run release` (universal DMG, needs rustup targets aarch64 + x86_64-apple-darwin; Homebrew rust lacks x86_64, so v4.0.0 was built with an isolated rustup via RUSTUP_HOME/CARGO_HOME). Never `tauri build` in this checkout while the user runs the app from `src-tauri/target/release/bundle`; build in a `git worktree` instead. v4 is published by hand (`gh release create`), not by `tools/release.py`. Preview the UI in a plain browser via `ui/dev-mock.js` (`python3 -m http.server -d tauri-app/ui`). `ci_pipeline.py` / `release.py` still target `electron-app/` only.
- Garmin login (`electron-app/garmin/auth.js`): DI OAuth first (SSO `/mobile/api/login` -> `diauth.garmin.com` token, as python-garminconnect 0.3.x), the old garth widget + OAuth1 flow once as fallback. DI tokens need the native `X-Garmin-*` headers on connectapi (403 without them). A 429 on both logins writes `sso-cooldown.json` (15 min). Never run live login experiments in a loop: Garmin rate limits sign ins per account (429 after 4 logins in 40 minutes on 2026-10-14).
- Tests (trophy shape): `cd electron-app && npm test` runs integration tests against `__tests__/helpers/fake-garmin.js` (set `GARMIN_FAKE_SERVER=http://127.0.0.1:<port>`; only loopback URLs are accepted) and the golden CSV regression (`__tests__/golden`, rebuild with `UPDATE_GOLDEN=1`). `cd tauri-app && bun test sidecar` includes sidecar end to end tests. The fixture is scrubbed by `scripts/make-fixture.js` with a per-run secret; never commit raw export JSON. Compare two `csv_*` folders with `node electron-app/scripts/compare-csv.js <old> <new>`.
- LLM pack (`tauri-app/sidecar/llm-pack.js`, test: `bun test sidecar`): the export writes no pack files; `previewPack` builds both languages in memory for the sheet. The sheet's buttons call the sidecar `pack` command, which writes ONE language into its own subfolder of the `csv_*` dir (`llm-<lang>/` = `CONTEXT.<lang>.md` + CSV copies, `llm-<lang>-one-file/` = context + all CSVs inline) and deletes v4.0.0's root-level pack files, so dragging a whole folder never duplicates data. Units are inferred from column-name suffixes in `UNITS`; check new columns there. The raw JSON is never packed (a 60-day export is ~38 MB, over Claude's 30 MB upload cap).
- `electron-app/` is the current shipped product (Electron, Node 18+, electron-builder).
- Tests use Node's built-in `node:test` in `electron-app/**/__tests__/*.test.js`. No Jest / Mocha.
- Build output: `electron-app/dist/*.dmg` and `electron-app/dist/mac*/*.app`.
- Build details: [electron-app/BUILD.md](electron-app/BUILD.md).

## Bottom Line

You sit between intent (workflows) and execution (tools). Read the SOP, call the right tool, recover from errors, and keep the framework sharper than you found it.
