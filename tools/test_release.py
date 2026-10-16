#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Checks tools/release.py: the version bump on the real config files (in memory),
and the stage order with a fake runner (nothing is pushed before verify passes).

Usage: uv run tools/test_release.py
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.dont_write_bytecode = True
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


class FakeRunner:
    """Records every command; answers the read only ones like a clean repo."""

    def __init__(self, version: str, archs: str = "x86_64 arm64", status: str = "") -> None:
        self.version, self.archs, self.status, self.cmds = version, archs, status, []

    def __call__(self, cmd, cwd=None, dry=False, check=True, capture=False):
        self.cmds.append(cmd)
        out, rc = "", 0
        if cmd[:2] == ["git", "status"]:
            out = self.status
        elif cmd[:3] == ["git", "rev-parse", "--abbrev-ref"]:
            out = "feat/x"
        elif cmd[:3] == ["git", "worktree", "list"]:
            out = "worktree /repo\nHEAD abc\nbranch refs/heads/main\n"
        elif cmd[0] == "lipo":
            out = self.archs
        elif cmd[0].endswith("PlistBuddy"):
            out = self.version
        elif cmd[0] == "gh":
            out = "https://example.invalid/release"
        if check and rc:
            raise SystemExit(1)
        return subprocess.CompletedProcess(cmd, rc, stdout=out, stderr="")


def publishing(cmd) -> bool:
    return (
        cmd[:2] == ["git", "push"]
        or cmd[:3] == ["git", "tag", "-a"]
        or cmd[:3] == ["gh", "release", "create"]
        or ("merge" in cmd and "--ff-only" in cmd)
    )


class OrderTest(unittest.TestCase):
    def run_release(self, runner: FakeRunner) -> None:
        tmp = Path(tempfile.mkdtemp())
        dmg = tmp / "x.dmg"
        dmg.write_text("")
        with (
            mock.patch.object(release, "run", runner),
            mock.patch.object(release, "gh_authed", return_value=True),
            mock.patch.object(release, "release_exists", return_value=False),
            mock.patch.object(release, "write_versions"),
            mock.patch.object(release, "dmg_path", return_value=dmg),
            mock.patch.object(release, "NOTES_DIR", tmp),
        ):
            release.main(["--version", runner.version])

    def test_nothing_is_pushed_before_verify_passes(self) -> None:
        r = FakeRunner("9.8.7")
        self.run_release(r)
        last_verify = max(i for i, c in enumerate(r.cmds) if c[0] == "codesign")
        first_publish = min(i for i, c in enumerate(r.cmds) if publishing(c))
        self.assertLess(last_verify, first_publish)
        self.assertIn(["git", "-C", "/repo", "merge", "--ff-only", "feat/x"], r.cmds)
        self.assertNotIn("checkout", [c[1] for c in r.cmds if c[0] == "git"])

    def test_branches_and_tag_go_in_one_atomic_push(self) -> None:
        r = FakeRunner("9.8.7")
        self.run_release(r)
        pushes = [c for c in r.cmds if c[:2] == ["git", "push"]]
        self.assertEqual(pushes, [["git", "push", "--atomic", "origin", "feat/x", "main", "v9.8.7"]])
        tag = r.cmds.index(["git", "tag", "-a", "v9.8.7", "-m", "Release v9.8.7"])
        self.assertLess(tag, r.cmds.index(pushes[0]))

    def test_a_failed_verify_pushes_nothing(self) -> None:
        r = FakeRunner("9.8.7", archs="arm64")
        with self.assertRaises(SystemExit):
            self.run_release(r)
        self.assertFalse([c for c in r.cmds if publishing(c)])
        self.assertIn("detach", [c[1] for c in r.cmds if c[0] == "hdiutil"])

    def test_a_dirty_tree_is_refused_with_exit_2(self) -> None:
        r = FakeRunner("9.8.7", status=" M tauri-app/package.json")
        with self.assertRaises(SystemExit) as cm, mock.patch("sys.stderr"):
            self.run_release(r)
        self.assertEqual(cm.exception.code, 2)
        self.assertFalse([c for c in r.cmds if c[:2] == ["git", "commit"] or publishing(c)])


if __name__ == "__main__":
    unittest.main()
