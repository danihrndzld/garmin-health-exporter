#!/usr/bin/env python3
#
# /// script
# requires-python = ">=3.9"
# dependencies = [
#     "garminconnect>=0.2.40",
#     "python-dotenv>=1.0.0",
# ]
# ///
"""
Download full activity details and export one CSV per activity type.

Type grouping:
  caminar  → walking, hiking
  correr   → running, treadmill_running
  gym      → strength_training, indoor_cardio

Usage: uv run download_activity_details.py --email E --password P --json FILE --output DIR
"""

import argparse
import csv
import json
import sys
import time
from pathlib import Path


TYPE_GROUPS = {
    "caminar": {"walking", "hiking"},
    "correr":  {"running", "treadmill_running"},
    "gym":     {"strength_training", "indoor_cardio"},
}

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


def group_for(type_key):
    for group, keys in TYPE_GROUPS.items():
        if type_key in keys:
            return group
    return None


def flatten_details(details):
    row = {}
    descriptors = details.get("metricDescriptors") or []
    idx_to_key = {d["metricsIndex"]: d["key"] for d in descriptors}
    metric_entries = details.get("activityDetailMetrics") or []
    if metric_entries and idx_to_key:
        sums = {}
        for entry in metric_entries:
            values = entry.get("metrics") or []
            for idx, val in enumerate(values):
                key = idx_to_key.get(idx)
                if key and val is not None:
                    sums.setdefault(key, []).append(val)
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


def flatten_laps(splits):
    rows = []
    for lap in (splits.get("lapDTOs") or []):
        row = {}
        for key in ["lapIndex", "startTimeGMT", "distance", "duration", "movingDuration",
                    "averageSpeed", "averageHR", "maxHR", "calories",
                    "averageRunCadence", "maxRunCadence",
                    "averageTemperature", "maxTemperature", "minTemperature",
                    "totalAscent", "totalDescent"]:
            row[f"lap_{key}"] = lap.get(key)
        rows.append(row)
    return rows


def main(email, password, json_path, output_dir):
    from garminconnect import Garmin

    print(f"Reading {json_path.name}...", flush=True)
    data = json.loads(json_path.read_text())
    activities = data.get("activities", [])

    print(f"Connecting as {email}…", flush=True)
    client = Garmin(email, password)
    client.login()
    print("Login successful.", flush=True)

    date_tag = data.get("export_date", "export")
    csv_dir  = output_dir / f"csv_{date_tag}"
    csv_dir.mkdir(parents=True, exist_ok=True)

    grouped = {g: [] for g in TYPE_GROUPS}
    total = len(activities)

    for i, act in enumerate(activities, 1):
        type_key = (act.get("activityType") or {}).get("typeKey", "")
        group    = group_for(type_key)
        if group is None:
            print(f"  [{i}/{total}] skip: {type_key}", flush=True)
            print(f"PROGRESS:{i}:{total}", flush=True)
            continue

        act_id = act.get("activityId")
        print(f"  [{i}/{total}] {group:8s} | {(act.get('startTimeLocal') or '')[:10]} | {act.get('activityName', '')}", flush=True)

        row = {f: act.get(f) for f in SUMMARY_FIELDS}
        row["activityType"] = type_key

        try:
            details = client.get_activity_details(act_id, maxchart=2000)
            row.update(flatten_details(details))
        except Exception as e:
            print(f"    [warn] details: {e}", flush=True)

        laps = flatten_laps(act.get("splits") or {})
        row["laps_json"] = json.dumps(laps)
        row["lap_count_actual"] = len(laps)

        grouped[group].append(row)
        print(f"PROGRESS:{i}:{total}", flush=True)
        time.sleep(0.3)

    for group, rows in grouped.items():
        if not rows:
            continue
        path = csv_dir / f"activities_{group}.csv"
        all_keys = list(dict.fromkeys(k for r in rows for k in r))
        with open(path, "w", newline="") as f:
            w = csv.DictWriter(f, fieldnames=all_keys, extrasaction="ignore")
            w.writeheader()
            w.writerows(rows)
        print(f"  {path.name}  ({len(rows)} activities)", flush=True)

    print("Done.", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--email",    required=True)
    parser.add_argument("--password", required=True)
    parser.add_argument("--json",     required=True)
    parser.add_argument("--output",   required=True)
    args = parser.parse_args()

    main(args.email, args.password, Path(args.json), Path(args.output))
