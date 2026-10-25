#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
CI/CD pipeline for the Garmin Export Tauri app (tauri-app/).

Runs under `uv run` (stdlib only, but uv provides a pinned interpreter).

Stages (run in order):
  1. clean     remove tauri-app/dist, src-tauri/binaries and the bundle/ folders
               under src-tauri/target/*/release/ (never the cargo cache)
  2. install   bun install in tauri-app/
  3. test      tools/test_report.py: test IDs, Go, ui/__tests__, Playwright, then
               bun run core (host Go binary for tauri-build) and cargo test; JUnit
               files and report.html/pdf in tauri-app/reportes-pruebas/<date>-ci/
  4. build     git worktree add .worktrees/build-<sha> HEAD, bun install and
               bun run release there (universal DMG), copy the DMG and the .app
               to tauri-app/dist/, remove the worktree. --here builds in this
               checkout instead (only when the app is closed).
  5. open      codesign --verify --deep --strict on tauri-app/dist/Garmin Export.app,
               then open -n

The universal build needs rustup with the aarch64-apple-darwin and
x86_64-apple-darwin targets. Homebrew's Rust has no x86_64 target, so pass an
isolated rustup with --rustup-home/--cargo-home (or RUSTUP_HOME/CARGO_HOME);
$CARGO_HOME/bin then goes first on PATH.

Usage:
  uv run tools/ci_pipeline.py                     # full pipeline
  uv run tools/ci_pipeline.py --skip-install      # skip bun install
  uv run tools/ci_pipeline.py --skip-tests        # skip tests
  uv run tools/ci_pipeline.py --skip-build        # skip build (test only run)
  uv run tools/ci_pipeline.py --no-open           # build but don't launch app
  uv run tools/ci_pipeline.py --stages test,build # explicit stage list
  uv run tools/ci_pipeline.py --here              # build in this checkout
  uv run tools/ci_pipeline.py --dry-run           # print every command, run nothing

Exit codes:
  0 - success
  1 - stage failure (stderr contains details)
  2 - invalid arguments / bad environment
"""
from __future__ import annotations

import argparse
import json
import os
import shlex
import subprocess
import sys
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
APP_DIR = REPO_ROOT / "tauri-app"
DIST_DIR = APP_DIR / "dist"
APP_NAME = "Garmin Export.app"
BUNDLE = Path("src-tauri/target/universal-apple-darwin/release/bundle")
RUST_TARGETS = ("aarch64-apple-darwin", "x86_64-apple-darwin")

ALL_STAGES = ["clean", "install", "test", "build", "open"]

DRY = False
ENV = os.environ.copy()


def log(msg: str, prefix: str = "==>") -> None:
    print(f"{prefix} {msg}", flush=True)


def rel(p: Path) -> str:
    try:
        return p.relative_to(REPO_ROOT).as_posix() or "."
    except ValueError:
        return str(p)


def run(cmd: list[str], cwd: Path = REPO_ROOT) -> int:
    log(f"$ {shlex.join(cmd)}  (cwd={rel(cwd)})", prefix="  ")
    if DRY:
        return 0
    return subprocess.run(cmd, cwd=cwd, env=ENV).returncode


def capture(cmd: list[str], cwd: Path = REPO_ROOT) -> subprocess.CompletedProcess:
    """Read only commands; they run even in --dry-run."""
    return subprocess.run(cmd, cwd=cwd, env=ENV, capture_output=True, text=True)


def stage_clean() -> int:
    log("stage: clean")
    targets = [DIST_DIR, APP_DIR / "src-tauri" / "binaries"]
    targets += sorted((APP_DIR / "src-tauri" / "target").glob("*/release/bundle"))
    return run(["rm", "-rf", *(rel(p) for p in targets)])


def stage_install() -> int:
    log("stage: install")
    return run(["bun", "install"], cwd=APP_DIR)


def stage_test() -> int:
    log("stage: test")
    # tools/test_report.py runs test IDs, Go, ui, Playwright and cargo (after bun run core,
    # which tauri-build needs and clean removed), each to the end, then writes the report to
    # tauri-app/reportes-pruebas/<date>-ci/. GitHub Actions runs the same script.
    return run([sys.executable, str(REPO_ROOT / "tools" / "test_report.py")], REPO_ROOT)


def check_rust() -> int:
    log(f"rust: RUSTUP_HOME={ENV.get('RUSTUP_HOME', '(unset)')} CARGO_HOME={ENV.get('CARGO_HOME', '(unset)')}", prefix="  ")
    cmd = ["rustup", "target", "list", "--installed"]
    log(f"$ {shlex.join(cmd)}", prefix="  ")
    try:
        installed = capture(cmd).stdout.split()
    except FileNotFoundError:
        installed = []
    missing = [t for t in RUST_TARGETS if t not in installed]
    if not missing:
        return 0
    log(
        f"rustup is missing target(s) {missing}. The universal build needs {list(RUST_TARGETS)}; "
        "pass --rustup-home/--cargo-home (or set RUSTUP_HOME/CARGO_HOME) for a rustup that has them.",
        prefix="  !!",
    )
    return 0 if DRY else 1


