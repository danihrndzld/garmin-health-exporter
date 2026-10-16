#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Checks the version bump of tools/release.py on the real config files, in memory.

Usage: uv run tools/test_release.py
"""
from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import release  # noqa: E402


class BumpTest(unittest.TestCase):
    def setUp(self) -> None:
        self.current = release.read_version()

    def test_json_files_change_only_the_version(self) -> None:
        for path in (release.PACKAGE_JSON, release.TAURI_CONF):
            before = path.read_text()
            after = release.bump_json(before, self.current, "9.8.7")
            self.assertEqual(json.loads(after)["version"], "9.8.7", path)
            self.assertEqual(after.replace('"version": "9.8.7"', f'"version": "{self.current}"', 1), before, path)

    def test_cargo_toml_changes_only_the_package_version(self) -> None:
        before = release.CARGO_TOML.read_text()
        after = release.bump_cargo_toml(before, self.current, "9.8.7")
        self.assertIn('[package]\nname = "garmin-export"\nversion = "9.8.7"', after)
        self.assertEqual(after.replace('version = "9.8.7"', f'version = "{self.current}"', 1), before)

    def test_a_drifted_file_fails(self) -> None:
        with self.assertRaises(SystemExit):
            release.bump_json('{"version": "1.0.0"}', "2.0.0", "2.1.0")
        with self.assertRaises(SystemExit):
            release.bump_cargo_toml('[package]\nversion = "1.0.0"\n', "2.0.0", "2.1.0")

    def test_bump_kinds(self) -> None:
        self.assertEqual(release.bump("4.4.0", "patch"), "4.4.1")
        self.assertEqual(release.bump("4.4.1", "minor"), "4.5.0")
        self.assertEqual(release.bump("4.5.0", "major"), "5.0.0")


if __name__ == "__main__":
    unittest.main()
