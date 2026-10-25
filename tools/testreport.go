package main

// test-report runs every test suite of tauri-app/ with machine readable results and builds one report.
//
// Suites, in order (one task per signal; a failing suite never stops the next one):
//
//	ids     bun scripts/test-ids.js                      (gate: unique technique IDs)
//	go      go vet, then go test -json -coverprofile     -> go-junit.xml, go-coverage.json
//	ui      bun test ui/__tests__ --reporter=junit       -> ui-junit.xml
//	e2e     bunx playwright test (Chromium + WebKit)     -> e2e-junit.xml, e2e.json (flaky count)
//	cargo   bun run core, then cargo test (text log only; cargo has no stable JUnit output)
//
// Output: tauri-app/reportes-pruebas/<date>-ci/ (gitignored): one .log per suite, the JUnit
// files, report.json, and report.html, report.pdf and summary.html from the art-of-cicd skill's
// build_report.py (ART_OF_CICD=<skill dir>, default ~/.claude/skills/art-of-cicd; without it the
// JUnit files are still written). PDF needs Chrome (CHROME=<binary> to choose one). When
// GITHUB_STEP_SUMMARY is set, the verdict and the suites table are appended to it as markdown.
//
// Exit 1 when any suite failed, after the report is written. Exit 0 otherwise.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const coreModule = "github.com/danihrndzld/garmin-health-exporter/tauri-app/core/"

var allSuites = []string{"ids", "go", "ui", "e2e", "cargo"}

type suiteResult struct {
	name, note, junit string // junit: path, "" when the suite wrote none
	rc, flaky         int
	secs              float64
}

// sh runs cmd in dir, appends its output to log (stdout to its own file when given) and returns the exit code.
func sh(cmd []string, dir, log string, env []string, stdout string) int {
	lf, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 1
	}
	defer lf.Close()
	fmt.Fprintf(lf, "$ %s  (in %s)\n", strings.Join(cmd, " "), rel(dir))
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir, c.Stdout, c.Stderr = dir, lf, lf
	c.Env = append(os.Environ(), env...)
	if stdout != "" {
		f, err := os.Create(stdout)
		if err != nil {
			fmt.Fprintln(lf, err)
			return 1
		}
		defer f.Close()
		c.Stdout = f
	}
	return exitCode(c.Run(), lf)
}

// exitCode turns a Run error into an exit code; a command that could not start is 127.
func exitCode(err error, log *os.File) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	if log != nil {
		fmt.Fprintln(log, err)
	}
	return 127
}

func runSuite(name, d string) suiteResult {
	app, core := filepath.Join(root, "tauri-app"), filepath.Join(root, "tauri-app", "core")
	log := filepath.Join(d, name+".log")
	_ = os.WriteFile(log, nil, 0o644)
	t0 := time.Now()
	r := suiteResult{name: name}
	switch name {
	case "ids":
		r.rc = sh([]string{"bun", "scripts/test-ids.js"}, app, log, nil, "")
	case "go":
		r.rc = sh([]string{"go", "vet", "./..."}, core, log, nil, "")
		testJSON, cover := filepath.Join(d, "go-test.json"), filepath.Join(d, "go-cover.out")
		rc2 := sh([]string{"go", "test", "-count=1", "-json", "-coverprofile=" + cover, "./..."}, core, log, nil, testJSON)
		if r.rc == 0 {
			r.rc = rc2
		}
		lf, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644)
		if err := goJUnit(testJSON, filepath.Join(d, "go-junit.xml"), lf); err != nil {
			fmt.Fprintln(lf, "go-junit.xml:", err)
		}
		if _, err := os.Stat(cover); err == nil {
			if err := goCover(cover, filepath.Join(d, "go-coverage.json"), coreModule, lf); err != nil {
				fmt.Fprintln(lf, "go-coverage.json:", err)
			}
		}
		lf.Close()
		r.junit = filepath.Join(d, "go-junit.xml")
	case "ui":
		r.junit = filepath.Join(d, "ui-junit.xml")
		r.rc = sh([]string{"bun", "test", "ui/__tests__", "--reporter=junit", "--reporter-outfile=" + r.junit}, app, log, nil, "")
	case "e2e":
		r.junit = filepath.Join(d, "e2e-junit.xml")
		r.rc = sh([]string{"bunx", "playwright", "test"}, app, log, []string{"E2E_REPORT_DIR=" + d}, "")
		var e2e struct {
			Stats map[string]any `json:"stats"` // also holds startTime and duration
		}
		raw, err := os.ReadFile(filepath.Join(d, "e2e.json"))
		if err == nil {
			err = json.Unmarshal(raw, &e2e)
		}
		if err != nil || e2e.Stats == nil {
			r.note = "no e2e.json"
		} else {
			n := func(k string) int { f, _ := e2e.Stats[k].(float64); return int(f) }
			r.flaky = n("flaky")
			r.note = fmt.Sprintf("%d passed, %d flaky (passed on a retry), %d failed", n("expected"), n("flaky"), n("unexpected"))
		}
	case "cargo":
		r.rc = sh([]string{"bun", "run", "core"}, app, log, nil, "") // tauri-build needs binaries/garmin-export-core-<host>
		if r.rc == 0 {
			r.rc = sh([]string{"cargo", "test"}, filepath.Join(app, "src-tauri"), log, nil, "")
		}
	}
	r.secs = time.Since(t0).Seconds()
	if r.junit != "" {
		if _, err := os.Stat(r.junit); err != nil {
			r.junit = ""
		}
	}
	return r
}

