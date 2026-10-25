package main

// Checks release: the version bump on the real config files (in memory), and the stage order
// with a fake runner (nothing is pushed before verify passes). No command runs for real.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func setRoot(t *testing.T) {
	t.Helper()
	wd, _ := os.Getwd()
	r, err := findRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	root = r
}

func TestBumpJSONFilesChangeOnlyTheVersion(t *testing.T) {
	setRoot(t)
	current := readVersion()
	for _, path := range []string{appFile("package.json"), appFile("src-tauri", "tauri.conf.json")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before := string(raw)
		after := bumpJSON(before, current, "9.8.7")
		var top map[string]any
		if err := json.Unmarshal([]byte(after), &top); err != nil || top["version"] != "9.8.7" {
			t.Errorf("%s: version is %v (%v)", path, top["version"], err)
		}
		if strings.Replace(after, `"version": "9.8.7"`, `"version": "`+current+`"`, 1) != before {
			t.Errorf("%s: more than the version changed", path)
		}
	}
}

func TestBumpCargoTOMLChangesOnlyThePackageVersion(t *testing.T) {
	setRoot(t)
	current := readVersion()
	raw, err := os.ReadFile(appFile("src-tauri", "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	before := string(raw)
	after := bumpCargoTOML(before, current, "9.8.7")
	if !strings.Contains(after, "[package]\nname = \"garmin-export\"\nversion = \"9.8.7\"") {
		t.Error("the [package] version did not change")
	}
	if strings.Replace(after, `version = "9.8.7"`, `version = "`+current+`"`, 1) != before {
		t.Error("more than the version changed")
	}
}

// exits runs f and returns the exit code it stopped with, or -1 when it returned.
func exits(f func()) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = r.(exitErr).code
		}
	}()
	f()
	return -1
}

func TestADriftedFileFails(t *testing.T) {
	if exits(func() { bumpJSON(`{"version": "1.0.0"}`, "2.0.0", "2.1.0") }) != 1 {
		t.Error("bumpJSON accepted a drifted file")
	}
	if exits(func() { bumpCargoTOML("[package]\nversion = \"1.0.0\"\n", "2.0.0", "2.1.0") }) != 1 {
		t.Error("bumpCargoTOML accepted a drifted file")
	}
}

