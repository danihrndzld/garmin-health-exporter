# Build Instructions

## Prerequisites

- Node.js 18+ (`brew install node`)
- That's it — no Python needed.

## Development

```bash
cd electron-app
npm install
npm start
```

## Build macOS .app

```bash
cd electron-app
npm install
npm run dist
```

The `.dmg` installer will appear in `electron-app/dist/`.

## First Run

1. Open the app
2. Enter your Garmin Connect email and password
3. Choose how many days back to export
4. Click **Export Health Data** — this downloads your health metrics as JSON
5. Click **Export Activity CSVs** — this uses the JSON to build per-type CSV files (walking, running, gym)

Output is saved to `~/Documents/GarminExport/` by default (configurable).
