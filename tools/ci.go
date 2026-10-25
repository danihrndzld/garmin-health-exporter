package main

// ci is the CI/CD pipeline for the Garmin Export Tauri app (tauri-app/).
//
// Stages (run in order):
//
//  1. clean     remove tauri-app/dist, src-tauri/binaries and the bundle/ folders
//     under src-tauri/target/*/release/ (never the cargo cache)
//  2. install   bun install in tauri-app/
//  3. test      test-report (in process): test IDs, Go, ui/__tests__, Playwright, then
//     bun run core (host Go binary for tauri-build) and cargo test; JUnit
//     files and report.html/pdf in tauri-app/reportes-pruebas/<date>-ci/
//  4. build     git worktree add .worktrees/build-<sha> HEAD, bun install and
//     bun run release there (universal DMG), copy the DMG and the .app
//     to tauri-app/dist/, remove the worktree. --here builds in this
//     checkout instead (only when the app is closed).
//  5. open      codesign --verify --deep --strict on tauri-app/dist/Garmin Export.app,
//     then open -n
//
// The universal build needs rustup with the aarch64-apple-darwin and x86_64-apple-darwin
// targets. Homebrew's Rust has no x86_64 target, so pass an isolated rustup with
// --rustup-home/--cargo-home (or RUSTUP_HOME/CARGO_HOME); $CARGO_HOME/bin then goes first on PATH.
//
// Exit codes: 0 success, 1 stage failure, 2 invalid arguments or bad environment.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const appName = "Garmin Export.app"

var (
	allStages   = []string{"clean", "install", "test", "build", "open"}
	rustTargets = []string{"aarch64-apple-darwin", "x86_64-apple-darwin"}
	bundleDir   = filepath.Join("src-tauri", "target", "universal-apple-darwin", "release", "bundle")
)

func logf(prefix, format string, a ...any) {
	fmt.Printf(prefix+" "+format+"\n", a...)
}