func TestBumpKinds(t *testing.T) {
	for _, c := range [][3]string{{"4.4.0", "patch", "4.4.1"}, {"4.4.1", "minor", "4.5.0"}, {"4.5.0", "major", "5.0.0"}} {
		if got := bump(c[0], c[1]); got != c[2] {
			t.Errorf("bump(%s, %s) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

// fakeRunner records every command; it answers the read only ones like a clean repo.
type fakeRunner struct {
	version, archs, status string
	cmds                   [][]string
	opts                   []runOpts // opts[i] belongs to cmds[i]
	versionsDry            bool
}

func (f *fakeRunner) run(cmd []string, o runOpts) cmdResult {
	f.cmds, f.opts = append(f.cmds, cmd), append(f.opts, o)
	out := ""
	switch {
	case slices.Equal(cmd[:2], []string{"git", "status"}):
		out = f.status
	case len(cmd) > 2 && slices.Equal(cmd[:3], []string{"git", "rev-parse", "--abbrev-ref"}):
		out = "feat/x"
	case len(cmd) > 2 && slices.Equal(cmd[:3], []string{"git", "worktree", "list"}):
		out = "worktree /repo\nHEAD abc\nbranch refs/heads/main\n"
	case cmd[0] == "lipo":
		out = f.archs
	case strings.HasSuffix(cmd[0], "PlistBuddy"):
		out = f.version
	case cmd[0] == "gh":
		out = "https://example.invalid/release"
	}
	return cmdResult{0, out}
}

func publishing(cmd []string) bool {
	return slices.Equal(cmd[:2], []string{"git", "push"}) ||
		(len(cmd) > 2 && slices.Equal(cmd[:3], []string{"git", "tag", "-a"})) ||
		(len(cmd) > 2 && slices.Equal(cmd[:3], []string{"gh", "release", "create"})) ||
		(slices.Contains(cmd, "merge") && slices.Contains(cmd, "--ff-only"))
}

// runRelease runs releaseMain with every seam faked and returns its exit code.
func runRelease(t *testing.T, f *fakeRunner, extra ...string) int {
	t.Helper()
	setRoot(t)
	tmp := t.TempDir()
	dmg := filepath.Join(tmp, "x.dmg")
	if err := os.WriteFile(dmg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	saved := []any{runCmd, runCI, ghAuthed, releaseExists, cargoOnPath, writeVersions, dmgPath, notesDir}
	t.Cleanup(func() {
		runCmd, runCI = saved[0].(func([]string, runOpts) cmdResult), saved[1].(func([]string) int)
		ghAuthed, releaseExists, cargoOnPath = saved[2].(func() bool), saved[3].(func(string) bool), saved[4].(func() bool)
		writeVersions, dmgPath, notesDir = saved[5].(func(string, string, bool)), saved[6].(func(string) string), saved[7].(func() string)
	})
	runCmd = f.run
	runCI = func(args []string) int {
		f.cmds, f.opts = append(f.cmds, append([]string{"go", "run", "-C", "tools", ".", "ci"}, args...)), append(f.opts, runOpts{})
		return 0
	}
	ghAuthed = func() bool { return true }
	releaseExists = func(string) bool { return false }
	cargoOnPath = func() bool { return true }
	writeVersions = func(_, _ string, dry bool) { f.versionsDry = dry }
	dmgPath = func(string) string { return dmg }
	notesDir = func() string { return tmp }
	return releaseMain(append([]string{"--version", f.version}, extra...))
}

func indexOf(cmds [][]string, cmd []string) int {
	return slices.IndexFunc(cmds, func(c []string) bool { return slices.Equal(c, cmd) })
}

func TestNothingIsPushedBeforeVerifyPasses(t *testing.T) {
	f := &fakeRunner{version: "9.8.7", archs: "x86_64 arm64"}
	if rc := runRelease(t, f); rc != 0 {
		t.Fatalf("exit %d", rc)
	}
	lastVerify, firstPublish := -1, -1
	for i, c := range f.cmds {
		if c[0] == "codesign" {
			lastVerify = i
		}
		if publishing(c) && firstPublish < 0 {
			firstPublish = i
		}
	}
	build := indexOf(f.cmds, []string{"go", "run", "-C", "tools", ".", "ci", "--no-open"})
	if build < 0 || lastVerify < build || firstPublish < lastVerify {
		t.Errorf("order: build %d, last verify %d, first publish %d", build, lastVerify, firstPublish)
	}
	if indexOf(f.cmds, []string{"git", "-C", "/repo", "merge", "--ff-only", "feat/x"}) < 0 {
		t.Error("main was not fast forwarded in its checkout")
	}
	for _, c := range f.cmds {
		if c[0] == "git" && c[1] == "checkout" {
			t.Error("the release ran git checkout")
		}
	}
}

func TestBranchesAndTagGoInOneAtomicPush(t *testing.T) {
	f := &fakeRunner{version: "9.8.7", archs: "x86_64 arm64"}
	if rc := runRelease(t, f); rc != 0 {
		t.Fatalf("exit %d", rc)
	}
	var pushes [][]string
	for _, c := range f.cmds {
		if slices.Equal(c[:2], []string{"git", "push"}) {
			pushes = append(pushes, c)
		}
	}
	want := []string{"git", "push", "--atomic", "origin", "feat/x", "main", "v9.8.7"}
	if len(pushes) != 1 || !slices.Equal(pushes[0], want) {
		t.Fatalf("pushes = %v, want one %v", pushes, want)
	}
	tag := indexOf(f.cmds, []string{"git", "tag", "-a", "v9.8.7", "-m", "Release v9.8.7"})
	if tag < 0 || tag > indexOf(f.cmds, want) {
		t.Errorf("the tag (%d) must come before the push", tag)
	}
}

func TestAFailedVerifyPushesNothing(t *testing.T) {
	f := &fakeRunner{version: "9.8.7", archs: "arm64"}
	if rc := runRelease(t, f); rc != 1 {
		t.Fatalf("exit %d, want 1", rc)
	}
	detached := false
	for _, c := range f.cmds {
		if publishing(c) {
			t.Errorf("published after a failed verify: %v", c)
		}
		detached = detached || (c[0] == "hdiutil" && c[1] == "detach")
	}
	if !detached {
		t.Error("the DMG was not detached")
	}
}

func TestADirtyTreeIsRefusedWithExit2(t *testing.T) {
	f := &fakeRunner{version: "9.8.7", archs: "x86_64 arm64", status: " M tauri-app/package.json"}
	if rc := runRelease(t, f); rc != 2 {
		t.Fatalf("exit %d, want 2", rc)
	}
	for _, c := range f.cmds {
		if slices.Equal(c[:2], []string{"git", "commit"}) || publishing(c) {
			t.Errorf("ran %v after the refusal", c)
		}
	}
}

func TestADryRunChangesNothing(t *testing.T) {
	f := &fakeRunner{version: "9.8.7", archs: "", status: ""}
	if rc := runRelease(t, f, "--dry-run"); rc != 0 {
		t.Fatalf("exit %d", rc)
	}
	if indexOf(f.cmds, []string{"go", "run", "-C", "tools", ".", "ci", "--no-open", "--dry-run"}) < 0 {
		t.Error("the pipeline did not get --dry-run")
	}
	if !f.versionsDry {
		t.Error("the version files were written in a dry run")
	}
	for i, c := range f.cmds {
		if (!f.opts[i].capture || publishing(c)) && !f.opts[i].dry && c[0] != "go" {
			t.Errorf("%v ran for real in a dry run", c)
		}
	}
}
