#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Release tool for the Garmin Export Tauri app (tauri-app/).

Nothing reaches origin until the DMG is built and verified:

  preflight  -> clean tree, gh auth, cargo on PATH, no tag or release vX.Y.Z,
                origin/main and main are ancestors of HEAD, the main checkout is clean
  bump       -> version in tauri-app/package.json, src-tauri/tauri.conf.json,
                src-tauri/Cargo.toml and Cargo.lock (cargo update --workspace --offline);
                one local commit on the source branch
  build      -> ci_pipeline.py --no-open (universal DMG built in a fresh git worktree of HEAD)
  verify     -> hdiutil verify on the DMG, then mount it read only and check the .app
                inside: lipo (arm64 + x86_64), Info.plist version, codesign --verify --deep --strict
  merge      -> fast forward main to the source branch without a checkout:
                git -C <main checkout> merge --ff-only <source>
  push       -> git push --atomic origin <source> main, then git tag -a vX.Y.Z and push the tag
  release    -> gh release create vX.Y.Z with the one universal DMG

A failure in build or verify leaves origin untouched and prints how to undo the
local bump commit.

Usage:
  uv run tools/release.py --bump minor                      # 4.4.0 -> 4.5.0
  uv run tools/release.py --bump patch --notes "Fixes the date picker."
  uv run tools/release.py --version 5.0.0 --notes-file NOTES.md --prerelease
  uv run tools/release.py --bump minor --dry-run            # print, don't execute
  uv run tools/release.py --bump minor --skip-merge         # release from current branch
  uv run tools/release.py --bump minor --rustup-home ~/rust/rustup --cargo-home ~/rust/cargo

Exit codes:
  0  success (release URL printed to stdout as the last line)
  1  bump, build, verify, merge, push or release failed (stderr contains details)
  2  refused before any change: invalid arguments or a preflight refusal
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
APP_DIR = REPO_ROOT / "tauri-app"
PACKAGE_JSON = APP_DIR / "package.json"
TAURI_CONF = APP_DIR / "src-tauri" / "tauri.conf.json"
CARGO_TOML = APP_DIR / "src-tauri" / "Cargo.toml"
CARGO_LOCK = APP_DIR / "src-tauri" / "Cargo.lock"
DIST_DIR = APP_DIR / "dist"
APP_NAME = "Garmin Export.app"
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
        refuse(f"!! tauri-app/package.json has no valid version: {v!r}")
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


def is_ancestor(ref: str) -> bool:
    return git(["merge-base", "--is-ancestor", ref, "HEAD"], capture=True, check=False).returncode == 0


def main_checkout() -> Path | None:
    """The worktree that has main checked out, if any."""
    path = None
    for line in git(["worktree", "list", "--porcelain"], capture=True).stdout.splitlines():
        if line.startswith("worktree "):
            path = Path(line[len("worktree "):])
        elif line == f"branch refs/heads/{MAIN_BRANCH}":
            return path
    return None


def dmg_path(version: str) -> Path:
    return DIST_DIR / f"Garmin Export_{version}_universal.dmg"


def refuse(msg: str) -> None:
    """A preflight refusal: nothing has changed yet, so exit 2 (not 1)."""
    sys.stderr.write(msg + "\n")
    raise SystemExit(2)


def preflight(new_version: str, source: str, skip_merge: bool, dry: bool) -> None:
    log("stage: preflight")

    if not PACKAGE_JSON.exists():
        refuse(f"!! {rel(PACKAGE_JSON)} not found; wrong repo?")
    if not CI_PIPELINE.exists():
        refuse(f"!! {rel(CI_PIPELINE)} not found; did the tool move?")
    if not shutil.which("cargo", path=ENV.get("PATH")):
        refuse("!! cargo is not on PATH (it refreshes Cargo.lock). Pass --cargo-home or fix PATH.")

    clean, dirty = working_tree_clean()
    if not clean:
        refuse(f"!! working tree not clean; commit first:\n{dirty}")

    if not gh_authed():
        refuse("!! gh CLI is not authenticated. Run `gh auth login` and retry.")

    branch = current_branch()
    if branch != source:
        refuse(
            f"!! current branch is {branch!r} but --source is {source!r}. "
            f"Check out {source} first or pass --source={branch}."
        )

    tag = f"v{new_version}"
    if tag_exists_local(tag):
        refuse(f"!! local tag {tag} already exists")
    if tag_exists_remote(tag):
        refuse(f"!! remote tag {tag} already exists on origin")
    if release_exists(tag):
        refuse(f"!! GitHub release {tag} already exists")

    if skip_merge:
        log("merge is skipped (on main or --skip-merge)", prefix="  ")
    else:
        # A non fast forward fails here, before any commit or build.
        git(["fetch", "origin", MAIN_BRANCH], dry=dry)
        for ref in (f"origin/{MAIN_BRANCH}", MAIN_BRANCH):
            if not is_ancestor(ref):
                refuse(f"!! {ref} is not an ancestor of {source}; rebase onto origin/{MAIN_BRANCH} first")
        mc = main_checkout()
        if mc is not None:
            dirty = git(["-C", str(mc), "status", "--porcelain"], capture=True).stdout.strip()
            if dirty:
                refuse(f"!! the main checkout {mc} has uncommitted changes:\n{dirty}")

    log("preflight ok", prefix="  ")


# ---------------------------------------------------------------------------
# Stages
# ---------------------------------------------------------------------------

