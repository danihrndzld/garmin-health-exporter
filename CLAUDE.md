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

- `electron-app/` is the current shipped product (Electron, Node 18+, electron-builder).
- Tests use Node's built-in `node:test` in `electron-app/**/__tests__/*.test.js`. No Jest / Mocha.
- Build output: `electron-app/dist/*.dmg` and `electron-app/dist/mac*/*.app`.
- Build details: [electron-app/BUILD.md](electron-app/BUILD.md).

## Bottom Line

You sit between intent (workflows) and execution (tools). Read the SOP, call the right tool, recover from errors, and keep the framework sharper than you found it.
