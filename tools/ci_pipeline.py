#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
CI/CD pipeline for the Garmin Data Exporter Electron app.

Runs under `uv run` (stdlib-only, but uv provides a pinned interpreter).

Stages (run in order):
  1. clean     - remove electron-app/dist/ (old DMGs and unpacked app bundles)
  2. install   - npm install (skippable if node_modules is fresh)
  3. test      - run Node test suites in electron-app/**/__tests__/
  4. build     - npm run dist (produces signed DMGs in electron-app/dist/)
  5. open      - open the freshly-built .app in Finder / launch it

Usage:
  uv run tools/ci_pipeline.py                     # full pipeline
  uv run tools/ci_pipeline.py --skip-install      # skip npm install
  uv run tools/ci_pipeline.py --skip-tests        # skip tests
  uv run tools/ci_pipeline.py --skip-build        # skip build (test-only run)
  uv run tools/ci_pipeline.py --no-open           # build but don't launch app
  uv run tools/ci_pipeline.py --stages test,build # explicit stage list

Exit codes:
  0 - success
  1 - stage failure (stderr contains details)
  2 - invalid arguments / bad environment
"""
from __future__ import annotations

import argparse
import os
import platform
import shutil
import subprocess
import sys
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
APP_DIR = REPO_ROOT / "electron-app"
DIST_DIR = APP_DIR / "dist"
NODE_MODULES = APP_DIR / "node_modules"
TEST_GLOBS = ["garmin/__tests__", "util/__tests__"]

ALL_STAGES = ["clean", "install", "test", "build", "open"]


def log(msg: str, prefix: str = "==>") -> None:
    print(f"{prefix} {msg}", flush=True)


def run(cmd: list[str], cwd: Path, env: dict | None = None) -> int:
    log(f"$ {' '.join(cmd)}  (cwd={cwd.relative_to(REPO_ROOT)})", prefix="  ")
    proc = subprocess.run(cmd, cwd=cwd, env=env)
    return proc.returncode


def stage_clean() -> int:
    log("stage: clean")
    if DIST_DIR.exists():
        log(f"removing {DIST_DIR.relative_to(REPO_ROOT)}", prefix="  ")
        shutil.rmtree(DIST_DIR)
    else:
        log("dist/ already clean", prefix="  ")
    return 0


def stage_install() -> int:
    log("stage: install")
    if not NODE_MODULES.exists():
        log("node_modules missing, running npm install", prefix="  ")
    else:
        log("node_modules present, refreshing lockfile state", prefix="  ")
    return run(["npm", "install"], cwd=APP_DIR)


def stage_test() -> int:
    log("stage: test")
    test_files: list[Path] = []
    for rel in TEST_GLOBS:
        d = APP_DIR / rel
        if not d.is_dir():
            continue
        test_files.extend(sorted(d.glob("*.test.js")))

    if not test_files:
        log("no test files found — skipping", prefix="  ")
        return 0

    log(f"discovered {len(test_files)} test file(s)", prefix="  ")
    failed = 0
    for tf in test_files:
        rel = tf.relative_to(APP_DIR)
        log(f"running {rel}", prefix="  ")
        rc = run(["node", "--test", str(rel)], cwd=APP_DIR)
        if rc != 0:
            failed += 1
            log(f"FAIL: {rel} (exit {rc})", prefix="  !!")
    if failed:
        log(f"{failed} test file(s) failed", prefix="!!")
        return 1
    return 0


def stage_build() -> int:
    log("stage: build")
    return run(["npm", "run", "dist"], cwd=APP_DIR)


def detect_mac_app_dir() -> str:
    """Map host arch to electron-builder's output dir name."""
    machine = platform.machine().lower()
    if machine in ("arm64", "aarch64"):
        return "mac-arm64"
    if machine in ("x86_64", "amd64"):
        return "mac"
    return "mac"  # unknown -> default x64 bundle


def stage_open() -> int:
    log("stage: open")
    if sys.platform != "darwin":
        log("open is macOS-only, skipping", prefix="  ")
        return 0

    preferred = detect_mac_app_dir()
    search_order = [preferred] + [d for d in ("mac-arm64", "mac") if d != preferred]

    app: Path | None = None
    for sub in search_order:
        hits = list((DIST_DIR / sub).glob("*.app"))
        if hits:
            app = hits[0]
            if sub != preferred:
                log(f"preferred {preferred}/ empty, falling back to {sub}/", prefix="  ")
            break

    if app is None:
        log("no .app bundle found in dist/ — did build run?", prefix="  !!")
        return 1

    log(f"host arch={platform.machine()}, preparing {app.relative_to(REPO_ROOT)}", prefix="  ")

    # electron-builder skips signing without a Developer ID, leaving only a
    # linker-injected ad-hoc signature with empty CodeResources. Launch Services
    # silently refuses to exec such a bundle even though `open` returns 0. The
    # fix is to ad-hoc re-sign so CodeResources is rewritten and the signature
    # is internally consistent. Also clear all xattrs (quarantine + provenance).
    for step in (
        ["xattr", "-cr", str(app)],
        ["codesign", "--force", "--deep", "--sign", "-", str(app)],
        ["codesign", "--verify", "--verbose", str(app)],
    ):
        log(f"$ {' '.join(step)}", prefix="  ")
        subprocess.run(step, check=False)

    log(f"opening {app.relative_to(REPO_ROOT)}", prefix="  ")
    return run(["open", "-n", "-a", str(app)], cwd=REPO_ROOT)


STAGE_FNS = {
    "clean": stage_clean,
    "install": stage_install,
    "test": stage_test,
    "build": stage_build,
    "open": stage_open,
}


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--skip-install", action="store_true")
    p.add_argument("--skip-tests", action="store_true")
    p.add_argument("--skip-build", action="store_true")
    p.add_argument("--skip-clean", action="store_true")
    p.add_argument("--no-open", action="store_true")
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
    if not APP_DIR.is_dir():
        log(f"electron-app/ not found at {APP_DIR}", prefix="!!")
        return 2

    args = parse_args()
    stages = resolve_stages(args)
    log(f"pipeline stages: {stages}")

    started = time.time()
    for name in stages:
        t0 = time.time()
        rc = STAGE_FNS[name]()
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
