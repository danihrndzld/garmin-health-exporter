package main

// release ships a version of the Garmin Export Tauri app (tauri-app/).
//
// Nothing reaches origin until the DMG is built and verified:
//
//	preflight  -> clean tree, gh auth, cargo on PATH, no tag or release vX.Y.Z,
//	              origin/main and main are ancestors of HEAD, the main checkout is clean
//	bump       -> version in tauri-app/package.json, src-tauri/tauri.conf.json,
//	              src-tauri/Cargo.toml and Cargo.lock (cargo update --workspace --offline);
//	              one local commit on the source branch
//	build      -> the ci pipeline with --no-open (universal DMG built in a fresh git worktree of HEAD)
//	verify     -> hdiutil verify on the DMG, then mount it read only and check the .app
//	              inside: lipo (arm64 + x86_64), Info.plist version, codesign --verify --deep --strict
//	merge      -> fast forward main to the source branch without a checkout:
//	              git -C <main checkout> merge --ff-only <source>
//	push       -> git tag -a vX.Y.Z, then git push --atomic origin <source> main vX.Y.Z
//	release    -> gh release create vX.Y.Z with the one universal DMG
//
// A failure in build or verify leaves origin untouched and prints how to undo the local bump commit.
//
// Exit codes:
//
//	0  success (release URL printed to stdout as the last line)
//	1  bump, build, verify, merge, push or release failed (stderr contains details)
//	2  refused before any change: invalid arguments or a preflight refusal

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const mainBranch = "main"

var semverRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// exitErr stops a release like Python's SystemExit: deferred cleanups (hdiutil detach) still
// run, and releaseMain turns it into the exit code. Code 2 means nothing changed yet.
type exitErr struct {
	code int
	msg  string
}

func fail(format string, a ...any) { panic(exitErr{1, fmt.Sprintf(format, a...)}) }

// refuse is a preflight refusal: nothing has changed yet, so exit 2 (not 1).
func refuse(format string, a ...any) { panic(exitErr{2, fmt.Sprintf(format, a...)}) }

type runOpts struct {
	dir     string // "" is the repo root
	dry     bool
	noCheck bool // a non zero exit is returned, not fatal
	capture bool
}

type cmdResult struct {
	rc     int
	stdout string
}

// The seams release_test.go replaces, so a test release runs no command at all.
var (
	runCmd        = realRun
	runCI         = ciMain
	ghAuthed      = func() bool { return exec.Command("gh", "auth", "status").Run() == nil }
	releaseExists = func(tag string) bool {
		c := exec.Command("gh", "release", "view", tag)
		c.Dir = root
		return c.Run() == nil
	}
	cargoOnPath   = func() bool { _, err := exec.LookPath("cargo"); return err == nil }
	writeVersions = writeVersionFiles
	dmgPath       = func(version string) string {
		return filepath.Join(root, "tauri-app", "dist", "Garmin Export_"+version+"_universal.dmg")
	}
	notesDir = func() string { return filepath.Join(root, ".tmp") }
)

func appFile(parts ...string) string {
	return filepath.Join(append([]string{root, "tauri-app"}, parts...)...)
}

func realRun(cmd []string, o runOpts) cmdResult {
	dir := o.dir
	if dir == "" {
		dir = root
	}
	logf("  ", "$ %s  (cwd=%s)", shjoin(cmd), rel(dir))
	if o.dry {
		return cmdResult{}
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = dir
	var out, errb bytes.Buffer
	if o.capture {
		c.Stdout, c.Stderr = &out, &errb
	} else {
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	}
	rc := exitCode(c.Run(), nil)
	if !o.noCheck && rc != 0 {
		if o.capture {
			os.Stderr.Write(errb.Bytes())
		}
		fail("!! command failed (exit %d): %s", rc, shjoin(cmd))
	}
	return cmdResult{rc, out.String()}
}

func git(args []string, o runOpts) cmdResult { return runCmd(append([]string{"git"}, args...), o) }

// ---------------------------------------------------------------------------
// Version handling
// ---------------------------------------------------------------------------

func readVersion() string {
	raw, err := os.ReadFile(appFile("package.json"))
	var pkg map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &pkg)
	}
	v, _ := pkg["version"].(string)
	if err != nil || !semverRE.MatchString(v) {
		refuse("!! tauri-app/package.json has no valid version: %s", pyRepr(pkg["version"]))
	}
	return v
}

