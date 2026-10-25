package main

// Turns `go test -json` output into JUnit XML and a Go cover profile into an istanbul
// json-summary, the inputs art-of-cicd build_report.py reads (--junit, --coverage).
// Ported from github.com/danihrndzld/gconnect scripts/report/go_to_report_inputs.py (MIT, same
// author). The output is byte for byte what that script wrote with Python's ElementTree and
// json.dump, so old and new reports compare.
//
// JUnit holds leaf tests only: a test with subtests is a group, not a case. Go measures
// statements, not branches, so every branch count is 0 of 0.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

type testKey struct{ pkg, name string }

type testResult struct {
	action  string
	elapsed float64
}

// goJUnit converts src (go test -json) to dst (JUnit XML) and writes "<n> leaf tests" to log.
func goJUnit(src, dst string, log io.Writer) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	tests, out := map[testKey]testResult{}, map[testKey][]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e struct {
			Action, Package, Test, Output string
			Elapsed                       float64
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Test == "" {
			continue
		}
		k := testKey{e.Package, e.Test}
		switch e.Action {
		case "output":
			out[k] = append(out[k], e.Output)
		case "pass", "fail", "skip":
			tests[k] = testResult{e.Action, e.Elapsed}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	parents := map[testKey]bool{}
	keys := make([]testKey, 0, len(tests))
	for k := range tests {
		keys = append(keys, k)
		if i := strings.LastIndex(k.name, "/"); i >= 0 {
			parents[testKey{k.pkg, k.name[:i]}] = true
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pkg != keys[j].pkg {
			return keys[i].pkg < keys[j].pkg
		}
		return keys[i].name < keys[j].name
	})
	var pkgs []string
	byPkg := map[string][]testKey{}
	for _, k := range keys {
		if parents[k] || k.name == "TestMain" {
			continue
		}
		if byPkg[k.pkg] == nil {
			pkgs = append(pkgs, k.pkg)
		}
		byPkg[k.pkg] = append(byPkg[k.pkg], k)
	}

	var b strings.Builder
	b.WriteString("<?xml version='1.0' encoding='utf-8'?>\n")
	if len(pkgs) == 0 {
		b.WriteString("<testsuites />")
	} else {
		b.WriteString("<testsuites>")
	}
	total := 0
	for _, pkg := range pkgs {
		cases := byPkg[pkg]
		total += len(cases)
		failures := 0
		for _, k := range cases {
			if tests[k].action == "fail" {
				failures++
			}
		}
		fmt.Fprintf(&b, `<testsuite name="%s" tests="%d" failures="%d">`, xmlAttr(pkg), len(cases), failures)
		for _, k := range cases {
			r := tests[k]
			fmt.Fprintf(&b, `<testcase classname="%s" name="%s" time="%s"`, xmlAttr(pkg), xmlAttr(k.name), strconv.FormatFloat(r.elapsed, 'g', -1, 64))
			switch r.action {
			case "fail":
				if text := strings.Join(out[k], ""); text != "" {
					fmt.Fprintf(&b, `><failure message="fail">%s</failure></testcase>`, xmlText(text))
				} else {
					b.WriteString(`><failure message="fail" /></testcase>`)
				}
			case "skip":
				b.WriteString("><skipped /></testcase>")
			default:
				b.WriteString(" />")
			}
		}
		b.WriteString("</testsuite>")
	}
	if len(pkgs) > 0 {
		b.WriteString("</testsuites>")
	}
	if err := os.WriteFile(dst, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(log, "%d leaf tests\n", total)
	return nil
}

// xmlText and xmlAttr escape the way ElementTree does (not encoding/xml, which writes &#xA;).
var (
	xmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	xmlAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\r", "&#13;", "\n", "&#10;", "\t", "&#09;").Replace
)

// goCover converts src (a Go cover profile) to dst (istanbul json-summary), with prefix cut
// from every file name, and writes one line per file to log.
func goCover(src, dst, prefix string, log io.Writer) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	type block struct{ n, count int }
	blocks := map[string]block{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "mode:") || strings.TrimSpace(line) == "" {
			continue
		}
		// "file.go:1.2,3.4 n count", split from the right like Python's rsplit(" ", 2).
		i := strings.LastIndex(line, " ")
		j := strings.LastIndex(line[:max(i, 0)], " ")
		if j < 0 {
			return fmt.Errorf("bad cover line: %q", line)
		}
		n, err1 := strconv.Atoi(line[j+1 : i])
		count, err2 := strconv.Atoi(strings.TrimSpace(line[i+1:]))
		if err1 != nil || err2 != nil {
			return fmt.Errorf("bad cover line: %q", line)
		}
		loc := line[:j]
		blocks[loc] = block{n, max(blocks[loc].count, count)}
	}
	files := map[string]*[2]int{} // covered, total
	var names []string
	for loc, b := range blocks {
		name := strings.TrimPrefix(strings.SplitN(loc, ":", 2)[0], prefix)
		if files[name] == nil {
			files[name] = &[2]int{}
			names = append(names, name)
		}
		files[name][1] += b.n
		if b.count > 0 {
			files[name][0] += b.n
		}
	}
	sort.Strings(names)
	var cov, tot int
	for _, v := range files {
		cov, tot = cov+v[0], tot+v[1]
	}
	names = append(names, "total")
	files["total"] = &[2]int{cov, tot}

	var b strings.Builder
	b.WriteString("{")
	for i, name := range names {
		v := files[name]
		if i > 0 {
			b.WriteString(",")
		}
		pct := pyPct(v[0], v[1])
		fmt.Fprintf(&b, "\n %s: {", jsonString(name))
		for j, kind := range []string{"statements", "branches", "lines", "functions"} {
			t, c, p := v[1], v[0], pct
			if kind == "branches" || kind == "functions" {
				t, c, p = 0, 0, "100"
			}
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "\n  %q: {\n   \"total\": %d,\n   \"covered\": %d,\n   \"pct\": %s\n  }", kind, t, c, p)
		}
		b.WriteString("\n }")
	}
	b.WriteString("\n}")
	if err := os.WriteFile(dst, []byte(b.String()), 0o644); err != nil {
		return err
	}
	for _, name := range names {
		v := files[name]
		p, _ := strconv.ParseFloat(pyPct(v[0], v[1]), 64)
		fmt.Fprintf(log, "%-32s %4d/%-4d %6.2f%%\n", name, v[0], v[1], p)
	}
	return nil
}

// pyPct is Python's round(100 * cov / tot, 2) as json.dump prints it: 3.45, 100.0, or the
// integer 100 when there is nothing to cover.
func pyPct(cov, tot int) string {
	if tot == 0 {
		return "100"
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(100*float64(cov)/float64(tot), 'f', 2, 64), 64)
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// jsonString encodes s as a JSON string without HTML escaping.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
