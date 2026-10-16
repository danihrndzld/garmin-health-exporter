package core

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

const mod = "github.com/danihrndzld/garmin-health-exporter/tauri-app/core/"

// allowedRequires lists the modules go.mod may require. It is empty: the gconnect extraction
// (spec section 2.5) adds exactly one, github.com/danihrndzld/gconnect, and must add it here.
var allowedRequires = map[string]bool{}

// envNames are the os and syscall members that read or change the process environment or
// its standard streams. Only cmd/ may use them.
var envNames = map[string]bool{
	"Getenv": true, "LookupEnv": true, "Environ": true, "Args": true, "Stdin": true, "Stdout": true,
	"Stderr": true, "ExpandEnv": true, "Expand": true, "Setenv": true, "Unsetenv": true,
}

// diskImports are the packages gconnect must not import. Without them the library cannot open,
// write or delete a file, which is how section 1.7's "no writes from gconnect" is enforced.
var diskImports = map[string]bool{
	"os": true, "os/exec": true, "os/signal": true, "path/filepath": true, "io/fs": true,
	"io/ioutil": true, "syscall": true, "unsafe": true,
}

var leafPackages = map[string]bool{"internal/jsjson": true, "internal/atomicfile": true, "internal/guard": true, "internal/protocol": true}

func isStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// checkTree returns every violation of the rules in spec section 1.7 under root, a module
// whose import path prefix is modPrefix (ending in "/").
func checkTree(root, modPrefix string) ([]string, error) {
	var bad []string
	report := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }
	if err := checkGoMod(filepath.Join(root, "go.mod"), report); err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		checkFile(rel, f, modPrefix, report)
		return nil
	})
	return bad, err
}

func under(dir, pkg string) bool { return dir == pkg || strings.HasPrefix(dir, pkg+"/") }

func checkFile(rel string, f *ast.File, modPrefix string, report func(string, ...any)) {
	dir, isTest := path.Dir(rel), strings.HasSuffix(rel, "_test.go")
	inCmd, inLib := under(dir, "cmd"), under(dir, "gconnect")
	envPkgs := map[string]bool{} // local names bound to os or syscall in this file
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		local := strings.TrimPrefix(p, modPrefix)
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if p == "os" || p == "syscall" {
			envPkgs[name] = true
			if name == "." && !inCmd && !isTest {
				report("%s: dot import of %s outside cmd/", rel, p)
			}
		}
		switch {
		case inLib && !isStd(p) && !under(local, "gconnect"):
			report("%s: the library imports %s; gconnect is standard library only", rel, p)
		case inLib && !isTest && diskImports[p]:
			report("%s: the library must not touch the disk or the process (%s)", rel, p)
		case local == "internal/protocol" && !inCmd && dir != "internal/protocol":
			report("%s: only cmd imports internal/protocol", rel)
		case under(dir, "internal/garmin") && under(local, "internal/mfp"),
			under(dir, "internal/mfp") && under(local, "internal/garmin"):
			report("%s: internal/garmin and internal/mfp must not import each other", rel)
		case leafPackages[dir] && !isStd(p) && !isTest:
			report("%s: %s is a leaf package; it imports %s", rel, dir, p)
		case p == "io/ioutil" && !inCmd && !isTest:
			report("%s: io/ioutil outside cmd/", rel)
		}
	}
	if inCmd || isTest {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := s.X.(*ast.Ident); ok && envPkgs[x.Name] && envNames[s.Sel.Name] {
				report("%s: %s.%s outside cmd/", rel, x.Name, s.Sel.Name)
			}
		}
		return true
	})
	if !under(dir, "internal/garmin") && !under(dir, "internal/mfp") && !inLib {
		return
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !fn.Name.IsExported() {
			continue
		}
		for _, list := range []*ast.FieldList{fn.Type.Params, fn.Type.Results} {
			if list == nil {
				continue
			}
			for _, field := range list.List {
				if hasWireMap(field.Type) {
					report("%s: %s takes or returns map[string]any; the wire shape belongs to cmd", rel, fn.Name.Name)
				}
			}
		}
	}
}

// hasWireMap reports whether t is or contains map[string]any or map[string]interface{}.
func hasWireMap(t ast.Expr) bool {
	found := false
	ast.Inspect(t, func(n ast.Node) bool {
		m, ok := n.(*ast.MapType)
		if !ok {
			return true
		}
		key, _ := m.Key.(*ast.Ident)
		if key == nil || key.Name != "string" {
			return true
		}
		switch v := m.Value.(type) {
		case *ast.Ident:
			found = found || v.Name == "any"
		case *ast.InterfaceType:
			found = found || len(v.Methods.List) == 0
		}
		return true
	})
	return found
}