// pyRepr prints a JSON value roughly as Python's repr does, for error messages.
func pyRepr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return "'" + x + "'"
	default:
		return fmt.Sprint(x)
	}
}

func bump(current, kind string) string {
	var n [3]int
	for i, s := range strings.Split(current, ".") {
		n[i], _ = strconv.Atoi(s)
	}
	switch kind {
	case "major":
		return fmt.Sprintf("%d.0.0", n[0]+1)
	case "minor":
		return fmt.Sprintf("%d.%d.0", n[0], n[1]+1)
	case "patch":
		return fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2]+1)
	}
	fail("!! unknown bump kind: %s", kind)
	return ""
}

// bumpJSON sets the top level "version" of package.json / tauri.conf.json. Only the field
// that holds the current version is replaced, so a file that drifted fails loudly.
func bumpJSON(text, current, next string) string {
	var top map[string]any
	if json.Unmarshal([]byte(text), &top) != nil || top["version"] != current {
		fail(`!! top level "version" is not %s`, current)
	}
	re := regexp.MustCompile(`("version":\s*")` + regexp.QuoteMeta(current) + `(")`)
	m := re.FindStringSubmatchIndex(text)
	if m == nil {
		fail(`!! could not find the top level "version" field`)
	}
	return text[:m[3]] + next + text[m[4]:]
}

// bumpCargoTOML sets the first `version = "..."` line, which is the [package] version.
func bumpCargoTOML(text, current, next string) string {
	m := regexp.MustCompile(`(?m)^version = "([^"]*)"`).FindStringSubmatchIndex(text)
	if m == nil {
		fail("!! first version line of Cargo.toml is not %s: None", current)
	}
	if text[m[2]:m[3]] != current {
		fail("!! first version line of Cargo.toml is not %s: '%s'", current, text[m[0]:m[1]])
	}
	return text[:m[2]] + next + text[m[3]:]
}

