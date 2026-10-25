#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Probe Garmin's Personal API with the key in .env (API_KEY="...").

Trades the key for a 24 hour access token (one POST), then sends one read only GET per
path the app uses, on personal.connectapi.garmin.com, with a 0.5 s pause. Prints a table of
status codes and writes .tmp/personal_api_probe.json. It never prints the key or the token,
and it never signs in with a password.

Run it when Garmin changes the Personal API, to see which of the app's endpoints answer:
  uv run tools/probe_personal_api.py               # end date = yesterday
  uv run tools/probe_personal_api.py --end 2026-10-20 --days 31

Exit 0 when every path the app depends on in API key mode answers 200, 1 otherwise.
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import json
import pathlib
import sys
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
BASE = "https://personal.connectapi.garmin.com"
TOKEN_PATH = "/personalapitoken-service/personal-api/personal-api-key/token"

# name -> (path template, the app needs it in API key mode). {d} is the end date, {s} the start.
PATHS = {
    # used by the app with the key (gconnect PersonalRanges and the shared paths)
    "personal_daily_summaries": ("/usersummary-service/v2/dailysummaries?startDate={s}&endDate={d}", True),
    "personal_sleep": ("/sleep-service/sleep/dailySleepSummariesByDate?startDate={s31}&endDate={d}", True),
    "personal_hrv": ("/hrv-service/hrv/daily/{s}/{d}", True),
    "personal_body_battery": ("/wellness-service/wellness/bodyBattery/reports/daily?startDate={s31}&endDate={d}", True),
    "personal_fitness_age": ("/fitnessage-service/fitnessage/v2/{d}", True),
    "daily_steps": ("/usersummary-service/stats/steps/daily/{s28}/{d}", True),
    "agg_weigh_ins": ("/weight-service/weight/range/{s}/{d}?includeAll=true", True),
    "activities_by_date": ("/activitylist-service/activities/search/activities?startDate={s}&endDate={d}&start=0&limit=20", True),
    # connectapi paths the app asks with the email login; 404 here means "login only"
    "training_readiness": ("/metrics-service/metrics/trainingreadiness/{d}", False),
    "training_status": ("/metrics-service/metrics/trainingstatus/aggregated/{d}", False),
    "endurance_score": ("/metrics-service/metrics/endurancescore?calendarDate={d}", False),
    "max_metrics": ("/metrics-service/metrics/maxmet/daily/{d}/{d}", False),
    "respiration": ("/wellness-service/wellness/daily/respiration/{d}", False),
    "stress": ("/wellness-service/wellness/dailyStress/{d}", False),
    "body_battery_events": ("/wellness-service/wellness/bodyBattery/events/{d}", False),
    "blood_pressure": ("/bloodpressure-service/bloodpressure/range/{s}/{d}?includeAll=true", False),
    "race_predictions": ("/metrics-service/metrics/racepredictions/latest/x", False),
    "goals": ("/goal-service/goal/goals?status=active&start=1&limit=30&sortOrder=asc", False),
    "social_profile": ("/userprofile-service/socialProfile", False),
}


def read_key() -> str:
    env = ROOT / ".env"
    for line in env.read_text().splitlines() if env.exists() else []:
        k, _, v = line.partition("=")
        if k.strip() == "API_KEY":
            return v.strip().strip('"').strip("'")
    sys.exit("API_KEY is missing from .env. Create a key at https://connect.garmin.com/app/settings/personalApiToken")


def request(method: str, url: str, headers: dict[str, str]) -> tuple[int, bytes]:
    req = urllib.request.Request(url, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def token(key: str) -> str:
    # Accept */*: the token comes back as text, and Accept: application/json gets 406.
    status, body = request("POST", BASE + TOKEN_PATH, {"Authorization": "ApiKey " + key, "Accept": "*/*"})
    if status != 200:
        sys.exit(f"The token request answered HTTP {status}. A 401 means Garmin refused the key (wrong, expired or deleted).")
    tok = body.decode().strip().strip('"')
    payload = tok.split(".")[1]
    exp = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4))).get("exp")
    print(f"token: OK, expires {dt.datetime.fromtimestamp(exp):%Y-%m-%d %H:%M}" if exp else "token: OK")
    return tok


def shape(body: bytes) -> str:
    try:
        v = json.loads(body)
    except ValueError:
        return f"{len(body)} bytes, not JSON"
    if isinstance(v, list):
        return f"list of {len(v)}"
    if isinstance(v, dict) and isinstance(v.get("data"), list):
        return f"data: list of {len(v['data'])}"
    return "keys: " + ", ".join(list(v)[:5]) if isinstance(v, dict) else type(v).__name__


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--end", default=(dt.date.today() - dt.timedelta(days=1)).isoformat(), help="end date, YYYY-MM-DD")
    ap.add_argument("--days", type=int, default=90, help="range for the 90 day endpoints (default 90)")
    a = ap.parse_args()
    end = dt.date.fromisoformat(a.end)
    span = lambda n: (end - dt.timedelta(days=min(n, a.days) - 1)).isoformat()  # noqa: E731
    fill = {"d": a.end, "s": span(90), "s31": span(31), "s28": span(28)}

    tok = token(read_key())
    headers = {"Authorization": "Bearer " + tok, "Accept": "application/json"}
    rows, failed = [], []
    for name, (tmpl, needed) in PATHS.items():
        status, body = request("GET", BASE + tmpl.format(**fill), headers)
        rows.append({"name": name, "status": status, "needed": needed, "shape": shape(body) if status == 200 else ""})
        if needed and status != 200:
            failed.append(name)
        print(f"{name:26} {status}  {'needed ' if needed else 'login  '} {rows[-1]['shape']}")
        time.sleep(0.5)

    out = ROOT / ".tmp" / "personal_api_probe.json"
    out.parent.mkdir(exist_ok=True)
    out.write_text(json.dumps({"probed_at": dt.datetime.now().isoformat(timespec="seconds"), "end": a.end, "rows": rows}, indent=2))
    print(f"\nwrote {out.relative_to(ROOT)}")
    if failed:
        print("API key mode is missing: " + ", ".join(failed))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