// checkGoMod scans go.mod line by line: no replace directive, no require outside allowedRequires.
func checkGoMod(file string, report func(string, ...any)) error {
	fh, err := os.Open(file)
	if err != nil {
		return err
	}
	defer fh.Close()
	block := ""
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "//")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if block != "" {
			if fields[0] == ")" {
				block = ""
				continue
			}
			fields = append([]string{block}, fields...)
		} else if len(fields) == 2 && fields[1] == "(" {
			block = fields[0]
			continue
		}
		switch fields[0] {
		case "replace":
			report("go.mod: replace directive %q", strings.Join(fields[1:], " "))
		case "require":
			if len(fields) > 1 && !allowedRequires[fields[1]] {
				report("go.mod: requires %s; only the standard library is allowed", fields[1])
			}
		}
	}
	return sc.Err()
}

// TestArchitectureRules runs the checker on this module.
func TestArchitectureRules(t *testing.T) {
	bad, err := checkTree(".", mod)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Error(b)
	}
}

// TestArchitectureCheckerCatchesEachRule proves each rule can fail: every fixture is a small
// module that breaks one rule, and the checker must report it.
func TestArchitectureCheckerCatchesEachRule(t *testing.T) {
	const fake = "example.com/fake/"
	const goMod = "module example.com/fake\n\ngo 1.24\n"
	run := func(t *testing.T, files map[string]string) []string {
		t.Helper()
		root := t.TempDir()
		if _, ok := files["go.mod"]; !ok {
			files["go.mod"] = goMod
		}
		for name, body := range files {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		bad, err := checkTree(root, fake)
		if err != nil {
			t.Fatal(err)
		}
		return bad
	}
	src := func(pkg, imports, body string) string {
		return "package " + pkg + "\n\nimport (\n" + imports + "\n)\n\n" + body + "\n"
	}
	type fixture struct {
		name  string
		files map[string]string
	}
	cases := []struct {
		id, want string // want: a part of the violation that rule reports
		fixtures []fixture
	}{
		{"CL-261 gconnect imports nothing outside the standard library", "standard library only", []fixture{
			{"third party", map[string]string{"gconnect/a.go": src("gconnect", `"golang.org/x/net/html"`, "var _ = html.Parse")}},
			{"an app package", map[string]string{"gconnect/a.go": src("gconnect", `"`+fake+`internal/guard"`, "var _ = guard.InsideHome")}},
			{"in a test file", map[string]string{"gconnect/a_test.go": src("gconnect", `"golang.org/x/net/html"`, "var _ = html.Parse")}},
		}},
		{"CL-262 gconnect imports no package that reaches the disk or the process", "must not touch the disk", func() []fixture {
			var out []fixture
			// Listed here, not read from diskImports: a name dropped from that map must fail this test.
			for _, p := range []string{"os", "os/exec", "os/signal", "path/filepath", "io/fs", "io/ioutil", "syscall", "unsafe"} {
				out = append(out, fixture{p, map[string]string{"gconnect/a.go": src("gconnect", `_ "`+p+`"`, "")}})
			}
			out = append(out, fixture{"os under an alias", map[string]string{"gconnect/gconnecttest/a.go": src("gconnecttest", `xos "os"`, "var _ = xos.WriteFile")}})
			return out
		}()},
		{"CL-263 only cmd imports internal/protocol", "only cmd imports internal/protocol", []fixture{
			{"internal/mfp", map[string]string{"internal/mfp/a.go": src("mfp", `"`+fake+`internal/protocol"`, "var _ protocol.Emitter")}},
			{"a test file in internal/garmin, under an alias", map[string]string{"internal/garmin/a_test.go": src("garmin", `p "`+fake+`internal/protocol"`, "var _ p.Emitter")}},
		}},
		{"CL-264 internal/garmin and internal/mfp do not import each other", "must not import each other", []fixture{
			{"garmin to mfp", map[string]string{"internal/garmin/a.go": src("garmin", `"`+fake+`internal/mfp"`, "var _ = mfp.Export")}},
			{"mfp to garmin", map[string]string{"internal/mfp/a.go": src("mfp", `"`+fake+`internal/garmin"`, "var _ = garmin.Connect")}},
			{"a subpackage of mfp to garmin", map[string]string{"internal/mfp/mfptest/a.go": src("mfptest", `"`+fake+`internal/garmin"`, "var _ = garmin.Connect")}},
		}},
		{"CL-265 a leaf package imports the standard library only", "is a leaf package", []fixture{
			{"guard", map[string]string{"internal/guard/a.go": src("guard", `"`+fake+`gconnect"`, "var _ gconnect.Hosts")}},
			{"atomicfile", map[string]string{"internal/atomicfile/a.go": src("atomicfile", `"golang.org/x/sys/unix"`, "var _ = unix.Fsync")}},
		}},
		{"CL-266 the environment and the standard streams are read only in cmd", "outside cmd/", func() []fixture {
			var out []fixture
			for _, n := range []string{"Getenv", "LookupEnv", "Environ", "Args", "Stdin", "Stdout", "Stderr", "ExpandEnv", "Expand", "Setenv", "Unsetenv"} {
				out = append(out, fixture{"os." + n, map[string]string{"internal/mfp/a.go": src("mfp", `"os"`, "var _ = os."+n)}})
			}
			return append(out,
				fixture{"os under an alias", map[string]string{"internal/mfp/a.go": src("mfp", `xos "os"`, `var _ = xos.Getenv("MFP_FAKE_SERVER")`)}},
				fixture{"a dot import of os", map[string]string{"internal/mfp/a.go": src("mfp", `. "os"`, `var _ = LookupEnv`)}},
				fixture{"syscall.Getenv", map[string]string{"gconnect/a.go": src("gconnect", `"syscall"`, `var _, _ = syscall.Getenv("X")`)}},
				fixture{"syscall under an alias", map[string]string{"internal/garmin/a.go": src("garmin", `sc "syscall"`, `var _ = sc.Environ`)}},
				fixture{"a dot import of syscall", map[string]string{"internal/garmin/a.go": src("garmin", `. "syscall"`, `var _ = Environ`)}},
				fixture{"a method value", map[string]string{"internal/guard/a.go": src("guard", `"os"`, `var f = os.Getenv`)}},
			)
		}()},
		{"CL-267 io/ioutil is not used outside cmd", "io/ioutil outside cmd/", []fixture{
			{"internal/mfp", map[string]string{"internal/mfp/a.go": src("mfp", `"io/ioutil"`, `var _ = ioutil.WriteFile`)}},
			{"internal/garmin under an alias", map[string]string{"internal/garmin/a.go": src("garmin", `iu "io/ioutil"`, `var _ = iu.ReadFile`)}},
		}},
		{"CL-268 no exported function of the inside takes or returns map[string]any", "takes or returns map[string]any", []fixture{
			{"a parameter", map[string]string{"internal/mfp/a.go": src("mfp", `"context"`, "func Export(ctx context.Context, req map[string]any) {}")}},
			{"a result", map[string]string{"internal/garmin/a.go": src("garmin", `"errors"`, "var _ = errors.New\nfunc Status() map[string]interface{} { return nil }")}},
			{"a method", map[string]string{"gconnect/a.go": src("gconnect", `"errors"`, "var _ = errors.New\ntype A struct{}\nfunc (A) Do(x []map[string]any) {}")}},
			{"a func parameter", map[string]string{"internal/mfp/a.go": src("mfp", `"errors"`, "var _ = errors.New\nfunc Run(f func(map[string]any)) {}")}},
		}},
		{"CL-269 go.mod has no replace directive", "replace directive", []fixture{
			{"one line", map[string]string{"go.mod": goMod + "replace example.com/x => ../x\n"}},
			{"a block", map[string]string{"go.mod": goMod + "replace (\n\texample.com/x => ../x\n)\n"}},
		}},
		{"CL-270 go.mod requires no module outside the allowlist", "only the standard library is allowed", []fixture{
			{"one line", map[string]string{"go.mod": goMod + "require example.com/x v1.0.0\n"}},
			{"a block", map[string]string{"go.mod": goMod + "require (\n\texample.com/x v1.0.0 // indirect\n)\n"}},
		}},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			for _, fx := range c.fixtures {
				bad := run(t, fx.files)
				if !strings.Contains(strings.Join(bad, "\n"), c.want) {
					t.Errorf("fixture %q breaks the rule, but the checker reported %q", fx.name, bad)
				}
			}
		})
	}
	t.Run("CL-271 a clean tree passes: cmd and test files may read the environment, and the allowed edges are allowed", func(t *testing.T) {
		bad := run(t, map[string]string{
			"cmd/app/main.go":            src("main", `"os"`+"\n"+`"`+fake+`internal/protocol"`+"\n"+`"`+fake+`internal/mfp"`, `var _ = os.Getenv("X")`+"\nvar _ protocol.Emitter\nvar _ = mfp.Export\nfunc decode(map[string]any) {}"),
			"internal/mfp/a.go":          src("mfp", `"os"`+"\n"+`"`+fake+`internal/guard"`, "var _ = os.ReadFile\nvar _ = guard.InsideHome\nfunc export(req map[string]any) {}"),
			"internal/mfp/a_test.go":     src("mfp", `"os"`, `var _ = os.Getenv("HOME")`),
			"internal/garmin/a.go":       src("garmin", `"`+fake+`gconnect"`+"\n"+`"`+fake+`internal/atomicfile"`, "var _ gconnect.Hosts\nvar _ = atomicfile.WriteFile"),
			"gconnect/a.go":              src("gconnect", `"net/http"`, "var _ http.Header"),
			"gconnect/gconnecttest/a.go": src("gconnecttest", `"`+fake+`gconnect"`, "var _ gconnect.Hosts"),
			"internal/guard/a.go":        src("guard", `"os"`, "var _ = os.UserHomeDir"),
			"internal/mfp/testdata/x.go": src("x", `"os"`, `var _ = os.Getenv("X")`),
		})
		if len(bad) != 0 {
			t.Fatalf("the clean fixture failed: %v", bad)
		}
	})
}