func writeVersionFiles(current, next string, dry bool) {
	for _, f := range []struct {
		path string
		fn   func(string, string, string) string
	}{
		{appFile("package.json"), bumpJSON},
		{appFile("src-tauri", "tauri.conf.json"), bumpJSON},
		{appFile("src-tauri", "Cargo.toml"), bumpCargoTOML},
	} {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			fail("!! %v", err)
		}
		out := f.fn(string(raw), current, next)
		verb := "writing"
		if dry {
			verb = "would write"
		}
		logf("  ", "%s version %s to %s", verb, next, rel(f.path))
		if !dry {
			if err := os.WriteFile(f.path, []byte(out), 0o644); err != nil {
				fail("!! %v", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Preflight
// ---------------------------------------------------------------------------

func currentBranch() string {
	return strings.TrimSpace(git([]string{"rev-parse", "--abbrev-ref", "HEAD"}, runOpts{capture: true}).stdout)
}

func isAncestor(ref string) bool {
	return git([]string{"merge-base", "--is-ancestor", ref, "HEAD"}, runOpts{capture: true, noCheck: true}).rc == 0
}

// mainCheckout is the worktree that has main checked out, or "".
func mainCheckout() string {
	path := ""
	for _, line := range strings.Split(git([]string{"worktree", "list", "--porcelain"}, runOpts{capture: true}).stdout, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		} else if line == "branch refs/heads/"+mainBranch {
			return path
		}
	}
	return ""
}

func preflight(next, source string, skipMerge, dry bool) {
	logf("==>", "stage: preflight")
	if _, err := os.Stat(appFile("package.json")); err != nil {
		refuse("!! %s not found; wrong repo?", rel(appFile("package.json")))
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "go.mod")); err != nil {
		refuse("!! tools/go.mod not found; did the tool move?")
	}
	if !cargoOnPath() {
		refuse("!! cargo is not on PATH (it refreshes Cargo.lock). Pass --cargo-home or fix PATH.")
	}
	if dirty := git([]string{"status", "--porcelain"}, runOpts{capture: true}).stdout; strings.TrimSpace(dirty) != "" {
		refuse("!! working tree not clean; commit first:\n%s", dirty)
	}
	if !ghAuthed() {
		refuse("!! gh CLI is not authenticated. Run `gh auth login` and retry.")
	}
	if branch := currentBranch(); branch != source {
		refuse("!! current branch is '%s' but --source is '%s'. Check out %s first or pass --source=%s.", branch, source, source, branch)
	}
	tag := "v" + next
	if strings.TrimSpace(git([]string{"tag", "--list", tag}, runOpts{capture: true}).stdout) == tag {
		refuse("!! local tag %s already exists", tag)
	}
	if strings.TrimSpace(git([]string{"ls-remote", "--tags", "origin", "refs/tags/" + tag}, runOpts{capture: true}).stdout) != "" {
		refuse("!! remote tag %s already exists on origin", tag)
	}
	if releaseExists(tag) {
		refuse("!! GitHub release %s already exists", tag)
	}
	if skipMerge {
		logf("  ", "merge is skipped (on main or --skip-merge)")
	} else {
		// A non fast forward fails here, before any commit or build.
		git([]string{"fetch", "origin", mainBranch}, runOpts{dry: dry})
		for _, ref := range []string{"origin/" + mainBranch, mainBranch} {
			if !isAncestor(ref) {
				refuse("!! %s is not an ancestor of %s; rebase onto origin/%s first", ref, source, mainBranch)
			}
		}
		if mc := mainCheckout(); mc != "" {
			if dirty := strings.TrimSpace(git([]string{"-C", mc, "status", "--porcelain"}, runOpts{capture: true}).stdout); dirty != "" {
				refuse("!! the main checkout %s has uncommitted changes:\n%s", mc, dirty)
			}
		}
	}
	logf("  ", "preflight ok")
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

func stageBump(current, next string, dry bool) {
	logf("==>", "stage: bump  %s -> %s  (local commit only)", current, next)
	writeVersions(current, next, dry)
	// Refreshes the garmin-export entry of Cargo.lock without touching the network.
	runCmd([]string{"cargo", "update", "--workspace", "--offline"}, runOpts{dir: appFile("src-tauri"), dry: dry})
	files := []string{"add"}
	for _, p := range []string{appFile("package.json"), appFile("src-tauri", "tauri.conf.json"), appFile("src-tauri", "Cargo.toml"), appFile("src-tauri", "Cargo.lock")} {
		files = append(files, rel(p))
	}
	git(files, runOpts{dry: dry})
	git([]string{"commit", "-m", "chore(release): bump to v" + next}, runOpts{dry: dry})
}

func stageBuild(dry bool) {
	logf("==>", "stage: build  (via ci: clean,install,test,build; no open)")
	// In a dry run the pipeline runs with --dry-run, so its own commands are printed too.
	args := []string{"--no-open"}
	if dry {
		args = append(args, "--dry-run")
	}
	cmd := append([]string{"go", "run", "-C", "tools", ".", "ci"}, args...)
	logf("  ", "$ %s  (cwd=.)", shjoin(cmd))
	if rc := runCI(args); rc != 0 {
		fail("!! command failed (exit %d): %s", rc, shjoin(cmd))
	}
}

func stageVerify(next string, dry bool) {
	dmg := dmgPath(next)
	logf("==>", "stage: verify  %s", rel(dmg))
	if _, err := os.Stat(dmg); !dry && err != nil {
		fail("!! expected DMG missing: %s", rel(dmg))
	}
	runCmd([]string{"hdiutil", "verify", rel(dmg)}, runOpts{dry: dry, capture: true})
	mnt := "<mount>"
	if !dry {
		var err error
		if mnt, err = os.MkdirTemp("", "garmin-export-dmg-"); err != nil {
			fail("!! %v", err)
		}
	}
	runCmd([]string{"hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mnt, rel(dmg)}, runOpts{dry: dry, capture: true})
	defer func() {
		runCmd([]string{"hdiutil", "detach", mnt}, runOpts{dry: dry, noCheck: true, capture: true})
		if !dry {
			os.RemoveAll(mnt)
		}
	}()
	app := filepath.Join(mnt, appName)
	exes, _ := filepath.Glob(filepath.Join(app, "Contents", "MacOS", "*"))
	sort.Strings(exes)
	if len(exes) == 0 {
		exes = []string{filepath.Join(app, "Contents", "MacOS", "*")}
	}
	for _, exe := range exes {
		archs := strings.Fields(runCmd([]string{"lipo", "-archs", exe}, runOpts{dry: dry, capture: true}).stdout)
		if !dry && !(slices.Contains(archs, "arm64") && slices.Contains(archs, "x86_64")) {
			fail("!! %s in the DMG is not universal: %s", filepath.Base(exe), pyList(archs))
		}
	}
	got := strings.TrimSpace(runCmd([]string{"/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", filepath.Join(app, "Contents", "Info.plist")},
		runOpts{dry: dry, capture: true}).stdout)
	if !dry && got != next {
		fail("!! Info.plist version in the DMG is '%s', expected %s", got, next)
	}
	runCmd([]string{"codesign", "--verify", "--deep", "--strict", app}, runOpts{dry: dry})
}

func stageMerge(source string, dry bool) {
	logf("==>", "stage: merge  %s -> %s (fast forward only, no checkout)", source, mainBranch)
	if mc := mainCheckout(); mc == "" {
		// main is checked out nowhere: move the branch ref, fast forward only.
		git([]string{"fetch", ".", source + ":" + mainBranch}, runOpts{dry: dry})
	} else {
		git([]string{"-C", mc, "merge", "--ff-only", source}, runOpts{dry: dry})
	}
}

func stagePush(source, next string, skipMerge, dry bool) {
	tag := "v" + next
	branches := []string{source}
	if !skipMerge && source != mainBranch {
		branches = append(branches, mainBranch)
	}
	logf("==>", "stage: push  tag %s, then %s and %s in one atomic push", tag, strings.Join(branches, " and "), tag)
	// One atomic push: origin never holds the bump without its tag.
	git([]string{"tag", "-a", tag, "-m", "Release " + tag}, runOpts{dry: dry})
	git(append(append([]string{"push", "--atomic", "origin"}, branches...), tag), runOpts{dry: dry})
}

func stageRelease(next, notes string, prerelease, dry bool) string {
	tag := "v" + next
	logf("==>", "stage: release  %s", tag)
	// The notes go through a file (also in a dry run, .tmp/ is scratch) so the printed
	// command can be pasted as is when the agent may not publish.
	notesFile := filepath.Join(notesDir(), "release-notes-"+tag+".md")
	logf("  ", "writing notes to %s", rel(notesFile))
	if err := os.Mkdir(notesDir(), 0o755); err != nil && !os.IsExist(err) {
		fail("!! %v", err)
	}
	if err := os.WriteFile(notesFile, []byte(notes), 0o644); err != nil {
		fail("!! %v", err)
	}
	cmd := []string{"gh", "release", "create", tag, rel(dmgPath(next)), "--title", tag, "--notes-file", rel(notesFile)}
	if prerelease {
		cmd = append(cmd, "--prerelease")
	}
	out := strings.TrimSpace(runCmd(cmd, runOpts{dry: dry, capture: true}).stdout)
	url := ""
	if out != "" {
		lines := strings.Split(out, "\n")
		url = lines[len(lines)-1]
		fmt.Println(url)
	}
	return url
}

func defaultNotes(next, source string) string {
	r := git([]string{"log", "origin/" + mainBranch + ".." + source, "--pretty=format:- %s"}, runOpts{capture: true, noCheck: true})
	commits := ""
	if r.rc == 0 {
		commits = strings.TrimSpace(r.stdout)
	}
	if commits == "" {
		commits = "- (no new commits found against origin/main)"
	}
	return "# v" + next + "\n\n" +
		"## Changes\n" + commits + "\n\n" +
		"## Install\n" +
		"Download `Garmin.Export_" + next + "_universal.dmg`. The one DMG runs on Apple Silicon and Intel Macs.\n\n" +
		"The app has an ad hoc signature, not an Apple Developer ID, so macOS asks before the first launch. " +
		"The first time, right click the app, choose Open, and confirm. After that it opens normally.\n"
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func releaseMain(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(exitErr)
			if !ok {
				panic(r)
			}
			if e.msg != "" {
				fmt.Fprintln(os.Stderr, e.msg)
			}
			code = e.code
		}
	}()

	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	bumpKind := fs.String("bump", "", "semver bump kind: patch, minor or major")
	version := fs.String("version", "", "explicit X.Y.Z (instead of --bump)")
	source := fs.String("source", "", "source branch (default: current branch)")
	notes := fs.String("notes", "", "release notes body")
	notesFile := fs.String("notes-file", "", "read release notes from this file (relative to the repo root)")
	prerelease := fs.Bool("prerelease", false, "publish as a GitHub pre-release")
	dry := fs.Bool("dry-run", false, "print every command without executing")
	skipMergeFlag := fs.Bool("skip-merge", false, "skip merge into main (release from current branch)")
	rustupHome := fs.String("rustup-home", "", "RUSTUP_HOME for the build (default: env)")
	cargoHome := fs.String("cargo-home", "", "CARGO_HOME for the build; its bin/ goes first on PATH (default: env)")
	if rc := parseFlags(fs, args); rc >= 0 {
		return rc
	}
	switch {
	case (*bumpKind == "") == (*version == ""):
		refuse("error: exactly one of --bump and --version is required")
	case *bumpKind != "" && !slices.Contains([]string{"patch", "minor", "major"}, *bumpKind):
		refuse("error: --bump must be patch, minor or major, got '%s'", *bumpKind)
	case *version != "" && !semverRE.MatchString(*version):
		refuse("error: --version must be X.Y.Z, got '%s'", *version)
	case *notes != "" && *notesFile != "":
		refuse("error: --notes and --notes-file are mutually exclusive")
	}
	setToolchain(*rustupHome, *cargoHome)

	current := readVersion()
	next := *version
	if next == "" {
		next = bump(current, *bumpKind)
	}
	if next == current {
		refuse("!! new version (%s) equals current; nothing to do", next)
	}
	src := *source
	if src == "" {
		src = currentBranch()
	}
	skipMerge := *skipMergeFlag || src == mainBranch

	body := *notes
	if *notesFile != "" {
		raw, err := os.ReadFile(fromRoot(*notesFile))
		if err != nil {
			fail("!! %v", err)
		}
		body = string(raw)
	} else if body == "" {
		body = defaultNotes(next, src)
	}

	logf("==>", "release plan: %s -> %s  (source: %s, prerelease: %s, dry-run: %s)", current, next, src, pyBool(*prerelease), pyBool(*dry))
	preflight(next, src, skipMerge, *dry)
	stageBump(current, next, *dry)
	func() {
		defer func() {
			if r := recover(); r != nil {
				logf("!!", "build or verify failed; origin is untouched. Undo the local bump on %s with: git reset --hard HEAD^", src)
				panic(r)
			}
		}()
		stageBuild(*dry)
		stageVerify(next, *dry)
	}()
	if skipMerge {
		logf("  ", "stage: merge  SKIPPED (on main or --skip-merge)")
	} else {
		stageMerge(src, *dry)
	}
	stagePush(src, next, skipMerge, *dry)
	url := stageRelease(next, body, *prerelease, *dry)
	logf("==>", "release complete: v%s", next)
	if url == "" {
		logf("  ", "(dry run, no release URL)")
	}
	return 0
}
