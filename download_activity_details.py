#!/usr/bin/env python3
#
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "garminconnect>=0.2.40",
#     "python-dotenv>=1.0.0",
# ]
# ///
"""
Download full activity details for all activities and export one CSV per type.

Activity type grouping:
  caminar  → walking, hiking
  correr   → running, treadmill_running
  gym      → strength_training, indoor_cardio

Usage: uv run download_activity_details.py [path/to/garmin_health_*.json]
       (defaults to latest file in output/)
"""

import csv
import json
import sys
import time
from pathlib import Path

from dotenv import load_dotenv
from garminconnect import Garmin
import os

load_dotenv(Path(__file__).parent / ".env")

OUTPUT_DIR = Path(__file__).parent / "output"

TYPE_GROUPS = {
    "caminar": {"walking", "hiking"},
    "correr":  {"running", "treadmill_running"},
    "gym":     {"strength_training", "indoor_cardio"},
}

# Fields from the activity summary to always include
SUMMARY_FIELDS = [
    "activityId", "activityName", "startTimeLocal", "distance", "duration",
    "movingDuration", "calories", "averageHR", "maxHR", "steps",
    "aerobicTrainingEffect", "anaerobicTrainingEffect", "activityTrainingLoad",
    "trainingEffectLabel", "differenceBodyBattery",
    "hrTimeInZone_1", "hrTimeInZone_2", "hrTimeInZone_3", "hrTimeInZone_4", "hrTimeInZone_5",
    "moderateIntensityMinutes", "vigorousIntensityMinutes",
    "avgElevation", "minTemperature", "maxTemperature",
    "totalReps", "totalSets", "activeSets",
    "lapCount", "averageSpeed",
]


def latest_json() -> Path:
    files = sorted(OUTPUT_DIR.glob("garmin_health_*.json"))
    if not files:
        sys.exit("No garmin_health_*.json found in output/")
    return files[-1]


def group_for(type_key: str) -> str | None:
    for group, keys in TYPE_GROUPS.items():
        if type_key in keys:
            return group
    return None


def flatten_details(details: dict) -> dict:
    """Extract averaged scalar metrics from get_activity_details using metricDescriptors."""
    row = {}

    descriptors = details.get("metricDescriptors") or []
    # Build index → key map
    idx_to_key = {d["metricsIndex"]: d["key"] for d in descriptors}

    metric_entries = details.get("activityDetailMetrics") or []
    if metric_entries and idx_to_key:
        sums: dict[str, list] = {}
        for entry in metric_entries:
            values = entry.get("metrics") or []
            for idx, val in enumerate(values):
                key = idx_to_key.get(idx)
                if key and val is not None:
                    sums.setdefault(key, []).append(val)
        # Skip GPS/timestamp columns — not useful for LLM
        skip = {"directLatitude", "directLongitude", "directTimestamp"}
        for key, vals in sums.items():
            if key in skip:
                continue
            clean = [v for v in vals if v is not None]
            if not clean:
                continue
            row[f"avg_{key}"] = round(sum(clean) / len(clean), 4)
            row[f"max_{key}"] = max(clean)
            row[f"min_{key}"] = min(clean)

    return row


def flatten_laps(splits: dict) -> list[dict]:
    """Turn lapDTOs into a flat list of dicts."""
    rows = []
    for lap in splits.get("lapDTOs") or []:
        row = {}
        for key in ["lapIndex", "startTimeGMT", "distance", "duration", "movingDuration",
                    "averageSpeed", "averageHR", "maxHR", "calories",
                    "averageRunCadence", "maxRunCadence",
                    "averageTemperature", "maxTemperature", "minTemperature",
                    "totalAscent", "totalDescent"]:
            row[f"lap_{key}"] = lap.get(key)
        rows.append(row)
    return rows


def main():
    json_path = Path(sys.argv[1]) if len(sys.argv) > 1 else latest_json()
    print(f"Reading {json_path.name}...")
    data = json.loads(json_path.read_text())
    activities = data.get("activities", [])

    email    = os.getenv("GARMIN_EMAIL", "")
    password = os.getenv("GARMIN_PASSWORD", "")
    if not email or not password:
        sys.exit("Set GARMIN_EMAIL and GARMIN_PASSWORD in .env")

    print(f"Logging in as {email}...")
    client = Garmin(email, password)
    client.login()
    print("Login successful.\n")

    # Group activities
    grouped: dict[str, list] = {g: [] for g in TYPE_GROUPS}

    date_tag = data.get("export_date", "export")
    csv_dir  = OUTPUT_DIR / f"csv_{date_tag}"
    csv_dir.mkdir(exist_ok=True)

    # Fetch details per activity
    total = len(activities)
    for i, act in enumerate(activities, 1):
        type_key = (act.get("activityType") or {}).get("typeKey", "")
        group    = group_for(type_key)
        if group is None:
            print(f"  [{i}/{total}] skip unknown type: {type_key}")
            continue

        act_id = act.get("activityId")
        print(f"  [{i}/{total}] {group:8s} | {act.get('startTimeLocal','')[:10]} | {act.get('activityName','')}")

        # Build base row from summary
        row = {f: act.get(f) for f in SUMMARY_FIELDS}
        row["activityType"] = type_key

        # Fetch detailed metrics
        try:
            details = client.get_activity_details(act_id, maxchart=2000)
            row.update(flatten_details(details))
        except Exception as e:
            print(f"    [warn] details: {e}")

        # Flatten laps from already-downloaded splits
        laps = flatten_laps(act.get("splits") or {})
        # Embed lap summary as JSON string (keeps one row per activity)
        row["laps_json"] = json.dumps(laps)
        row["lap_count_actual"] = len(laps)

        grouped[group].append(row)
        time.sleep(0.3)  # be polite to the API

    # Write CSVs
    print()
    for group, rows in grouped.items():
        if not rows:
            continue
        path = csv_dir / f"activities_{group}.csv"
        all_keys = list(dict.fromkeys(k for r in rows for k in r))
        with open(path, "w", newline="") as f:
            w = csv.DictWriter(f, fieldnames=all_keys, extrasaction="ignore")
            w.writeheader()
            w.writerows(rows)
        print(f"  {path.name}  ({len(rows)} activities)")

    print("\nDone.")


if __name__ == "__main__":
    main()
