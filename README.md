# Garmin Data Exporter

A macOS desktop app that downloads your complete Garmin Connect health data and exports it as structured CSV files — ready for analysis, spreadsheets, or LLM context.

---

## What it exports

**Daily metrics** (per day, for the chosen date range):
- Heart rate (resting, min, max, HRV)
- Sleep (duration, stages, SpO2, respiration)
- Stress & body battery
- Steps, intensity minutes, hydration
- Training readiness & morning readiness
- Training status, VO2max, endurance score
- Running tolerance, weight & weigh-ins

**Activity CSVs** (per type, with detailed metrics + lap data):
- `activities_caminar.csv` — walking & hiking
- `activities_correr.csv` — running & treadmill
- `activities_gym.csv` — strength training & indoor cardio
- `activities.csv` — all activities combined

**Health summary CSVs:**
`daily_summary`, `sleep`, `hrv`, `body_battery`, `training`, `spo2_respiration`, `weight_body_comp`, `blood_pressure`, `misc_metrics`

---

## Download

Go to [Releases](https://github.com/danihrndzld/garmin-health-exporter/releases/latest) and download the DMG for your Mac:

| File | For |
|------|-----|
| `Garmin Data Exporter-*-arm64.dmg` | Apple Silicon (M1 / M2 / M3 / M4) |
| `Garmin Data Exporter-*.dmg` | Intel |

---

## First-time setup (macOS security)

The app is not signed with an Apple Developer certificate, so macOS will block it on first launch.

### Option A — Remove Quarantine.command (recommended)

1. Open the DMG and drag the app to `/Applications`
2. Double-click **`Remove Quarantine.command`** (also inside the DMG)
3. Open the app normally

### Option B — Terminal

```bash
xattr -rd com.apple.quarantine "/Applications/Garmin Data Exporter.app"
```

### Option C — Right-click

Right-click the app → **Open** → click **Open** in the dialog. Only needed once.

---

## Usage

1. Enter your Garmin Connect email and password
2. Set the number of days to export (1–90)
3. Choose an output folder (default: `~/Documents/GarminExport`)
4. Click **Download Health Data**

The app runs the full pipeline automatically:

```
Step 1 — Download health data from Garmin API  →  garmin_health_YYYY-MM-DD_HH-MM-SS.json
Step 2 — Convert to health CSVs               →  csv_YYYY-MM-DD/
Step 3 — Download activity details per type   →  csv_YYYY-MM-DD/activities_*.csv
```

Credentials are saved locally between sessions. The raw JSON is kept for debugging.

---

## Dependencies

The app handles everything automatically. On first run it will install [`uv`](https://github.com/astral-sh/uv) if not present — no Terminal required.

If you prefer to install it manually:

```bash
brew install uv
# or
curl -LsSf https://astral.sh/uv/install.sh | sh
```

Python dependencies (`garminconnect`, `python-dotenv`) are managed by `uv` and installed automatically per-script — nothing is installed globally.

---

## CLI scripts (optional)

The Python scripts can also be run standalone without the Electron app:

```bash
# Export health data
uv run garmin_health_export.py --email you@example.com --password yourpass --days 30 --output ./output

# Convert JSON to CSVs
uv run json_to_csv.py output/garmin_health_2026-03-19_*.json output/

# Download activity details (per-type CSVs with lap data)
uv run download_activity_details.py --email you@example.com --password yourpass \
  --json output/garmin_health_2026-03-19_*.json --output output/
```

---

## Building from source

```bash
# Prerequisites: Node.js 18+, uv
git clone https://github.com/danihrndzld/garmin-health-exporter.git
cd garmin-health-exporter/electron-app

npm install
npm run fonts     # download bundled WOFF2 fonts (run once)
npm start         # development mode
npm run dist      # build .dmg installers → electron-app/dist/
```

---

## Output structure

```
~/Documents/GarminExport/
├── garmin_health_2026-03-19_15-30-00.json   ← raw data (kept for debugging)
└── csv_2026-03-19/
    ├── daily_summary.csv
    ├── sleep.csv
    ├── hrv.csv
    ├── body_battery.csv
    ├── training.csv
    ├── spo2_respiration.csv
    ├── weight_body_comp.csv
    ├── blood_pressure.csv
    ├── misc_metrics.csv
    ├── activities.csv
    ├── activities_caminar.csv
    ├── activities_correr.csv
    └── activities_gym.csv
```
