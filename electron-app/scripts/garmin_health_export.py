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
Garmin Connect Health Data Exporter
Downloads comprehensive health & fitness data.
Usage: uv run garmin_health_export.py --email E --password P --days N --output DIR
"""

import argparse
import json
import os
import sys
from datetime import date, timedelta
from pathlib import Path


def date_range(days_back: int):
    today = date.today()
    return [(today - timedelta(days=i)).isoformat() for i in range(days_back)]


def safe(fn, *args, label="", **kwargs):
    try:
        return fn(*args, **kwargs)
    except Exception as e:
        print(f"  [skip] {label or fn.__name__}: {e}", flush=True)
        return None


def export(email: str, password: str, days_back: int, output_dir: Path) -> Path:
    from garminconnect import Garmin

    print(f"Connecting as {email}…", flush=True)
    client = Garmin(email, password)
    client.login()
    print("Login successful.", flush=True)

    today      = date.today().isoformat()
    start_date = (date.today() - timedelta(days=days_back - 1)).isoformat()
    dates      = date_range(days_back)

    print(f"Pulling {days_back} days: {start_date} → {today}", flush=True)

    data = {
        "export_date": today,
        "date_range": {"start": start_date, "end": today, "days": days_back},
        "daily": {},
        "aggregated": {},
        "activities": [],
        "goals": [],
    }

    # Per-day metrics
    for i, d in enumerate(dates):
        print(f"  {d}", flush=True)
        daily = {}

        daily["stats"]                      = safe(client.get_stats,                        d, label="stats")
        daily["user_summary"]               = safe(client.get_user_summary,                 d, label="user_summary")
        daily["heart_rates"]                = safe(client.get_heart_rates,                  d, label="heart_rates")
        daily["rhr"]                        = safe(client.get_rhr_day,                      d, label="rhr")
        daily["hrv"]                        = safe(client.get_hrv_data,                     d, label="hrv")
        daily["stress"]                     = safe(client.get_stress_data,                  d, label="stress")
        daily["all_day_stress"]             = safe(client.get_all_day_stress,               d, label="all_day_stress")
        daily["sleep"]                      = safe(client.get_sleep_data,                   d, label="sleep")
        daily["body_battery"]               = safe(client.get_body_battery,                 d, d, label="body_battery")
        daily["body_battery_events"]        = safe(client.get_body_battery_events,          d, label="body_battery_events")
        daily["respiration"]                = safe(client.get_respiration_data,             d, label="respiration")
        daily["spo2"]                       = safe(client.get_spo2_data,                    d, label="spo2")
        daily["steps"]                      = safe(client.get_steps_data,                   d, label="steps")
        daily["intensity_minutes"]          = safe(client.get_intensity_minutes_data,       d, label="intensity_minutes")
        daily["hydration"]                  = safe(client.get_hydration_data,               d, label="hydration")
        daily["training_readiness"]         = safe(client.get_training_readiness,           d, label="training_readiness")
        daily["morning_training_readiness"] = safe(client.get_morning_training_readiness,   d, label="morning_training_readiness")
        daily["training_status"]            = safe(client.get_training_status,              d, label="training_status")
        daily["weigh_ins"]                  = safe(client.get_daily_weigh_ins,              d, label="weigh_ins")
        daily["max_metrics"]                = safe(client.get_max_metrics,                  d, label="max_metrics")
        daily["endurance_score"]            = safe(client.get_endurance_score,              d, label="endurance_score")
        daily["running_tolerance"]          = safe(client.get_running_tolerance,            d, d, label="running_tolerance")

        data["daily"][d] = daily
        print(f"PROGRESS:daily:{i + 1}:{days_back}", flush=True)

    # Aggregated metrics
    print("Pulling aggregated metrics…", flush=True)
    data["aggregated"]["blood_pressure"]   = safe(client.get_blood_pressure,                start_date, today, label="blood_pressure")
    data["aggregated"]["weigh_ins"]        = safe(client.get_weigh_ins,                     start_date, today, label="weigh_ins_range")
    data["aggregated"]["daily_steps"]      = safe(client.get_daily_steps,                   start_date, today, label="daily_steps")
    data["aggregated"]["weekly_steps"]     = safe(client.get_weekly_steps,                  start_date, today, 1, label="weekly_steps")
    data["aggregated"]["weekly_intensity"] = safe(client.get_weekly_intensity_minutes,      start_date, today, label="weekly_intensity")
    data["aggregated"]["body_composition"] = safe(client.get_body_composition,              start_date, today, label="body_composition")
    data["aggregated"]["progress_summary"] = safe(client.get_progress_summary_between_dates, start_date, today, label="progress_summary")
    data["aggregated"]["race_predictions"] = safe(client.get_race_predictions,                           label="race_predictions")
    data["aggregated"]["lactate_threshold"]= safe(client.get_lactate_threshold,                          label="lactate_threshold")
    data["aggregated"]["cycling_ftp"]      = safe(client.get_cycling_ftp,                                label="cycling_ftp")
    data["aggregated"]["fitness_age"]      = safe(client.get_fitnessage_data,               today,       label="fitness_age")
    data["aggregated"]["hill_score"]       = safe(client.get_hill_score,                    start_date, today, label="hill_score")

    # Activities
    print("Pulling activities…", flush=True)
    activities = safe(client.get_activities_by_date, start_date, today, label="activities_by_date") or []
    for i, act in enumerate(activities):
        act_id = act.get("activityId")
        name   = act.get("activityName") or act.get("activityType", {}).get("typeKey") or "unknown"
        print(f"  [{i + 1}/{len(activities)}] {(act.get('startTimeLocal') or '').split(' ')[0]} | {name}", flush=True)
        if act_id:
            act["splits"]       = safe(client.get_activity_splits,       act_id, label=f"splits/{act_id}")
            act["typed_splits"] = safe(client.get_activity_typed_splits, act_id, label=f"typed_splits/{act_id}")
        print(f"PROGRESS:activities:{i + 1}:{len(activities)}", flush=True)
    data["activities"] = activities

    # Goals
    data["goals"] = safe(client.get_goals, "active", label="goals_active") or []

    # Save JSON
    output_dir.mkdir(parents=True, exist_ok=True)
    from datetime import datetime
    ts = datetime.now().strftime("%H-%M-%S")
    out_file = output_dir / f"garmin_health_{today}_{ts}.json"
    out_file.write_text(json.dumps(data, indent=2, default=str))
    print(f"JSON_PATH:{out_file}", flush=True)
    print(f"Done. {days_back} days | {len(activities)} activities", flush=True)
    return out_file


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--email",    required=True)
    parser.add_argument("--password", required=True)
    parser.add_argument("--days",     type=int, default=7)
    parser.add_argument("--output",   required=True)
    args = parser.parse_args()

    export(args.email, args.password, args.days, Path(args.output))
