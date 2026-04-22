#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Release tool for the Garmin Data Exporter Electron app.

Replays the v3.2.0 release flow end-to-end:

  preflight  -> verify clean tree, gh auth, no existing tag/release, deps present
  bump       -> edit electron-app/package.json, commit the bump on source branch
  merge      -> push source, fast-forward merge into main, push main
  build      -> run ci_pipeline.py (clean,install,test,build) so DMGs match new version
  tag        -> create vX.Y.Z annotated tag on main, push tag
  release    -> gh release create vX.Y.Z with both DMGs attached

Usage:
  uv run tools/release.py --bump minor                              # 3.1.0 -> 3.2.0
  uv run tools/release.py --bump patch                              # 3.2.0 -> 3.2.1
  uv run tools/release.py --bump major                              # 3.2.0 -> 4.0.0
  uv run tools/release.py --version 3.3.0                           # explicit
  uv run tools/release.py --bump minor --notes "Adds custom date range picker"
  uv run tools/release.py --bump minor --notes-file RELEASE_NOTES.md
  uv run tools/release.py --bump minor --dry-run                    # print, don't execute
  uv run tools/release.py --bump minor --skip-merge                 # release from current branch
  uv run tools/release.py --version 3.2.0 --skip-build              # reuse existing DMGs

Exit codes:
  0  success (release URL printed to stdout as the last line)
  1  stage failure (stderr contains details)
  2  invalid arguments / bad environment / preflight failure
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
APP_DIR = REPO_ROOT / "electron-app"
PACKAGE_JSON = APP_DIR / "package.json"
DIST_DIR = APP_DIR / "dist"
CI_PIPELINE = REPO_ROOT / "tools" / "ci_pipeline.py"
MAIN_BRANCH = "main"

SEMVER_RE = re.compile(r"^\d+\.\d+\.\d+$")


# ---------------------------------------------------------------------------
# Shell helpers
# ---------------------------------------------------------------------------

def log(msg: str, prefix: str = "==>") -> None:
    print(f"{prefix} {msg}", flush=True)


def run(
    cmd: list[str],
    cwd: Path | None = None,
    dry: bool = False,
    check: bool = True,
    capture: bool = False,
) -> subprocess.CompletedProcess:
    rel_cwd = cwd.relative_to(REPO_ROOT) if cwd else "."
    log(f"$ {' '.join(cmd)}  (cwd={rel_cwd})", prefix="  ")
    if dry:
        return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")
    proc = subprocess.run(
        cmd,
        cwd=cwd or REPO_ROOT,
        capture_output=capture,
        text=True,
    )
    if check and proc.returncode != 0:
        if capture:
            sys.stderr.write(proc.stderr or "")
        raise SystemExit(f"!! command failed (exit {proc.returncode}): {' '.join(cmd)}")
    return proc


def git(args: list[str], dry: bool = False, capture: bool = False, check: bool = True) -> subprocess.CompletedProcess:
    return run(["git", *args], cwd=REPO_ROOT, dry=dry, capture=capture, check=check)


# ---------------------------------------------------------------------------
# Version handling
# ---------------------------------------------------------------------------

def read_version() -> str:
    data = json.loads(PACKAGE_JSON.read_text())
    v = data.get("version")
    if not v or not SEMVER_RE.match(v):
        raise SystemExit(f"!! package.json has no valid version: {v!r}")
    return v


def bump(current: str, kind: str) -> str:
    major, minor, patch = (int(x) for x in current.split("."))
    if kind == "major":
        return f"{major + 1}.0.0"
    if kind == "minor":
        return f"{major}.{minor + 1}.0"
    if kind == "patch":
        return f"{major}.{minor}.{patch + 1}"
    raise SystemExit(f"!! unknown bump kind: {kind}")


def write_version(new: str, dry: bool = False) -> None:
    text = PACKAGE_JSON.read_text()
    # Narrow replacement: only the top-level "version" field, not the schemaVersion
    # or any dependency pinned at the same literal. electron-builder package.jsons
    # keep "version" as the second top-level key; match with its surrounding quotes.
    new_text, n = re.subn(
        r'("version":\s*")(\d+\.\d+\.\d+)(")',
        rf'\g<1>{new}\g<3>',
        text,
        count=1,
    )
    if n != 1:
        raise SystemExit("!! could not find top-level \"version\" field in package.json")
    if dry:
        log(f"would write new version {new} to {PACKAGE_JSON.relative_to(REPO_ROOT)}", prefix="  ")
        return
    PACKAGE_JSON.write_text(new_text)


# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

def working_tree_clean() -> tuple[bool, str]:
    proc = git(["status", "--porcelain"], capture=True)
    return (proc.stdout.strip() == ""), proc.stdout


def current_branch() -> str:
    return git(["rev-parse", "--abbrev-ref", "HEAD"], capture=True).stdout.strip()


def tag_exists_local(tag: str) -> bool:
    proc = git(["tag", "--list", tag], capture=True)
    return proc.stdout.strip() == tag


def tag_exists_remote(tag: str) -> bool:
    proc = git(["ls-remote", "--tags", "origin", f"refs/tags/{tag}"], capture=True)
    return bool(proc.stdout.strip())


def release_exists(tag: str) -> bool:
    proc = subprocess.run(
        ["gh", "release", "view", tag],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
    )
    return proc.returncode == 0


def gh_authed() -> bool:
    proc = subprocess.run(["gh", "auth", "status"], capture_output=True, text=True)
    return proc.returncode == 0


def preflight(new_version: str, source: str, skip_merge: bool, skip_build: bool) -> None:
    log("stage: preflight")

    if not PACKAGE_JSON.exists():
        raise SystemExit(f"!! {PACKAGE_JSON.relative_to(REPO_ROOT)} not found — wrong repo?")

    if not CI_PIPELINE.exists():
        raise SystemExit(f"!! {CI_PIPELINE.relative_to(REPO_ROOT)} not found — did the tool move?")

    clean, dirty = working_tree_clean()
    if not clean:
        raise SystemExit(f"!! working tree not clean — commit or stash first:\n{dirty}")

    if not gh_authed():
        raise SystemExit("!! gh CLI is not authenticated. Run `gh auth login` and retry.")

    branch = current_branch()
    if branch != source:
        raise SystemExit(
            f"!! current branch is {branch!r} but --source is {source!r}. "
            f"Check out {source} first or pass --source={branch}."
        )

    tag = f"v{new_version}"
    if tag_exists_local(tag):
        raise SystemExit(f"!! local tag {tag} already exists")
    if tag_exists_remote(tag):
        raise SystemExit(f"!! remote tag {tag} already exists on origin")
    if release_exists(tag):
        raise SystemExit(f"!! GitHub release {tag} already exists")

    if not skip_merge and source == MAIN_BRANCH:
        log("source is main — will skip merge stage automatically", prefix="  ")

    if skip_build:
        arm = DIST_DIR / f"Garmin Data Exporter-{new_version}-arm64.dmg"
        x64 = DIST_DIR / f"Garmin Data Exporter-{new_version}.dmg"
        missing = [p for p in (arm, x64) if not p.exists()]
        if missing:
            rel = ", ".join(p.relative_to(REPO_ROOT).as_posix() for p in missing)
            raise SystemExit(f"!! --skip-build set but DMGs missing: {rel}")

    log("preflight ok", prefix="  ")


# ---------------------------------------------------------------------------
# Stages
# ---------------------------------------------------------------------------

def stage_bump(current: str, new_version: str, dry: bool) -> None:
    log(f"stage: bump  {current} -> {new_version}")
    write_version(new_version, dry=dry)
    git(["add", str(PACKAGE_JSON.relative_to(REPO_ROOT))], dry=dry)
    git(
        [
            "commit",
            "-m",
            f"chore(release): bump to v{new_version}",
        ],
        dry=dry,
    )


def stage_merge(source: str, dry: bool) -> None:
    log(f"stage: merge  {source} -> {MAIN_BRANCH} (fast-forward only)")
    git(["push", "-u", "origin", source], dry=dry)
    git(["fetch", "origin", MAIN_BRANCH], dry=dry)
    git(["checkout", MAIN_BRANCH], dry=dry)
    git(["pull", "--ff-only", "origin", MAIN_BRANCH], dry=dry)
    git(["merge", "--ff-only", source], dry=dry)
    git(["push", "origin", MAIN_BRANCH], dry=dry)


def stage_build(dry: bool) -> None:
    log("stage: build  (via ci_pipeline.py: clean,install,test,build; no open)")
    run(
        [
            "uv",
            "run",
            str(CI_PIPELINE.relative_to(REPO_ROOT)),
            "--no-open",
        ],
        cwd=REPO_ROOT,
        dry=dry,
    )


def stage_tag(new_version: str, dry: bool) -> None:
    tag = f"v{new_version}"
    log(f"stage: tag  {tag}")
    git(["tag", "-a", tag, "-m", f"Release {tag}"], dry=dry)
    git(["push", "origin", tag], dry=dry)


