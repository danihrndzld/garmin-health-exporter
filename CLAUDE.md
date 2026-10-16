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

- `tauri-app/` is the Tauri 2 rewrite (v4.0.0, not yet released). Vanilla HTML/CSS/JS in `tauri-app/ui/` (no bundler); the Garmin export runs in the Go core `garmin-export-core` (below); the Bun sidecar `garmin-core` (`bun build --compile tauri-app/sidecar/cli.js`, code in `electron-app/garmin/`) builds the summary, the LLM pack and the bug report, and still runs the old export when `ge_engine` is `'bun'`. Sidecar protocol: one JSON request line on stdin (password never in argv), JSON lines out (`log` / `progress` / `result`). Each binary is a trust boundary: range, refresh window and `$HOME` containment are validated there. Dev: `(cd electron-app && npm install) && cd tauri-app && bun install && bun run dev` (the sidecar build reads `electron-app/node_modules/sql.js` and `electron-app/renderer/date-range.js`); build: `bun run build` (host arch); release: `bun run release` (universal DMG, needs rustup targets aarch64 + x86_64-apple-darwin; Homebrew rust lacks x86_64, so v4.0.0 was built with an isolated rustup via RUSTUP_HOME/CARGO_HOME). Never `tauri build` in this checkout while the user runs the app from `src-tauri/target/release/bundle`; build in a `git worktree` instead. v4 is published by hand (`gh release create`), not by `tools/release.py`. Preview the UI in a plain browser via `ui/dev-mock.js` (`python3 -m http.server -d tauri-app/ui`). `ci_pipeline.py` / `release.py` still target `electron-app/` only.
- Go core (`tauri-app/core`, binary `garmin-export-core`; stage 1 spec `docs/superpowers/specs/2026-10-14-go-core-mfp-design.md`, stage 2 spec `docs/superpowers/specs/2026-10-16-go-core-garmin-design.md`, architecture `docs/superpowers/specs/2026-10-16-architecture-hexagonal-gconnect.md`). Commands: `garmin-status` (never sends a request), `garmin-connect` (the only command that signs in; password on stdin only), `garmin-disconnect`, `clear-cache`, `export` (Garmin; `garmin:false` only makes the `csv_*` dir) and `mfp-export`. `summary`, `pack` and `report` stay in Bun until stage 3. The data dir holds `tokens.json` (0600, the exact shapes of `auth.js` plus `saved_at`), `sso-cooldown.json`, `garmin-cache/daily/<date>.json` and `garmin-cache/activity/<id>.json`, `mfp_session.json` and `mfp-cache/`; Clear cache deletes `garmin_cache.db`, `garmin-cache/` and `mfp-cache/`, never the logins. A dead login (400/401 from the DI refresh grant, 401 from the legacy OAuth2 exchange, or a second 401 on data after a refresh) replaces `tokens.json` with the expired marker `{"expired_at","saved_at"}`, which holds no secret; a network error, 5xx, 403 or 429 keeps the tokens. `GARMIN_FAKE_SERVER` and `MFP_FAKE_SERVER` are read by Go too, loopback URLs only, on every request. The UI picks the binary for `export` with `localStorage.ge_engine` (`DEFAULT_ENGINE = 'go'` in `ui/app.js`; `'bun'` runs the old sidecar export; going back is a one line commit). Go reads the golden CSV files in `electron-app/garmin/__tests__/golden` (read only) until stage 3 moves them. The Go binary never opens `garmin_cache.db` (sql.js rewrites the whole file, so two writers would erase each other). Rust (`mfp_connect` / `mfp_disconnect` / `mfp_status`) writes `mfp_session.json`; Go only reads it and rewrites it on cookie rotation.
- Go layout and its strict rules (Hexagonal, enforced by `tauri-app/core/arch_test.go`, which `go test .` runs): `cmd/garmin-export-core` is the only place that reads the environment, stdin, stdout or args and the only importer of `internal/protocol`; `internal/garmin` and `internal/mfp` never import each other; `gconnect/` (the Garmin sign in and API client, in tree until its own repository after stage 2) imports the standard library only and never `os`, `path/filepath`, `io/fs` or `syscall`, so it cannot touch the disk; `gconnect/gconnecttest` is the fake Garmin every test uses. Standard library only, `CGO_ENABLED=0`, universal binary through `lipo`. Tests: `cd tauri-app && bun run test:core` and `cd tauri-app/core && go test -race ./...`.
- Garmin login (`tauri-app/core/gconnect/auth.go`, ported from `electron-app/garmin/auth.js`): only `garmin-connect` signs in, at most twice (mobile, then widget); an export never signs in and ends with `GARMIN_SESSION` when the login is missing or dead. DI OAuth first (SSO `/mobile/api/login` -> `diauth.garmin.com` token, as python-garminconnect 0.3.x), the old garth widget + OAuth1 flow once as fallback. DI tokens need the native `X-Garmin-*` headers on connectapi (403 without them). A 429 on both logins writes `sso-cooldown.json` (15 min). Never run live login experiments in a loop: Garmin rate limits sign ins per account (429 after 4 logins in 40 minutes on 2026-10-14).
- Tests (trophy shape): `cd electron-app && npm test` runs integration tests against `__tests__/helpers/fake-garmin.js` (set `GARMIN_FAKE_SERVER=http://127.0.0.1:<port>`; only loopback URLs are accepted) and the golden CSV regression (`__tests__/golden`, rebuild with `UPDATE_GOLDEN=1`). `cd tauri-app && bun test sidecar` includes sidecar end to end tests. The fixture is scrubbed by `scripts/make-fixture.js` with a per-run secret; never commit raw export JSON. Compare two `csv_*` folders with `node electron-app/scripts/compare-csv.js <old> <new>`.
- UI e2e regression (`tauri-app/e2e`, Playwright, Chromium + WebKit): `cd tauri-app && bunx playwright test`. Black box through `ui/dev-mock.js` served by `e2e/support/serve-ui.js`. Page Object Model: one page object per component in `e2e/pages/`, mock switches only in `e2e/support/mock-backend.js` (it maps intent to the mock's `localStorage` keys), "today" pinned by `FixedToday`, wiring only in `e2e/fixtures.js`, assertions only in specs. Every test name starts with its technique ID (EP, BVA, ST, DT, EG, CL; new Go IDs start at 201). Test reports go to `tauri-app/reportes-pruebas/` (gitignored).
- LLM pack (`tauri-app/sidecar/llm-pack.js`, test: `bun test sidecar`): the export writes no pack files; `previewPack` builds both languages in memory for the sheet. The sheet's buttons call the sidecar `pack` command, which writes ONE language into its own subfolder of the `csv_*` dir (`llm-<lang>/` = `CONTEXT.<lang>.md` + CSV copies, `llm-<lang>-one-file/` = context + all CSVs inline) and deletes v4.0.0's root-level pack files, so dragging a whole folder never duplicates data. Units are inferred from column-name suffixes in `UNITS`; check new columns there. The raw JSON is never packed (a 60-day export is ~38 MB, over Claude's 30 MB upload cap).
- `electron-app/` is the current shipped product (Electron, Node 18+, electron-builder).
- Tests use Node's built-in `node:test` in `electron-app/**/__tests__/*.test.js`. No Jest / Mocha.
- Build output: `electron-app/dist/*.dmg` and `electron-app/dist/mac*/*.app`.
- Build details: [electron-app/BUILD.md](electron-app/BUILD.md).

## Bottom Line

You sit between intent (workflows) and execution (tools). Read the SOP, call the right tool, recover from errors, and keep the framework sharper than you found it.