// setToolchain points RUSTUP_HOME and CARGO_HOME at an isolated rustup and puts
// $CARGO_HOME/bin first on PATH, for this process and every command it starts.
func setToolchain(rustupHome, cargoHome string) {
	if rustupHome != "" {
		os.Setenv("RUSTUP_HOME", fromRoot(rustupHome))
	}
	if cargoHome != "" {
		cargoHome = fromRoot(cargoHome)
		os.Setenv("CARGO_HOME", cargoHome)
		os.Setenv("PATH", filepath.Join(cargoHome, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}

type pipeline struct {
	dry  bool
	here bool
}

func (p *pipeline) run(cmd []string, dir string) int {
	logf("  ", "$ %s  (cwd=%s)", shjoin(cmd), rel(dir))
	if p.dry {
		return 0
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir, c.Stdin, c.Stdout, c.Stderr = dir, os.Stdin, os.Stdout, os.Stderr
	return exitCode(c.Run(), os.Stderr)
}

// capture runs a read only command, also in a dry run.
func capture(cmd []string, dir string) (string, error) {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = dir
	out, err := c.Output()
	return string(out), err
}

func (p *pipeline) clean() int {
	logf("==>", "stage: clean")
	app := filepath.Join(root, "tauri-app")
	targets := []string{filepath.Join(app, "dist"), filepath.Join(app, "src-tauri", "binaries")}
	bundles, _ := filepath.Glob(filepath.Join(app, "src-tauri", "target", "*", "release", "bundle"))
	sort.Strings(bundles)
	cmd := []string{"rm", "-rf"}
	for _, t := range append(targets, bundles...) {
		cmd = append(cmd, rel(t))
	}
	return p.run(cmd, root)
}

func (p *pipeline) install() int {
	logf("==>", "stage: install")
	return p.run([]string{"bun", "install"}, filepath.Join(root, "tauri-app"))
}

func (p *pipeline) test() int {
	logf("==>", "stage: test")
	// test-report runs test IDs, Go, ui, Playwright and cargo (after bun run core, which
	// tauri-build needs and clean removed), each to the end, then writes the report to
	// tauri-app/reportes-pruebas/<date>-ci/. GitHub Actions runs the same command.
	logf("  ", "$ go run -C tools . test-report  (cwd=.)")
	if p.dry {
		return 0
	}
	return testReportMain(nil)
}

func (p *pipeline) checkRust() int {
	env := func(k string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		return "(unset)"
	}
	logf("  ", "rust: RUSTUP_HOME=%s CARGO_HOME=%s", env("RUSTUP_HOME"), env("CARGO_HOME"))
	cmd := []string{"rustup", "target", "list", "--installed"}
	logf("  ", "$ %s", shjoin(cmd))
	out, _ := capture(cmd, root)
	installed := strings.Fields(out)
	var missing []string
	for _, t := range rustTargets {
		if !slices.Contains(installed, t) {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return 0
	}
	logf("  !!", "rustup is missing target(s) %s. The universal build needs %s; "+
		"pass --rustup-home/--cargo-home (or set RUSTUP_HOME/CARGO_HOME) for a rustup that has them.", pyList(missing), pyList(rustTargets))
	if p.dry {
		return 0
	}
	return 1
}

func (p *pipeline) buildIn(dir string) int {
	app := filepath.Join(dir, "tauri-app")
	for _, cmd := range [][]string{{"bun", "install"}, {"bun", "run", "release"}} {
		if rc := p.run(cmd, app); rc != 0 {
			return rc
		}
	}
	// In a dry run the worktree does not exist; this checkout has the same version.
	pkg := filepath.Join(app, "package.json")
	if _, err := os.Stat(pkg); err != nil {
		pkg = filepath.Join(root, "tauri-app", "package.json")
	}
	version, err := readPackageVersion(pkg)
	if err != nil {
		logf("  !!", "%v", err)
		return 1
	}
	dist := filepath.Join(root, "tauri-app", "dist")
	dmg := filepath.Join(app, bundleDir, "dmg", "Garmin Export_"+version+"_universal.dmg")
	bundleApp := filepath.Join(app, bundleDir, "macos", appName)
	for _, cmd := range [][]string{
		{"mkdir", "-p", rel(dist)},
		{"rm", "-rf", rel(filepath.Join(dist, appName))},
		{"cp", rel(dmg), rel(dist) + "/"},
		// ditto keeps the bundle's symlinks and signature intact.
		{"ditto", rel(bundleApp), rel(filepath.Join(dist, appName))},
	} {
		if rc := p.run(cmd, root); rc != 0 {
			return rc
		}
	}
	return 0
}

func readPackageVersion(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg struct{ Version string }
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("%s: %v", rel(path), err)
	}
	return pkg.Version, nil
}

func (p *pipeline) build() int {
	suffix := ""
	if p.here {
		suffix = "  (--here: in this checkout)"
	}
	logf("==>", "stage: build%s", suffix)
	if rc := p.checkRust(); rc != 0 {
		return rc
	}
	if p.here {
		return p.buildIn(root)
	}
	if out, _ := capture([]string{"git", "status", "--porcelain"}, root); strings.TrimSpace(out) != "" {
		logf("  !!", "working tree has uncommitted changes; the worktree builds HEAD without them")
	}
	sha, _ := capture([]string{"git", "rev-parse", "--short", "HEAD"}, root)
	wt := filepath.Join(root, ".worktrees", "build-"+strings.TrimSpace(sha))
	if _, err := os.Stat(wt); err == nil {
		logf("  ", "%s is left from an earlier run, removing it", rel(wt))
		p.run([]string{"git", "worktree", "remove", "--force", rel(wt)}, root)
	}
	if rc := p.run([]string{"git", "worktree", "add", "--detach", rel(wt), "HEAD"}, root); rc != 0 {
		return rc
	}
	defer p.run([]string{"git", "worktree", "remove", "--force", rel(wt)}, root)
	return p.buildIn(wt)
}

func (p *pipeline) open() int {
	logf("==>", "stage: open")
	app := filepath.Join(root, "tauri-app", "dist", appName)
	if fi, err := os.Stat(app); !p.dry && (err != nil || !fi.IsDir()) {
		logf("  !!", "%s not found; did build run?", rel(app))
		return 1
	}
	if rc := p.run([]string{"codesign", "--verify", "--deep", "--strict", rel(app)}, root); rc != 0 {
		return rc
	}
	return p.run([]string{"open", "-n", rel(app)}, root)
}

func ciMain(args []string) int {
	if fi, err := os.Stat(filepath.Join(root, "tauri-app")); err != nil || !fi.IsDir() {
		logf("!!", "tauri-app/ not found at %s", filepath.Join(root, "tauri-app"))
		return 2
	}
	fs := flag.NewFlagSet("ci", flag.ContinueOnError)
	skipInstall := fs.Bool("skip-install", false, "skip bun install")
	skipTests := fs.Bool("skip-tests", false, "skip tests")
	skipBuild := fs.Bool("skip-build", false, "skip build")
	skipClean := fs.Bool("skip-clean", false, "skip clean")
	noOpen := fs.Bool("no-open", false, "build but don't launch the app")
	here := fs.Bool("here", false, "build in this checkout, not a worktree (only when the app is closed)")
	dry := fs.Bool("dry-run", false, "print every command without executing")
	rustupHome := fs.String("rustup-home", os.Getenv("RUSTUP_HOME"), "RUSTUP_HOME for the build (default: env)")
	cargoHome := fs.String("cargo-home", os.Getenv("CARGO_HOME"), "CARGO_HOME for the build; its bin/ goes first on PATH (default: env)")
	stagesFlag := fs.String("stages", "", "comma-separated subset of "+pyList(allStages)+" (overrides --skip-* flags)")
	if rc := parseFlags(fs, args); rc >= 0 {
		return rc
	}
	setToolchain(*rustupHome, *cargoHome)

	var stages []string
	if *stagesFlag != "" {
		var unknown []string
		for _, s := range strings.Split(*stagesFlag, ",") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			stages = append(stages, s)
			if !slices.Contains(allStages, s) {
				unknown = append(unknown, s)
			}
		}
		if len(unknown) > 0 {
			logf("!!", "unknown stage(s): %s. valid: %s", pyList(unknown), pyList(allStages))
			return 2
		}
	} else {
		skip := map[string]bool{"clean": *skipClean, "install": *skipInstall, "test": *skipTests, "build": *skipBuild, "open": *noOpen}
		for _, s := range allStages {
			if !skip[s] {
				stages = append(stages, s)
			}
		}
	}
	p := &pipeline{dry: *dry, here: *here}
	dryNote := ""
	if p.dry {
		dryNote = "  (dry run)"
	}
	logf("==>", "pipeline stages: %s%s", pyList(stages), dryNote)

	fns := map[string]func() int{"clean": p.clean, "install": p.install, "test": p.test, "build": p.build, "open": p.open}
	started := time.Now()
	for _, name := range stages {
		t0 := time.Now()
		rc := fns[name]()
		dt := time.Since(t0).Seconds()
		if rc != 0 {
			logf("!!", "stage '%s' failed after %.1fs (exit %d)", name, dt, rc)
			return 1
		}
		logf("==>", "stage '%s' ok (%.1fs)", name, dt)
	}
	logf("==>", "pipeline complete in %.1fs", time.Since(started).Seconds())
	return 0
}