def stage_release(new_version: str, notes: str, dry: bool) -> str:
    tag = f"v{new_version}"
    log(f"stage: release  {tag}")

    arm = DIST_DIR / f"Garmin Data Exporter-{new_version}-arm64.dmg"
    x64 = DIST_DIR / f"Garmin Data Exporter-{new_version}.dmg"
    for p in (arm, x64):
        if not dry and not p.exists():
            raise SystemExit(f"!! expected DMG missing: {p.relative_to(REPO_ROOT)}")

    cmd = [
        "gh", "release", "create", tag,
        "--title", tag,
        "--notes", notes,
        str(arm),
        str(x64),
    ]
    proc = run(cmd, cwd=REPO_ROOT, dry=dry, capture=True)
    url = proc.stdout.strip().splitlines()[-1] if proc.stdout else ""
    if url:
        print(url)
    return url


# ---------------------------------------------------------------------------
# Notes body
# ---------------------------------------------------------------------------

def default_notes(new_version: str, source: str) -> str:
    # Grab commits on the source branch that aren't in origin/main yet.
    proc = git(
        ["log", f"origin/{MAIN_BRANCH}..{source}", "--pretty=format:- %s"],
        capture=True,
        check=False,
    )
    commits = proc.stdout.strip() if proc.returncode == 0 else ""
    if not commits:
        commits = "- (no new commits detected vs origin/main)"
    return (
        f"# v{new_version}\n\n"
        f"## Changes\n{commits}\n\n"
        "## Install\n"
        f"- Apple Silicon: `Garmin Data Exporter-{new_version}-arm64.dmg`\n"
        f"- Intel Mac: `Garmin Data Exporter-{new_version}.dmg`\n\n"
        "Unsigned build — after installing, run `Remove Quarantine.command` inside the DMG once."
    )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Ship a release of the Garmin Data Exporter.")
    g = p.add_mutually_exclusive_group(required=True)
    g.add_argument("--bump", choices=("patch", "minor", "major"), help="semver bump kind")
    g.add_argument("--version", help="explicit X.Y.Z (overrides --bump)")

    p.add_argument("--source", default=None, help="source branch (default: current branch)")
    p.add_argument("--notes", help="release notes body")
    p.add_argument("--notes-file", help="read release notes from this file")
    p.add_argument("--dry-run", action="store_true", help="print every command without executing")
    p.add_argument("--skip-merge", action="store_true", help="skip merge into main (release from current branch)")
    p.add_argument("--skip-build", action="store_true", help="reuse existing DMGs in electron-app/dist/")

    args = p.parse_args()

    if args.version and not SEMVER_RE.match(args.version):
        p.error(f"--version must be X.Y.Z, got {args.version!r}")
    if args.notes and args.notes_file:
        p.error("--notes and --notes-file are mutually exclusive")
    return args


def main() -> int:
    args = parse_args()

    current = read_version()
    new_version = args.version or bump(current, args.bump)
    if new_version == current:
        sys.stderr.write(f"!! new version ({new_version}) equals current — nothing to do\n")
        return 2

    source = args.source or current_branch()
    skip_merge = args.skip_merge or source == MAIN_BRANCH

    if args.notes_file:
        notes = Path(args.notes_file).read_text()
    elif args.notes:
        notes = args.notes
    else:
        notes = default_notes(new_version, source)

    log(f"release plan: {current} -> {new_version}  (source: {source}, dry-run: {args.dry_run})")

    preflight(new_version, source, skip_merge=skip_merge, skip_build=args.skip_build)
    stage_bump(current, new_version, dry=args.dry_run)
    if not skip_merge:
        stage_merge(source, dry=args.dry_run)
    else:
        log("stage: merge  SKIPPED (on main or --skip-merge)", prefix="  ")
        # Still push the bump commit so the tag-release below references an
        # origin-visible SHA.
        git(["push", "origin", source], dry=args.dry_run)
    if not args.skip_build:
        stage_build(dry=args.dry_run)
    else:
        log("stage: build  SKIPPED (--skip-build)", prefix="  ")
    stage_tag(new_version, dry=args.dry_run)
    url = stage_release(new_version, notes, dry=args.dry_run)

    log(f"release complete: v{new_version}")
    if not url:
        log("(dry run — no release URL)", prefix="  ")
    return 0


if __name__ == "__main__":
    sys.exit(main())
