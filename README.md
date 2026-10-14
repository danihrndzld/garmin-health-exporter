<div align="center">

<img src="docs/images/icon.png" alt="Garmin Export icon" width="128" height="128">

<h1>Garmin Export</h1>

**A Mac app that downloads your Garmin Connect health data as CSV files.<br>It also packs the files so you can give them to Claude, ChatGPT, or Gemini.**

[![Latest release](https://img.shields.io/github/v/release/danihrndzld/garmin-health-exporter?style=flat-square&color=c6f432&label=release)](https://github.com/danihrndzld/garmin-health-exporter/releases/latest)
[![macOS 11 or later, Apple silicon and Intel](https://img.shields.io/badge/macOS-11%2B%20universal-555555?style=flat-square&logo=apple)](#install)
[![Tauri 2](https://img.shields.io/badge/Tauri-2-555555?style=flat-square&logo=tauri)](#build-it-yourself)

**[Download the latest version](https://github.com/danihrndzld/garmin-health-exporter/releases/latest)**

[What it exports](#what-it-exports) | [Install](#install) | [First export](#first-export) | [Give the data to an LLM](#give-the-data-to-an-llm) | [Questions](#questions) | [Build it yourself](#build-it-yourself)

<br>

<img src="docs/images/garmin-export.png" alt="The Garmin Export window after a 90 day export, with progress rings on the left and the LLM pack panel on the right, showing an English context file, a token meter at 119k tokens, and 18 CSV files" width="900">

</div>

<br>

Garmin Export signs in to Garmin Connect with your account, downloads each day in the range you pick, and writes one CSV file per kind of data. The files are plain text with a header row, so you can open them in Numbers, Excel, or pandas, or upload them to a chat with an LLM.

Garmin Export is for personal use with your own account. It isn't affiliated with Garmin.

---

## What it exports

Each export creates a folder named `csv_YYYY-MM-DD` inside your output folder, which is `~/Documents/GarminExport` by default. The folder holds these files.

- Daily health data, with one row per day, in `daily_summary.csv`, `sleep.csv`, `hrv.csv`, `body_battery.csv`, `training.csv`, and `spo2_respiration.csv`. The daily summary covers steps, calories, heart rate, stress, intensity minutes, and hydration.
- Longer term data in `weight_body_comp.csv`, `blood_pressure.csv`, `misc_metrics.csv`, `running_tolerance.csv`, `daily_steps_aggregated.csv`, `weekly_intensity.csv`, and `hill_score_daily.csv`. The misc file holds fitness age, race predictions, lactate threshold, and FTP.
- Activities, with every activity in `activities.csv` and one file per type in `caminar.csv`, `correr.csv`, and `gym.csv`. Each type also has a laps file, e.g., `correr_laps.csv`.
- The LLM pack folders, after you click a button in the pack panel, as described in [Give the data to an LLM](#give-the-data-to-an-llm).

The app also saves the raw JSON that Garmin returned next to the folder, in case you need a field that the CSV files leave out. A file is missing from the folder when Garmin has no data of that kind for your account, e.g., `blood_pressure.csv` when you never logged a reading.

## Install

Garmin Export needs macOS 11 or later, and the same download runs on Apple silicon and Intel Macs. The app isn't signed with an Apple Developer ID, so macOS asks you to confirm it the first time you open it.

1. Download `Garmin Export_X.Y.Z_universal.dmg` from the [latest release](https://github.com/danihrndzld/garmin-health-exporter/releases/latest).
2. Open the DMG, and drag Garmin Export to your Applications folder.
3. Open Garmin Export. When macOS says it can't verify the app, click Done.
4. Open System Settings, go to Privacy & Security, and click Open Anyway next to the Garmin Export message. Then confirm with your password.

<details>
<summary>Prefer the Terminal?</summary>

This command removes the download flag, and Garmin Export then opens normally.

```sh
xattr -dr com.apple.quarantine "/Applications/Garmin Export.app"
```

</details>

You only do this once for each version. To check for a new version, click the version number in the top left corner of the window. When a newer release exists, the label changes to the new version number, and a second click opens the release page.

## First export

1. Type your Garmin Connect email and password in the Setup card. The app remembers your email, and it doesn't save your password.
2. Pick a range at the top of the window. You can choose the last 7, 30, or 90 days, or choose Custom and set a start and end date.
3. Click the Pull button, or press Command and Return. The Run card shows each step, and you can press Escape to cancel.

After the first sign in, the app keeps the Garmin login tokens in its Application Support folder, so later exports reuse the tokens and don't sign in to Garmin again until the tokens expire. The password field still has to be filled in for each export. Downloaded days are also kept in a local cache, so a second export of the same range only asks Garmin for the newest days.

<div align="center">
<img src="docs/images/export-done.png" alt="The main Garmin Export window, with progress rings on the left, the setup and run cards in the middle, and step and sleep charts on the right" width="900">
</div>

## Give the data to an LLM

Press L, or click LLM pack, to open the pack panel. The panel shows the description of your data in the language you pick, Spanish or English, and an estimate of the token count against a 200,000 token context window. The export itself doesn't write any LLM files. The app writes them only when you click one of the two buttons, and it writes only the language you picked.

1. Build folder writes `llm-en`, which holds `CONTEXT.en.md` and a copy of every CSV file, and it opens the folder. Drag everything in the folder into the chat, and then type your question.
2. Build one file writes `llm-en-one-file`, which holds one Markdown file with the description and every CSV file inside it, and it selects the file in Finder. Drag the file into the chat.

Each folder holds the data once, so you can drag the whole folder without sending the same rows twice. A second click replaces the folder. `CONTEXT.en.md` lists the date range, each file with its row count and dates, each column with its unit, and any data that Garmin didn't return, and it doesn't ask the LLM to do anything. The panel also has links that open claude.ai, chatgpt.com, and gemini.google.com.

The raw JSON is never part of the pack, because a 60 day export is about 38 MB, and Claude accepts files up to 30 MB. The CSV files for a 90 day export are about 350 KB, which is about 120,000 tokens.

## Questions

<details>
<summary>What is the refresh window?</summary>

The refresh window is the number of recent days that the app downloads again on every export, even when those days are already in the cache. Garmin keeps changing the newest days after you sync, e.g., last night's sleep can arrive late, and today's steps keep going up. With the default of 3 days, the app downloads today, yesterday, and the day before again, and it reads older days from the cache. The setting applies to daily data only, because activities don't change after you save them.

</details>

<details>
<summary>What does a line that starts with [skip] mean?</summary>

A `[skip]` line means that Garmin returned no data for one kind of data, and the export kept going without it. For example, `endurance_score: Empty response body` means that Garmin answered with HTTP 204, which is an empty answer, usually because your watch doesn't record that score. The skipped data is listed under missing data in `CONTEXT.en.md` when you build the LLM pack.

</details>

<details>
<summary>What happens when an export fails?</summary>

When an export fails, the Run card shows a red Report it bar with two buttons. GitHub issue opens a new issue on this repository with the error and the last lines of the log filled in, and email opens a message to the developer with the same text. Before the text leaves your Mac, the app replaces tokens and passwords with `REDACTED`, email addresses with `[email]`, and your home folder path with `~`.

</details>

<details>
<summary>I used Garmin Data Exporter 3.x. What changes?</summary>

Version 4 is a new app named Garmin Export built with Tauri instead of Electron. macOS treats it as a new app, so type your email again and sign in once. The CSV file names are the same as in 3.3. You can delete `Garmin Data Exporter.app` after you install version 4. The update button in 3.x stays on Checking... when a newer version exists, because of a bug in how 3.x compares version numbers, so download version 4 from the releases page by hand.

</details>

<details>
<summary>What does Clear cache do?</summary>

Clear cache deletes the local copy of downloaded days, so the next export downloads the whole range from Garmin again. Use it when you think that older days in your CSV files don't match Garmin Connect.

</details>

---

## Build it yourself

<details>
<summary>Requirements, build, and tests</summary>

You need Node.js 18 or later, Bun, Rust, and the Xcode command line tools. The Garmin code lives in `electron-app/garmin`, so install its packages first, and then run the app from `tauri-app`.

```sh
git clone https://github.com/danihrndzld/garmin-health-exporter.git
cd garmin-health-exporter
(cd electron-app && npm install)
cd tauri-app
bun install
bun run dev
```

`bun run build` makes an app and a DMG for your own Mac. `bun run release` makes a universal app and DMG, and it needs the `aarch64-apple-darwin` and `x86_64-apple-darwin` Rust targets, which you can add with `rustup target add`.

To run the tests, use the commands below. The first command tests the Garmin client and cache, and the second tests the LLM pack and the error report.

```sh
(cd electron-app && for f in garmin/__tests__/*.test.js; do node "$f"; done)
(cd tauri-app && bun test sidecar)
```

</details>

<details>
<summary>Project layout</summary>

- `tauri-app/ui` holds the window, written in plain HTML, CSS, and JavaScript with no build step. `ui/dev-mock.js` lets you open `index.html` in a browser with fake data.
- `tauri-app/sidecar` holds `garmin-core`, a small program compiled with `bun build --compile`. The window starts it for each export and sends one JSON request on its standard input, so your password never appears in the process list. `garmin-core` checks the range, the refresh window, and the output folder before it does anything.
- `electron-app/garmin` holds the Garmin sign in, the API client, the SQLite cache, and the CSV writer. The 3.x Electron app in `electron-app` still uses the same code.
- `garmin_health_export.py`, `json_to_csv.py`, and `download_activity_details.py` are the original Python scripts, which you can run with `uv run`. The app doesn't use them.

</details>
