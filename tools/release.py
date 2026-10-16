#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Release tool for the Garmin Export Tauri app (tauri-app/).

  preflight  -> clean tree, gh auth, no tag or release vX.Y.Z, tauri-app/package.json present
  bump       -> version in tauri-app/package.json, src-tauri/tauri.conf.json,
                src-tauri/Cargo.toml and Cargo.lock (cargo update --workspace --offline);
                commit the four files on the source branch
  merge      -> push source, fast forward merge into main, push main
  build      -> ci_pipeline.py --no-open (universal DMG built in a git worktree)
  verify     -> hdiutil verify on the DMG; lipo, Info.plist version and
                codesign --verify --deep --strict on tauri-app/dist/Garmin Export.app
  tag        -> annotated tag vX.Y.Z, push the tag
  release    -> gh release create vX.Y.Z with the one universal DMG

Usage:
  uv run tools/release.py --bump minor                      # 4.4.0 -> 4.5.0
  uv run tools/release.py --bump patch --notes "Fixes the date picker."
  uv run tools/release.py --version 5.0.0 --notes-file NOTES.md --prerelease
  uv run tools/release.py --bump minor --dry-run            # print, don't execute
  uv run tools/release.py --bump minor --skip-merge         # release from current branch
  uv run tools/release.py --version 4.5.0 --skip-build      # reuse the DMG in tauri-app/dist/
  uv run tools/release.py --bump minor --rustup-home ~/rust/rustup --cargo-home ~/rust/cargo

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
import shlex
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
APP_DIR = REPO_ROOT / "tauri-app"
PACKAGE_JSON = APP_DIR / "package.json"
TAURI_CONF = APP_DIR / "src-tauri" / "tauri.conf.json"
CARGO_TOML = APP_DIR / "src-tauri" / "Cargo.toml"
CARGO_LOCK = APP_DIR / "src-tauri" / "Cargo.lock"
DIST_DIR = APP_DIR / "dist"
APP_BUNDLE = DIST_DIR / "Garmin Export.app"
NOTES_DIR = REPO_ROOT / ".tmp"
CI_PIPELINE = REPO_ROOT / "tools" / "ci_pipeline.py"
MAIN_BRANCH = "main"

SEMVER_RE = re.compile(r"^\d+\.\d+\.\d+$")
ENV = os.environ.copy()


# ---------------------------------------------------------------------------
# Shell helpers
# ---------------------------------------------------------------------------

def log(msg: str, prefix: str = "==>") -> None:
    print(f"{prefix} {msg}", flush=True)


def rel(p: Path) -> str:
    try:
        return p.relative_to(REPO_ROOT).as_posix() or "."
    except ValueError:
        return str(p)


def run(
    cmd: list[str],
    cwd: Path | None = None,
    dry: bool = False,
    check: bool = True,
    capture: bool = False,
) -> subprocess.CompletedProcess:
    log(f"$ {shlex.join(cmd)}  (cwd={rel(cwd or REPO_ROOT)})", prefix="  ")
    if dry:
        return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")
    proc = subprocess.run(cmd, cwd=cwd or REPO_ROOT, env=ENV, capture_output=capture, text=True)
    if check and proc.returncode != 0:
        if capture:
            sys.stderr.write(proc.stderr or "")
        raise SystemExit(f"!! command failed (exit {proc.returncode}): {shlex.join(cmd)}")
    return proc


def git(args: list[str], dry: bool = False, capture: bool = False, check: bool = True) -> subprocess.CompletedProcess:
    return run(["git", *args], cwd=REPO_ROOT, dry=dry, capture=capture, check=check)


# ---------------------------------------------------------------------------
# Version handling
# ---------------------------------------------------------------------------

def read_version() -> str:
    v = json.loads(PACKAGE_JSON.read_text()).get("version")
    if not v or not SEMVER_RE.match(v):
        raise SystemExit(f"!! tauri-app/package.json has no valid version: {v!r}")
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


def bump_json(text: str, current: str, new: str) -> str:
    """Top level "version" of package.json / tauri.conf.json. Only the field that
    holds the current version is replaced, so a file that drifted fails loudly."""
    if json.loads(text).get("version") != current:
        raise SystemExit(f"!! top level \"version\" is not {current}")
    out, n = re.subn(rf'("version":\s*"){re.escape(current)}(")', rf"\g<1>{new}\g<2>", text, count=1)
    if n != 1:
        raise SystemExit("!! could not find the top level \"version\" field")
    return out


def bump_cargo_toml(text: str, current: str, new: str) -> str:
    """The first `version = "..."` line, which is the [package] version."""
    m = re.search(r'^version = "([^"]*)"', text, flags=re.M)
    if not m or m.group(1) != current:
        raise SystemExit(f"!! first version line of Cargo.toml is not {current}: {m and m.group(0)!r}")
    return text[: m.start(1)] + new + text[m.end(1):]


def write_versions(current: str, new: str, dry: bool) -> None:
    for path, fn in ((PACKAGE_JSON, bump_json), (TAURI_CONF, bump_json), (CARGO_TOML, bump_cargo_toml)):
        new_text = fn(path.read_text(), current, new)
        log(f"{'would write' if dry else 'writing'} version {new} to {rel(path)}", prefix="  ")
        if not dry:
            path.write_text(new_text)


# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

def working_tree_clean() -> tuple[bool, str]:
    proc = git(["status", "--porcelain"], capture=True)
    return (proc.stdout.strip() == ""), proc.stdout


def current_branch() -> str:
    return git(["rev-parse", "--abbrev-ref", "HEAD"], capture=True).stdout.strip()


def tag_exists_local(tag: str) -> bool:
    return git(["tag", "--list", tag], capture=True).stdout.strip() == tag


def tag_exists_remote(tag: str) -> bool:
    return bool(git(["ls-remote", "--tags", "origin", f"refs/tags/{tag}"], capture=True).stdout.strip())


def release_exists(tag: str) -> bool:
    return subprocess.run(["gh", "release", "view", tag], cwd=REPO_ROOT, capture_output=True, text=True).returncode == 0


def gh_authed() -> bool:
    return subprocess.run(["gh", "auth", "status"], capture_output=True, text=True).returncode == 0


def dmg_path(version: str) -> Path:
    return DIST_DIR / f"Garmin Export_{version}_universal.dmg"


def preflight(new_version: str, source: str, skip_merge: bool, skip_build: bool) -> None:
    log("stage: preflight")

    if not PACKAGE_JSON.exists():
        raise SystemExit(f"!! {rel(PACKAGE_JSON)} not found; wrong repo?")
    if not CI_PIPELINE.exists():
        raise SystemExit(f"!! {rel(CI_PIPELINE)} not found; did the tool move?")

    clean, dirty = working_tree_clean()
    if not clean:
        raise SystemExit(f"!! working tree not clean; commit first:\n{dirty}")

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
        log("source is main; the merge stage is skipped", prefix="  ")

    if skip_build and not dmg_path(new_version).exists():
        raise SystemExit(f"!! --skip-build set but the DMG is missing: {rel(dmg_path(new_version))}")

    log("preflight ok", prefix="  ")


# ---------------------------------------------------------------------------
# Stages
# ---------------------------------------------------------------------------

def stage_bump(current: str, new_version: str, dry: bool) -> None:
    log(f"stage: bump  {current} -> {new_version}")
    write_versions(current, new_version, dry=dry)
    # Refreshes the garmin-export entry of Cargo.lock without touching the network.
    run(["cargo", "update", "--workspace", "--offline"], cwd=CARGO_TOML.parent, dry=dry)
    git(["add", *(rel(p) for p in (PACKAGE_JSON, TAURI_CONF, CARGO_TOML, CARGO_LOCK))], dry=dry)
    git(["commit", "-m", f"chore(release): bump to v{new_version}"], dry=dry)


def stage_merge(source: str, dry: bool) -> None:
    log(f"stage: merge  {source} -> {MAIN_BRANCH} (fast forward only)")
    git(["push", "-u", "origin", source], dry=dry)
    git(["fetch", "origin", MAIN_BRANCH], dry=dry)
    git(["checkout", MAIN_BRANCH], dry=dry)
    git(["pull", "--ff-only", "origin", MAIN_BRANCH], dry=dry)
    git(["merge", "--ff-only", source], dry=dry)
    git(["push", "origin", MAIN_BRANCH], dry=dry)


def stage_build(dry: bool) -> None:
    log("stage: build  (via ci_pipeline.py: clean,install,test,build; no open)")
    run(["uv", "run", rel(CI_PIPELINE), "--no-open"], cwd=REPO_ROOT, dry=dry)


def stage_verify(new_version: str, dry: bool) -> None:
    log(f"stage: verify  {rel(dmg_path(new_version))}")
    dmg = dmg_path(new_version)
    if not dry and not (dmg.exists() and APP_BUNDLE.is_dir()):
        raise SystemExit(f"!! expected {rel(dmg)} and {rel(APP_BUNDLE)}")

    run(["hdiutil", "verify", rel(dmg)], dry=dry, capture=True)

    exes = sorted((APP_BUNDLE / "Contents" / "MacOS").glob("*")) or [APP_BUNDLE / "Contents" / "MacOS" / "*"]
    for exe in exes:
        archs = run(["lipo", "-archs", rel(exe)], dry=dry, capture=True).stdout.split()
        if not dry and not {"arm64", "x86_64"} <= set(archs):
            raise SystemExit(f"!! {rel(exe)} is not universal: {archs}")

    plist = APP_BUNDLE / "Contents" / "Info.plist"
    got = run(
        ["/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", rel(plist)], dry=dry, capture=True
    ).stdout.strip()
    if not dry and got != new_version:
        raise SystemExit(f"!! Info.plist version is {got!r}, expected {new_version}")

    run(["codesign", "--verify", "--deep", "--strict", rel(APP_BUNDLE)], dry=dry)


def stage_tag(new_version: str, dry: bool) -> None:
    tag = f"v{new_version}"
    log(f"stage: tag  {tag}")
    git(["tag", "-a", tag, "-m", f"Release {tag}"], dry=dry)
    git(["push", "origin", tag], dry=dry)