func gitOut(args ...string) string {
	c := exec.Command("git", args...)
	c.Dir = root
	out, _ := c.Output()
	return strings.TrimSpace(string(out))
}

// The report.json shape art-of-cicd build_report.py reads; field order is the order on disk.
type report struct {
	Title    string    `json:"title"`
	Skill    string    `json:"skill"`
	Meta     meta      `json:"meta"`
	Summary  string    `json:"summary"`
	Verdict  verdict   `json:"verdict"`
	Sections []section `json:"sections"`
}

type meta struct {
	Date    string `json:"date"`
	Branch  string `json:"branch"`
	Commit  string `json:"commit"`
	Command string `json:"command"`
	Tools   string `json:"tools"`
}

type verdict struct {
	Status string `json:"status"`
	Text   string `json:"text"`
}

type section struct {
	Heading string   `json:"heading"`
	Text    string   `json:"text"`
	Table   table    `json:"table"`
	Callout *callout `json:"callout,omitempty"`
}

type table struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Numeric []int      `json:"numeric"`
}

type callout struct {
	Tone string `json:"tone"`
	Text string `json:"text"`
}

func buildReport(results []suiteResult, args []string) report {
	var failed []string
	var rows [][]string
	flaky, total := "", 0.0
	for _, r := range results {
		res, cell := "pass", "log only"
		if r.rc != 0 {
			failed = append(failed, r.name)
			res = fmt.Sprintf("fail (exit %d)", r.rc)
		}
		if r.note != "" {
			cell = r.note
		} else if r.junit != "" {
			cell = filepath.Base(r.junit)
		}
		rows = append(rows, []string{r.name, res, fmt.Sprintf("%.0f", r.secs), r.name + ".log", cell})
		if r.flaky != 0 && flaky == "" {
			flaky = r.note
		}
		total += r.secs
	}
	list := ""
	if len(failed) > 0 {
		list = ": " + strings.Join(failed, ", ")
	}
	v := verdict{"pass", "Every suite passed."}
	if len(failed) > 0 {
		v = verdict{"fail", "Suites failed: " + strings.Join(failed, ", ") + ". See the Failures section and the logs."}
	}
	s := section{
		Heading: "Suites",
		Text:    "<p>Order: fastest gate first. Budget: unit under 1 minute, the full run under 30.</p>",
		Table:   table{Columns: []string{"Suite", "Result", "Seconds", "Log", "Results"}, Rows: rows, Numeric: []int{2}},
	}
	if flaky != "" {
		s.Callout = &callout{"warn", "Playwright: " + flaky + ". A test that passed only on a retry is a flake to investigate, not a pass."}
	}
	return report{
		Title: "Garmin Export test run",
		Skill: "art-of-cicd",
		Meta: meta{
			Date:    time.Now().Format("2006-01-02"),
			Branch:  gitOut("rev-parse", "--abbrev-ref", "HEAD"),
			Commit:  gitOut("rev-parse", "--short", "HEAD"),
			Command: strings.TrimSpace("go run -C tools . test-report " + strings.Join(args, " ")),
			Tools:   "go test, bun test, Playwright (Chromium, WebKit), cargo test",
		},
		Summary: fmt.Sprintf("<p>%d suites ran in %.1f minutes; %d failed%s. Go coverage covers tauri-app/core statements. Cargo writes a log only.</p>",
			len(results), total/60, len(failed), list),
		Verdict:  v,
		Sections: []section{s},
	}
}