def build_in(root: Path) -> int:
    app = root / "tauri-app"
    for cmd in (["bun", "install"], ["bun", "run", "release"]):
        rc = run(cmd, cwd=app)
        if rc != 0:
            return rc
    # In --dry-run the worktree does not exist; this checkout has the same version.
    pkg = app / "package.json" if (app / "package.json").exists() else APP_DIR / "package.json"
    version = json.loads(pkg.read_text())["version"]
    dmg = app / BUNDLE / "dmg" / f"Garmin Export_{version}_universal.dmg"
    bundle_app = app / BUNDLE / "macos" / APP_NAME
    for cmd in (
        ["mkdir", "-p", rel(DIST_DIR)],
        ["rm", "-rf", rel(DIST_DIR / APP_NAME)],
        ["cp", rel(dmg), rel(DIST_DIR) + "/"],
        # ditto keeps the bundle's symlinks and signature intact.
        ["ditto", rel(bundle_app), rel(DIST_DIR / APP_NAME)],
    ):
        rc = run(cmd)
        if rc != 0:
            return rc
    return 0


def stage_build(here: bool) -> int:
    log("stage: build" + ("  (--here: in this checkout)" if here else ""))
    rc = check_rust()
    if rc != 0:
        return rc
    if here:
        return build_in(REPO_ROOT)

    if capture(["git", "status", "--porcelain"]).stdout.strip():
        log("working tree has uncommitted changes; the worktree builds HEAD without them", prefix="  !!")
    sha = capture(["git", "rev-parse", "--short", "HEAD"]).stdout.strip()
    wt = REPO_ROOT / ".worktrees" / f"build-{sha}"
    if wt.exists():
        log(f"{rel(wt)} is left from an earlier run, removing it", prefix="  ")
        run(["git", "worktree", "remove", "--force", rel(wt)])
    rc = run(["git", "worktree", "add", "--detach", rel(wt), "HEAD"])
    if rc != 0:
        return rc
    try:
        return build_in(wt)
    finally:
        run(["git", "worktree", "remove", "--force", rel(wt)])


def stage_open() -> int:
    log("stage: open")
    app = DIST_DIR / APP_NAME
    if not DRY and not app.is_dir():
        log(f"{rel(app)} not found; did build run?", prefix="  !!")
        return 1
    rc = run(["codesign", "--verify", "--deep", "--strict", rel(app)])
    if rc != 0:
        return rc
    return run(["open", "-n", rel(app)])


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--skip-install", action="store_true")
    p.add_argument("--skip-tests", action="store_true")
    p.add_argument("--skip-build", action="store_true")
    p.add_argument("--skip-clean", action="store_true")
    p.add_argument("--no-open", action="store_true")
    p.add_argument("--here", action="store_true", help="build in this checkout, not a worktree (only when the app is closed)")
    p.add_argument("--dry-run", action="store_true", help="print every command without executing")
    p.add_argument("--rustup-home", default=os.environ.get("RUSTUP_HOME"), help="RUSTUP_HOME for the build (default: env)")
    p.add_argument("--cargo-home", default=os.environ.get("CARGO_HOME"), help="CARGO_HOME for the build; its bin/ goes first on PATH (default: env)")
    p.add_argument(
        "--stages",
        help=f"comma-separated subset of {ALL_STAGES} (overrides --skip-* flags)",
    )
    return p.parse_args()


def resolve_stages(args: argparse.Namespace) -> list[str]:
    if args.stages:
        requested = [s.strip() for s in args.stages.split(",") if s.strip()]
        unknown = [s for s in requested if s not in ALL_STAGES]
        if unknown:
            log(f"unknown stage(s): {unknown}. valid: {ALL_STAGES}", prefix="!!")
            sys.exit(2)
        return requested

    stages = list(ALL_STAGES)
    if args.skip_clean:
        stages.remove("clean")
    if args.skip_install:
        stages.remove("install")
    if args.skip_tests:
        stages.remove("test")
    if args.skip_build:
        stages.remove("build")
    if args.no_open:
        stages.remove("open")
    return stages


def main() -> int:
    global DRY
    if not APP_DIR.is_dir():
        log(f"tauri-app/ not found at {APP_DIR}", prefix="!!")
        return 2

    args = parse_args()
    DRY = args.dry_run
    if args.rustup_home:
        ENV["RUSTUP_HOME"] = args.rustup_home
    if args.cargo_home:
        ENV["CARGO_HOME"] = args.cargo_home
        ENV["PATH"] = f"{Path(args.cargo_home) / 'bin'}{os.pathsep}{ENV.get('PATH', '')}"

    stages = resolve_stages(args)
    log(f"pipeline stages: {stages}" + ("  (dry run)" if DRY else ""))

    fns = {
        "clean": stage_clean,
        "install": stage_install,
        "test": stage_test,
        "build": lambda: stage_build(args.here),
        "open": stage_open,
    }
    started = time.time()
    for name in stages:
        t0 = time.time()
        rc = fns[name]()
        dt = time.time() - t0
        if rc != 0:
            log(f"stage '{name}' failed after {dt:.1f}s (exit {rc})", prefix="!!")
            return 1
        log(f"stage '{name}' ok ({dt:.1f}s)")

    total = time.time() - started
    log(f"pipeline complete in {total:.1f}s")
    return 0


if __name__ == "__main__":
    sys.exit(main())