def stage_release(new_version: str, notes: str, prerelease: bool, dry: bool) -> str:
    tag = f"v{new_version}"
    log(f"stage: release  {tag}")
    # The notes go through a file so the printed command can be pasted as is
    # when the agent may not publish.
    notes_file = NOTES_DIR / f"release-notes-{tag}.md"
    log(f"{'would write' if dry else 'writing'} notes to {rel(notes_file)}", prefix="  ")
    if not dry:
        NOTES_DIR.mkdir(exist_ok=True)
        notes_file.write_text(notes)

    cmd = ["gh", "release", "create", tag, rel(dmg_path(new_version)), "--title", tag, "--notes-file", rel(notes_file)]
    if prerelease:
        cmd.append("--prerelease")
    proc = run(cmd, cwd=REPO_ROOT, dry=dry, capture=True)
    url = proc.stdout.strip().splitlines()[-1] if proc.stdout else ""
    if url:
        print(url)
    return url


# ---------------------------------------------------------------------------
# Notes body
# ---------------------------------------------------------------------------

def default_notes(new_version: str, source: str) -> str:
    proc = git(["log", f"origin/{MAIN_BRANCH}..{source}", "--pretty=format:- %s"], capture=True, check=False)
    commits = proc.stdout.strip() if proc.returncode == 0 else ""
    if not commits:
        commits = "- (no new commits found against origin/main)"
    return (
        f"# v{new_version}\n\n"
        f"## Changes\n{commits}\n\n"
        "## Install\n"
        f"Download `Garmin.Export_{new_version}_universal.dmg`. The one DMG runs on Apple Silicon and Intel Macs.\n\n"
        "The app has an ad hoc signature, not an Apple Developer ID, so macOS asks before the first launch. "
        "The first time, right click the app, choose Open, and confirm. After that it opens normally.\n"
    )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Ship a release of Garmin Export.")
    g = p.add_mutually_exclusive_group(required=True)
    g.add_argument("--bump", choices=("patch", "minor", "major"), help="semver bump kind")
    g.add_argument("--version", help="explicit X.Y.Z (overrides --bump)")

    p.add_argument("--source", default=None, help="source branch (default: current branch)")
    p.add_argument("--notes", help="release notes body")
    p.add_argument("--notes-file", help="read release notes from this file")
    p.add_argument("--prerelease", action="store_true", help="publish as a GitHub pre-release")
    p.add_argument("--dry-run", action="store_true", help="print every command without executing")
    p.add_argument("--skip-merge", action="store_true", help="skip merge into main (release from current branch)")
    p.add_argument("--skip-build", action="store_true", help="reuse the DMG in tauri-app/dist/")
    p.add_argument("--rustup-home", help="RUSTUP_HOME for the build (default: env)")
    p.add_argument("--cargo-home", help="CARGO_HOME for the build (default: env)")

    args = p.parse_args()

    if args.version and not SEMVER_RE.match(args.version):
        p.error(f"--version must be X.Y.Z, got {args.version!r}")
    if args.notes and args.notes_file:
        p.error("--notes and --notes-file are mutually exclusive")
    return args


def main() -> int:
    args = parse_args()
    # ci_pipeline.py reads these from the environment and puts $CARGO_HOME/bin first on PATH.
    if args.rustup_home:
        ENV["RUSTUP_HOME"] = args.rustup_home
    if args.cargo_home:
        ENV["CARGO_HOME"] = args.cargo_home

    current = read_version()
    new_version = args.version or bump(current, args.bump)
    if new_version == current:
        sys.stderr.write(f"!! new version ({new_version}) equals current; nothing to do\n")
        return 2

    source = args.source or current_branch()
    skip_merge = args.skip_merge or source == MAIN_BRANCH

    if args.notes_file:
        notes = Path(args.notes_file).read_text()
    elif args.notes:
        notes = args.notes
    else:
        notes = default_notes(new_version, source)

    log(f"release plan: {current} -> {new_version}  (source: {source}, prerelease: {args.prerelease}, dry-run: {args.dry_run})")

    preflight(new_version, source, skip_merge=skip_merge, skip_build=args.skip_build)
    stage_bump(current, new_version, dry=args.dry_run)
    if not skip_merge:
        stage_merge(source, dry=args.dry_run)
    else:
        log("stage: merge  SKIPPED (on main or --skip-merge)", prefix="  ")
        # Still push the bump commit so the tag references an origin visible SHA.
        git(["push", "origin", source], dry=args.dry_run)
    if not args.skip_build:
        stage_build(dry=args.dry_run)
    else:
        log("stage: build  SKIPPED (--skip-build)", prefix="  ")
    stage_verify(new_version, dry=args.dry_run)
    stage_tag(new_version, dry=args.dry_run)
    url = stage_release(new_version, notes, args.prerelease, dry=args.dry_run)

    log(f"release complete: v{new_version}")
    if not url:
        log("(dry run, no release URL)", prefix="  ")
    return 0


if __name__ == "__main__":
    sys.exit(main())