// stepSummary is the markdown GitHub shows on the run page: the verdict and the suites table.
func stepSummary(r report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s: %s\n\n%s\n\n", r.Title, r.Verdict.Status, r.Verdict.Text)
	t := r.Sections[0].Table
	fmt.Fprintf(&b, "| %s |\n|%s\n", strings.Join(t.Columns, " | "), strings.Repeat("---|", len(t.Columns)))
	for _, row := range t.Rows {
		fmt.Fprintf(&b, "| %s |\n", strings.Join(row, " | "))
	}
	return b.String()
}

func testReportMain(args []string) int {
	fs := flag.NewFlagSet("test-report", flag.ContinueOnError)
	suitesFlag := fs.String("suites", strings.Join(allSuites, ","), "comma separated subset of "+strings.Join(allSuites, ","))
	formats := fs.String("formats", "html,pdf,summary", "build_report.py formats")
	out := fs.String("out", "", "output dir (default tauri-app/reportes-pruebas/<date>-ci); relative to the repo root")
	if rc := parseFlags(fs, args); rc >= 0 {
		return rc
	}
	var suites, bad []string
	for _, s := range strings.Split(*suitesFlag, ",") {
		if s == "" {
			continue
		}
		suites = append(suites, s)
		if !slices.Contains(allSuites, s) {
			bad = append(bad, s)
		}
	}
	if len(bad) > 0 {
		fmt.Fprintln(os.Stderr, "unknown suites: "+pyList(bad))
		return 1
	}
	d := fromRoot(*out)
	if d == "" {
		d = filepath.Join(root, "tauri-app", "reportes-pruebas", time.Now().Format("2006-01-02")+"-ci")
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if real, err := filepath.EvalSymlinks(d); err == nil {
		d = real
	}

	var results []suiteResult
	for _, s := range suites {
		fmt.Printf("[test_report] %s ...\n", s)
		r := runSuite(s, d)
		results = append(results, r)
		status := "pass"
		if r.rc != 0 {
			status = "FAIL"
		}
		fmt.Printf("[test_report] %s: %s in %.0fs %s\n", s, status, r.secs, r.note)
	}

	rep := buildReport(results, args)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rep)
	if err := os.WriteFile(filepath.Join(d, "report.json"), bytes.TrimSuffix(buf.Bytes(), []byte("\n")), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if p := os.Getenv("GITHUB_STEP_SUMMARY"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(stepSummary(rep))
			f.Close()
		} else {
			fmt.Printf("[test_report] could not write GITHUB_STEP_SUMMARY: %v\n", err)
		}
	}

	skill := os.Getenv("ART_OF_CICD")
	if skill == "" {
		home, _ := os.UserHomeDir()
		skill = filepath.Join(home, ".claude", "skills", "art-of-cicd")
	}
	script := filepath.Join(skill, "scripts", "build_report.py")
	if _, err := os.Stat(script); err == nil {
		cmd := []string{"python3", script, filepath.Join(d, "report.json"), "--root", root, "--formats", *formats, "--ci"}
		for _, r := range results {
			if r.junit != "" {
				cmd = append(cmd, "--junit", r.junit)
			}
		}
		if _, err := os.Stat(filepath.Join(d, "go-coverage.json")); err == nil {
			cmd = append(cmd, "--coverage", filepath.Join(d, "go-coverage.json"))
		}
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir, c.Stdout, c.Stderr = root, os.Stdout, os.Stderr
		if rc := exitCode(c.Run(), os.Stderr); rc != 0 {
			fmt.Printf("[test_report] build_report.py exited %d (2 means no Chrome for the PDF; the HTML is written)\n", rc)
		}
	} else {
		fmt.Printf("[test_report] %s not found; set ART_OF_CICD. JUnit files are in %s\n", script, d)
	}
	fmt.Printf("[test_report] report: %s\n", d)
	for _, r := range results {
		if r.rc != 0 {
			return 1
		}
	}
	return 0
}
