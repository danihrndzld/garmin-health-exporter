#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Runs every test suite of tauri-app/ with machine readable results and builds one report.

Suites, in order (one task per signal; a failing suite never stops the next one):
  ids     bun scripts/test-ids.js                      (gate: unique technique IDs)
  go      go vet, then go test -json -coverprofile     -> go-junit.xml, go-coverage.json
  ui      bun test ui/__tests__ --reporter=junit       -> ui-junit.xml
  e2e     bunx playwright test (Chromium + WebKit)      -> e2e-junit.xml, e2e.json (flaky count)
  cargo   bun run core, then cargo test (text log only; cargo has no stable JUnit output)

Output: tauri-app/reportes-pruebas/<date>-ci/ (gitignored): one .log per suite, the JUnit
files, report.json, and report.html, report.pdf and summary.html from the art-of-cicd skill's
build_report.py (ART_OF_CICD=<skill dir>, default ~/.claude/skills/art-of-cicd; without it the
JUnit files are still written). PDF needs Chrome (CHROME=<binary> to choose one).

  uv run tools/test_report.py                       # all suites
  uv run tools/test_report.py --suites ids,go,ui,e2e --formats html,summary   # CI on Linux

Exit 1 when any suite failed, after the report is written. Exit 0 otherwise.
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import pathlib
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent
APP = ROOT / "tauri-app"
CORE = APP / "core"
MODULE = "github.com/danihrndzld/garmin-health-exporter/tauri-app/core/"
ALL = ["ids", "go", "ui", "e2e", "cargo"]


def sh(cmd: list[str], cwd: pathlib.Path, log: pathlib.Path, env: dict | None = None, stdout: pathlib.Path | None = None) -> int:
    """Runs cmd, appends its output to log (stdout to its own file when given), returns the exit code."""
    with open(log, "a") as lf:
        lf.write(f"$ {' '.join(cmd)}  (in {cwd.relative_to(ROOT)})\n")
        lf.flush()
        out = open(stdout, "w") if stdout else lf
        try:
            return subprocess.run(cmd, cwd=cwd, stdout=out, stderr=lf, env={**os.environ, **(env or {})}).returncode
        finally:
            if stdout:
                out.close()


def run_suite(name: str, d: pathlib.Path) -> dict:
    log = d / f"{name}.log"
    log.write_text("")
    t0 = time.monotonic()
    junit, rc, note, flaky = None, 0, "", 0
    if name == "ids":
        rc = sh(["bun", "scripts/test-ids.js"], APP, log)
    elif name == "go":
        rc = sh(["go", "vet", "./..."], CORE, log)
        test_json = d / "go-test.json"
        rc2 = sh(["go", "test", "-count=1", "-json", f"-coverprofile={d / 'go-cover.out'}", "./..."], CORE, log, stdout=test_json)
        rc = rc or rc2
        conv = ROOT / "tools" / "go_junit.py"
        sh([sys.executable, "-I", str(conv), "junit", str(test_json), str(d / "go-junit.xml")], ROOT, log)
        if (d / "go-cover.out").exists():
            sh([sys.executable, "-I", str(conv), "cober", str(d / "go-cover.out"), str(d / "go-coverage.json"), MODULE], ROOT, log)
        junit = d / "go-junit.xml"
    elif name == "ui":
        junit = d / "ui-junit.xml"
        rc = sh(["bun", "test", "ui/__tests__", "--reporter=junit", f"--reporter-outfile={junit}"], APP, log)
    elif name == "e2e":
        junit = d / "e2e-junit.xml"
        rc = sh(["bunx", "playwright", "test"], APP, log, env={"E2E_REPORT_DIR": str(d)})
        try:
            stats = json.loads((d / "e2e.json").read_text())["stats"]
            flaky = stats.get("flaky", 0)
            note = f"{stats.get('expected', 0)} passed, {stats.get('flaky', 0)} flaky (passed on a retry), {stats.get('unexpected', 0)} failed"
        except (OSError, ValueError, KeyError):
            note = "no e2e.json"
    elif name == "cargo":
        rc = sh(["bun", "run", "core"], APP, log)  # tauri-build needs binaries/garmin-export-core-<host>
        rc = rc or sh(["cargo", "test"], APP / "src-tauri", log)
    secs = time.monotonic() - t0
    return {"name": name, "rc": rc, "secs": secs, "junit": junit if junit and junit.exists() else None, "note": note, "flaky": flaky}