def stage_bump(current: str, new_version: str, dry: bool) -> None:
    log(f"stage: bump  {current} -> {new_version}  (local commit only)")
    write_versions(current, new_version, dry=dry)
    # Refreshes the garmin-export entry of Cargo.lock without touching the network.
    run(["cargo", "update", "--workspace", "--offline"], cwd=CARGO_TOML.parent, dry=dry)
    git(["add", *(rel(p) for p in (PACKAGE_JSON, TAURI_CONF, CARGO_TOML, CARGO_LOCK))], dry=dry)
    git(["commit", "-m", f"chore(release): bump to v{new_version}"], dry=dry)


def stage_build(dry: bool) -> None:
    log("stage: build  (via ci_pipeline.py: clean,install,test,build; no open)")
    # In a dry run the child runs with --dry-run, so its own commands are printed too.
    run(["uv", "run", rel(CI_PIPELINE), "--no-open", *(["--dry-run"] if dry else [])], cwd=REPO_ROOT)


def stage_verify(new_version: str, dry: bool) -> None:
    dmg = dmg_path(new_version)
    log(f"stage: verify  {rel(dmg)}")
    if not dry and not dmg.exists():
        raise SystemExit(f"!! expected DMG missing: {rel(dmg)}")

    run(["hdiutil", "verify", rel(dmg)], dry=dry, capture=True)
    mnt = Path("<mount>") if dry else Path(tempfile.mkdtemp(prefix="garmin-export-dmg-"))
    run(["hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", str(mnt), rel(dmg)], dry=dry, capture=True)
    try:
        app = mnt / APP_NAME
        exes = sorted((app / "Contents" / "MacOS").glob("*")) or [app / "Contents" / "MacOS" / "*"]
        for exe in exes:
            archs = run(["lipo", "-archs", str(exe)], dry=dry, capture=True).stdout.split()
            if not dry and not {"arm64", "x86_64"} <= set(archs):
                raise SystemExit(f"!! {exe.name} in the DMG is not universal: {archs}")

        got = run(
            ["/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", str(app / "Contents" / "Info.plist")],
            dry=dry,
            capture=True,
        ).stdout.strip()
        if not dry and got != new_version:
            raise SystemExit(f"!! Info.plist version in the DMG is {got!r}, expected {new_version}")

        run(["codesign", "--verify", "--deep", "--strict", str(app)], dry=dry)
    finally:
        run(["hdiutil", "detach", str(mnt)], dry=dry, check=False, capture=True)
        if not dry:
            shutil.rmtree(mnt, ignore_errors=True)


def stage_merge(source: str, dry: bool) -> None:
    log(f"stage: merge  {source} -> {MAIN_BRANCH} (fast forward only, no checkout)")
    mc = main_checkout()
    if mc is None:
        # main is checked out nowhere: move the branch ref, fast forward only.
        git(["fetch", ".", f"{source}:{MAIN_BRANCH}"], dry=dry)
    else:
        git(["-C", str(mc), "merge", "--ff-only", source], dry=dry)


def stage_push(source: str, new_version: str, skip_merge: bool, dry: bool) -> None:
    tag = f"v{new_version}"
    log(f"stage: push  {source}{'' if skip_merge or source == MAIN_BRANCH else ' and ' + MAIN_BRANCH}, then tag {tag}")
    branches = [source] if skip_merge or source == MAIN_BRANCH else [source, MAIN_BRANCH]
    git(["push", "--atomic", "origin", *branches], dry=dry)
    git(["tag", "-a", tag, "-m", f"Release {tag}"], dry=dry)
    git(["push", "origin", tag], dry=dry)


def stage_release(new_version: str, notes: str, prerelease: bool, dry: bool) -> str:
    tag = f"v{new_version}"
    log(f"stage: release  {tag}")
    # The notes go through a file (also in a dry run, .tmp/ is scratch) so the
    # printed command can be pasted as is when the agent may not publish.
    notes_file = NOTES_DIR / f"release-notes-{tag}.md"
    log(f"writing notes to {rel(notes_file)}", prefix="  ")
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

def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
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
    p.add_argument("--rustup-home", help="RUSTUP_HOME for the build (default: env)")
    p.add_argument("--cargo-home", help="CARGO_HOME for the build; its bin/ goes first on PATH (default: env)")

    args = p.parse_args(argv)

    if args.version and not SEMVER_RE.match(args.version):
        p.error(f"--version must be X.Y.Z, got {args.version!r}")
    if args.notes and args.notes_file:
        p.error("--notes and --notes-file are mutually exclusive")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.rustup_home:
        ENV["RUSTUP_HOME"] = args.rustup_home
    if args.cargo_home:
        ENV["CARGO_HOME"] = args.cargo_home
        ENV["PATH"] = f"{Path(args.cargo_home) / 'bin'}{os.pathsep}{ENV.get('PATH', '')}"

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

    preflight(new_version, source, skip_merge=skip_merge, dry=args.dry_run)
    stage_bump(current, new_version, dry=args.dry_run)
    try:
        stage_build(dry=args.dry_run)
        stage_verify(new_version, dry=args.dry_run)
    except SystemExit:
        log(
            f"build or verify failed; origin is untouched. Undo the local bump on {source} with: git reset --hard HEAD^",
            prefix="!!",
        )
        raise
    if skip_merge:
        log("stage: merge  SKIPPED (on main or --skip-merge)", prefix="  ")
    else:
        stage_merge(source, dry=args.dry_run)
    stage_push(source, new_version, skip_merge, dry=args.dry_run)
    url = stage_release(new_version, notes, args.prerelease, dry=args.dry_run)

    log(f"release complete: v{new_version}")
    if not url:
        log("(dry run, no release URL)", prefix="  ")
    return 0


if __name__ == "__main__":
    sys.exit(main())
