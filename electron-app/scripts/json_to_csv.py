#!/usr/bin/env python3
#
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Convert garmin_health_*.json → multiple CSVs, one per data type.
Usage: uv run json_to_csv.py [path/to/garmin_health_YYYY-MM-DD.json]
       (defaults to latest file in output/)
"""

import csv
import json
import sys
from pathlib import Path

OUTPUT_DIR = Path(__file__).parent / "output"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def latest_json() -> Path:
    files = sorted(OUTPUT_DIR.glob("garmin_health_*.json"))
    if not files:
        sys.exit("No garmin_health_*.json found in output/")
    return files[-1]


def write_csv(name: str, rows: list[dict], out_dir: Path):
    if not rows:
        return
    path = out_dir / f"{name}.csv"
    keys = list(rows[0].keys())
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=keys, extrasaction="ignore")
        w.writeheader()
        w.writerows(rows)
    print(f"  {path.name}  ({len(rows)} rows)")


def flatten(d: dict, prefix: str = "", sep: str = "_") -> dict:
    """Recursively flatten a nested dict into a single-level dict."""
    out = {}
    for k, v in d.items():
        key = f"{prefix}{sep}{k}" if prefix else k
        if isinstance(v, dict):
            out.update(flatten(v, key, sep))
        elif isinstance(v, list):
            # store lists as JSON strings — keeps CSV simple
            out[key] = json.dumps(v)
        else:
            out[key] = v
    return out


# ---------------------------------------------------------------------------
# Per-section extractors
# ---------------------------------------------------------------------------
def extract_daily_summary(daily: dict) -> list[dict]:
    rows = []
    for date, d in daily.items():
        row = {"date": date}
        # stats
        for key in ["totalKilocalories", "activeKilocalories", "bmrKilocalories",
                    "totalSteps", "totalDistanceMeters", "wellnessKilocalories",
                    "remainingKilocalories", "floorsAscended", "floorsDescended",
                    "durationInMilliseconds", "stepGoal"]:
            row[key] = (d.get("stats") or {}).get(key)
        # heart rate
        hr = d.get("heart_rates") or {}
        row["maxHeartRate"]                  = hr.get("maxHeartRate")
        row["minHeartRate"]                  = hr.get("minHeartRate")
        row["restingHeartRate"]              = hr.get("restingHeartRate")
        row["lastSevenDaysAvgRestingHeartRate"] = hr.get("lastSevenDaysAvgRestingHeartRate")
        # stress
        stress = d.get("stress") or {}
        row["maxStressLevel"] = stress.get("maxStressLevel")
        row["avgStressLevel"] = stress.get("avgStressLevel")
        # intensity minutes
        im = d.get("intensity_minutes") or {}
        row["moderateIntensityMinutes"] = im.get("moderateIntensityMinutes")
        row["vigorousIntensityMinutes"] = im.get("vigorousIntensityMinutes")
        # steps
        steps = d.get("steps") or {}
        if isinstance(steps, dict):
            row["stepsGoal"]     = steps.get("dailyStepGoal")
            row["stepsWellness"] = steps.get("totalSteps")
        # hydration
        hyd = d.get("hydration") or {}
        row["hydrationGoalMl"]         = hyd.get("goalMl")
        row["hydrationValueMl"]        = hyd.get("valueMl")
        row["hydrationSweatLossMl"]    = hyd.get("sweatLossMl")
        rows.append(row)
    return rows


def extract_sleep(daily: dict) -> list[dict]:
    rows = []
    for date, d in daily.items():
        sleep = d.get("sleep") or {}
        dto = sleep.get("dailySleepDTO") or {}
        if not dto:
            continue
        row = {"date": date}
        for key in ["sleepTimeSeconds", "napTimeSeconds", "unmeasurableSeconds",
                    "deepSleepSeconds", "lightSleepSeconds", "remSleepSeconds",
                    "awakeSleepSeconds", "averageSpO2Value", "lowestSpO2Value",
                    "highestSpO2Value", "averageSpO2HRSleep", "averageRespirationValue",
                    "lowestRespirationValue", "highestRespirationValue",
                    "avgSleepStress", "sleepScores", "sleepResultType",
                    "sleepStartTimestampGMT", "sleepEndTimestampGMT"]:
            val = dto.get(key)
            row[key] = json.dumps(val) if isinstance(val, (dict, list)) else val
        rows.append(row)
    return rows


def extract_hrv(daily: dict) -> list[dict]:
    rows = []
    for date, d in daily.items():
        hrv = d.get("hrv") or {}
        summary = hrv.get("hrvSummary") or {}
        if not summary:
            continue
        row = {"date": date}
        for key in ["weeklyAvg", "lastNight", "lastNight5MinHigh", "lastNight5MinLow",
                    "status", "feedbackPhrase", "startTimestampGMT", "endTimestampGMT"]:
            row[key] = summary.get(key)
        rows.append(row)
    return rows


def extract_body_battery(daily: dict) -> list[dict]:
    rows = []
    for date, d in daily.items():
        bb = d.get("body_battery") or []
        for entry in bb:
            row = {"date": date}
            for key in ["charged", "drained", "startTimestampLocal", "endTimestampLocal"]:
                row[key] = entry.get(key)
            rows.append(row)
    return rows


def extract_training(daily: dict) -> list[dict]:
    rows = []
    for date, d in daily.items():
        row = {"date": date}
        # training readiness — may be list or dict
        tr_raw = d.get("training_readiness") or {}
        tr = tr_raw[-1] if isinstance(tr_raw, list) and tr_raw else (tr_raw if isinstance(tr_raw, dict) else {})
        row["readiness_level"]         = tr.get("level")
        row["readiness_feedbackShort"] = tr.get("feedbackShort")
        row["readiness_feedbackLong"]  = tr.get("feedbackLong")
        # morning readiness
        mr_raw = d.get("morning_training_readiness") or {}
        mr = mr_raw[-1] if isinstance(mr_raw, list) and mr_raw else (mr_raw if isinstance(mr_raw, dict) else {})
        row["morning_readiness_level"] = mr.get("level")
        row["morning_readiness_feedback"] = mr.get("feedbackShort")
        # training status
        ts = d.get("training_status") or {}
        vo2 = ts.get("mostRecentVO2Max") or {}
        row["vo2max"] = vo2.get("vo2MaxPreciseValue") or vo2.get("vo2MaxValue")
        tlb = ts.get("mostRecentTrainingLoadBalance") or {}
        row["trainingLoad_7day"]  = tlb.get("sevenDayTrainingLoad")
        row["trainingLoad_28day"] = tlb.get("twentyEightDayTrainingLoad")
        mts = ts.get("mostRecentTrainingStatus") or {}
        row["trainingStatus"] = mts.get("trainingStatusPhrase") or mts.get("trainingStatus")
        # endurance
        end = d.get("endurance_score") or {}
        row["enduranceScore"]        = end.get("overallScore")
        row["enduranceScore_label"]  = end.get("overallScoreLabel")
        rows.append(row)
    return rows


def extract_spo2_respiration(daily: dict) -> list[dict]:
    rows = []
    for date, d in daily.items():
        row = {"date": date}
        spo2 = d.get("spo2") or {}
        row["spo2_avg"]      = spo2.get("averageSpO2")
        row["spo2_lowest"]   = spo2.get("lowestSpO2")
        resp = d.get("respiration") or {}
        row["respiration_avg"]     = resp.get("avgWakingRespirationValue")
        row["respiration_highest"] = resp.get("highestRespirationValue")
        row["respiration_lowest"]  = resp.get("lowestRespirationValue")
        rows.append(row)
    return rows


def extract_activities(activities: list) -> list[dict]:
    rows = []
    for act in activities:
        row = {}
        for key in ["activityId", "activityName", "startTimeLocal", "distance",
                    "duration", "movingDuration", "elapsedDuration",
                    "averageSpeed", "calories", "averageHR", "maxHR", "steps",
                    "aerobicTrainingEffect", "anaerobicTrainingEffect",
                    "activityTrainingLoad", "trainingEffectLabel",
                    "moderateIntensityMinutes", "vigorousIntensityMinutes",
                    "differenceBodyBattery", "lapCount",
                    "hrTimeInZone_1", "hrTimeInZone_2", "hrTimeInZone_3",
                    "hrTimeInZone_4", "hrTimeInZone_5",
                    "minTemperature", "maxTemperature", "avgElevation",
                    "totalReps", "totalSets", "activeSets",
                    "aerobicTrainingEffectMessage", "anaerobicTrainingEffectMessage"]:
            row[key] = act.get(key)
        act_type = act.get("activityType") or {}
        row["activityType"] = act_type.get("typeKey") if isinstance(act_type, dict) else act_type
        rows.append(row)
    return rows


def extract_weight(aggregated: dict) -> list[dict]:
    rows = []
    weigh_ins = aggregated.get("weigh_ins") or {}
    for entry in weigh_ins.get("dateWeightList") or []:
        row = flatten(entry)
        rows.append(row)
    bc = aggregated.get("body_composition") or {}
    # body composition comes as totalAverage or dateList
    for entry in bc.get("dateWeightList") or []:
        rows.append(flatten(entry))
    return rows


def extract_aggregated_misc(aggregated: dict) -> list[dict]:
    """Scalar aggregated metrics → single-row CSVs bundled into one file."""
    row = {}
    for key in ["race_predictions", "lactate_threshold", "cycling_ftp",
                "fitness_age", "hill_score"]:
        val = aggregated.get(key)
        if val is None:
            continue
        if isinstance(val, dict):
            for k, v in flatten(val).items():
                row[f"{key}_{k}"] = v
        else:
            row[key] = val
    return [row] if row else []


def extract_blood_pressure(aggregated: dict) -> list[dict]:
    bp = aggregated.get("blood_pressure") or {}
    rows = []
    for entry in bp.get("measurementSummaries") or []:
        rows.append(flatten(entry))
    return rows


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
def convert(json_path: Path, output_base=None):
    print(f"Reading {json_path.name}...")
    data = json.loads(json_path.read_text())

    date_tag = data.get("export_date", "export")
    base     = output_base if output_base is not None else OUTPUT_DIR
    csv_dir  = base / f"csv_{date_tag}"
    csv_dir.mkdir(parents=True, exist_ok=True)
    print(f"Writing CSVs to {csv_dir}/\n")

    daily      = data.get("daily", {})
    aggregated = data.get("aggregated", {})
    activities = data.get("activities", [])

    write_csv("daily_summary",      extract_daily_summary(daily),           csv_dir)
    write_csv("sleep",              extract_sleep(daily),                    csv_dir)
    write_csv("hrv",                extract_hrv(daily),                      csv_dir)
    write_csv("body_battery",       extract_body_battery(daily),             csv_dir)
    write_csv("training",           extract_training(daily),                 csv_dir)
    write_csv("spo2_respiration",   extract_spo2_respiration(daily),         csv_dir)
    write_csv("activities",         extract_activities(activities),          csv_dir)
    write_csv("weight_body_comp",   extract_weight(aggregated),              csv_dir)
    write_csv("blood_pressure",     extract_blood_pressure(aggregated),      csv_dir)
    write_csv("misc_metrics",       extract_aggregated_misc(aggregated),     csv_dir)

    print("\nDone.")


if __name__ == "__main__":
    path        = Path(sys.argv[1]) if len(sys.argv) > 1 else latest_json()
    output_base = Path(sys.argv[2]) if len(sys.argv) > 2 else None
    convert(path, output_base)