def git(*args: str) -> str:
    return subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True).stdout.strip()


def report_json(results: list[dict], d: pathlib.Path) -> dict:
    failed = [r["name"] for r in results if r["rc"]]
    rows = [[r["name"], "pass" if not r["rc"] else f"fail (exit {r['rc']})", f"{r['secs']:.0f}", f"{r['name']}.log",
             r["note"] or (r["junit"].name if r["junit"] else "log only")] for r in results]
    flaky = next((r["note"] for r in results if r["flaky"]), "")
    total = sum(r["secs"] for r in results)
    return {
        "title": "Garmin Export test run",
        "skill": "art-of-cicd",
        "meta": {"date": dt.date.today().isoformat(), "branch": git("rev-parse", "--abbrev-ref", "HEAD"),
                 "commit": git("rev-parse", "--short", "HEAD"), "command": "uv run tools/test_report.py " + " ".join(sys.argv[1:]),
                 "tools": "go test, bun test, Playwright (Chromium, WebKit), cargo test"},
        "summary": f"<p>{len(results)} suites ran in {total / 60:.1f} minutes; {len(failed)} failed{': ' + ', '.join(failed) if failed else ''}. "
                   "Go coverage covers tauri-app/core statements. Cargo writes a log only.</p>",
        "verdict": {"status": "fail" if failed else "pass",
                    "text": "Every suite passed." if not failed else "Suites failed: " + ", ".join(failed) + ". See the Failures section and the logs."},
        "sections": [{
            "heading": "Suites",
            "text": "<p>Order: fastest gate first. Budget: unit under 1 minute, the full run under 30.</p>",
            "table": {"columns": ["Suite", "Result", "Seconds", "Log", "Results"], "rows": rows, "numeric": [2]},
            **({"callout": {"tone": "warn", "text": "Playwright: " + flaky + ". A test that passed only on a retry is a flake to investigate, not a pass."}} if flaky else {}),
        }],
    }


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--suites", default=",".join(ALL), help=f"comma separated subset of {','.join(ALL)}")
    ap.add_argument("--formats", default="html,pdf,summary", help="build_report.py formats")
    ap.add_argument("--out", type=pathlib.Path, help="output dir (default tauri-app/reportes-pruebas/<date>-ci)")
    a = ap.parse_args()
    suites = [s for s in a.suites.split(",") if s]
    if bad := [s for s in suites if s not in ALL]:
        sys.exit(f"unknown suites: {bad}")
    d = (a.out or APP / "reportes-pruebas" / f"{dt.date.today()}-ci").resolve()
    d.mkdir(parents=True, exist_ok=True)

    results = []
    for s in suites:
        print(f"[test_report] {s} ...", flush=True)
        results.append(r := run_suite(s, d))
        print(f"[test_report] {s}: {'pass' if not r['rc'] else 'FAIL'} in {r['secs']:.0f}s {r['note']}", flush=True)

    (d / "report.json").write_text(json.dumps(report_json(results, d), indent=2))
    skill = pathlib.Path(os.environ.get("ART_OF_CICD", pathlib.Path.home() / ".claude/skills/art-of-cicd"))
    script = skill / "scripts" / "build_report.py"
    if script.exists():
        cmd = [sys.executable, str(script), str(d / "report.json"), "--root", str(ROOT), "--formats", a.formats, "--ci"]
        for r in results:
            if r["junit"]:
                cmd += ["--junit", str(r["junit"])]
        if (d / "go-coverage.json").exists():
            cmd += ["--coverage", str(d / "go-coverage.json")]
        rc = subprocess.run(cmd, cwd=ROOT).returncode
        if rc:
            print(f"[test_report] build_report.py exited {rc} (2 means no Chrome for the PDF; the HTML is written)", flush=True)
    else:
        print(f"[test_report] {script} not found; set ART_OF_CICD. JUnit files are in {d}", flush=True)
    print(f"[test_report] report: {d}", flush=True)
    return 1 if any(r["rc"] for r in results) else 0


if __name__ == "__main__":
    sys.exit(main())
