// Command tools holds the repo's CI, test report, release and Personal API probe tools.
// Run it from the repo root:
//
//	go run -C tools . test-report [--suites ids,go,ui,e2e,cargo] [--formats html,pdf,summary] [--out DIR]
//	go run -C tools . ci [--stages ...] [--skip-*] [--no-open] [--here] [--dry-run] [--rustup-home D] [--cargo-home D]
//	go run -C tools . release --bump patch|minor|major | --version X.Y.Z [flags]
//	go run -C tools . probe [--end YYYY-MM-DD] [--days N]
//
// Relative paths in flags (--out, --notes-file) are read from the repo root, because
// `go run -C tools` starts the program in tools/.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// root is the repo root: the nearest directory, from the working directory up, that holds tauri-app/.
var root string

func main() {
	cmds := map[string]func([]string) int{"test-report": testReportMain, "ci": ciMain, "release": releaseMain, "probe": probeMain}
	if len(os.Args) < 2 || cmds[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: go run -C tools . <test-report|ci|release|probe> [flags]")
		os.Exit(2)
	}
	wd, _ := os.Getwd()
	var err error
	if root, err = findRoot(wd); err != nil {
		fmt.Fprintln(os.Stderr, "!! "+err.Error())
		os.Exit(2)
	}
	os.Exit(cmds[os.Args[1]](os.Args[2:]))
}

// findRoot walks up from start to the first directory that has a tauri-app/ directory.
func findRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, "tauri-app")); err == nil && fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("tauri-app/ not found in %s or any parent", start)
		}
		dir = parent
	}
}

// fromRoot makes a flag path absolute; a relative one is read from the repo root.
func fromRoot(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// rel is p relative to the repo root ("." for the root), or p itself when it is outside.
func rel(p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, "../") {
		return p
	}
	return filepath.ToSlash(r)
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shjoin quotes like Python's shlex.join, so printed commands paste into a shell.
func shjoin(cmd []string) string {
	out := make([]string, len(cmd))
	for i, s := range cmd {
		switch {
		case s == "":
			out[i] = "''"
		case shellSafe.MatchString(s):
			out[i] = s
		default:
			out[i] = "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
		}
	}
	return strings.Join(out, " ")
}

// pyList prints a list of strings the way Python's repr does: ['a', 'b'].
func pyList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "'" + x + "'"
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// parseFlags parses args and refuses positional arguments. It returns -1 to go on, or the
// exit code: 0 after -h, 2 on a bad flag.
func parseFlags(fs *flag.FlagSet, args []string) int {
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unrecognized arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	return -1
}
