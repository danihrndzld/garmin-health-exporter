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

// allowedRequires lists the modules go.mod may require: only the extracted gconnect library
// (spec section 2.5), whose gconnecttest package lives in the same module.
var allowedRequires = map[string]bool{"github.com/danihrndzld/gconnect": true}

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

// hostNames are the os and syscall members that read the host's environment, files or streams
// in other ways. Only cmd/ may use them; internal/guard may also call UserHomeDir, which is how
// it resolves $HOME for the trust boundary.
var hostNames = map[string]bool{
	"UserCacheDir": true, "UserConfigDir": true, "TempDir": true, "NewFile": true, "Hostname": true,
	"Getwd": true, "Executable": true, "UserHomeDir": true,
}

// wirePackages may not hold map[string]any in an exported signature or type: the wire shape belongs to cmd.
var wirePackages = []string{"internal/garmin", "internal/mfp", "gconnect"}

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
	pkgs := map[string][]*ast.File{} // non-test files of the wire packages, by dir
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
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		checkFile(rel, f, modPrefix, report)
		dir := path.Dir(rel)
		for _, w := range wirePackages {
			if under(dir, w) && !strings.HasSuffix(rel, "_test.go") {
				pkgs[dir] = append(pkgs[dir], f)
			}
		}
		return nil
	})
	for dir, files := range pkgs {
		checkWire(dir, files, report)
	}
	return bad, err
}

func under(dir, pkg string) bool { return dir == pkg || strings.HasPrefix(dir, pkg+"/") }

func checkFile(rel string, f *ast.File, modPrefix string, report func(string, ...any)) {
	dir, isTest := path.Dir(rel), strings.HasSuffix(rel, "_test.go")
	inCmd, inLib := under(dir, "cmd"), under(dir, "gconnect")
	envPkgs := map[string]bool{} // local names bound to os or syscall in this file
	fmtPkgs := map[string]bool{} // local names bound to fmt in this file
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
		if p == "fmt" {
			fmtPkgs[name] = true
		}
		switch {
		case p == "C":
			report("%s: cgo (import \"C\") is not allowed", rel)
		case under(local, "cmd") && local != p && !inCmd:
			report("%s: imports %s; nothing outside cmd/ may import the driving adapter", rel, p)
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
		case p == "unsafe" && !inCmd && !isTest:
			report("%s: unsafe outside cmd/", rel)
		case (p == "log" || strings.HasPrefix(p, "log/")) && !inCmd && !isTest:
			report("%s: imports %s, which writes to the standard streams; only cmd/ may", rel, p)
		}
	}
	if isTest {
		return
	}
	for _, g := range f.Comments {
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, "//go:linkname") {
				report("%s: a //go:linkname directive reaches into another package", rel)
			}
		}
	}
	if inCmd {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			x, ok := s.X.(*ast.Ident)
			name := s.Sel.Name
			sanctioned := name == "UserHomeDir" && dir == "internal/guard"
			if ok && envPkgs[x.Name] && (envNames[name] || hostNames[name] && !sanctioned) {
				report("%s: %s.%s outside cmd/", rel, x.Name, name)
			}
			// stdout is the JSON line protocol: a stray Println corrupts it.
			if ok && fmtPkgs[x.Name] && (name == "Print" || name == "Printf" || name == "Println") {
				report("%s: %s.%s writes to the standard streams outside cmd/", rel, x.Name, name)
			}
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
				report("%s: the %s builtin writes to the standard streams outside cmd/", rel, id.Name)
			}
		}
		return true
	})
}

// checkWire reports exported funcs, methods and types of one wire package that hold
// map[string]any, also behind a type of the same package (alias, defined type or struct field).
func checkWire(dir string, files []*ast.File, report func(string, ...any)) {
	types := map[string]ast.Expr{}
	for _, f := range files {
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
				for _, sp := range g.Specs {
					ts := sp.(*ast.TypeSpec)
					types[ts.Name.Name] = ts.Type
				}
			}
		}
	}
	var holds func(e ast.Expr, seen map[string]bool) bool
	holds = func(e ast.Expr, seen map[string]bool) bool {
		found := hasWireMap(e)
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && types[id.Name] != nil && !seen[id.Name] {
				seen[id.Name] = true
				found = holds(types[id.Name], seen) || found
			}
			return true
		})
		return found
	}
	for name, t := range types {
		if ast.IsExported(name) && holds(t, map[string]bool{name: true}) {
			report("%s: type %s holds map[string]any; the wire shape belongs to cmd", dir, name)
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			for _, list := range []*ast.FieldList{fn.Type.Params, fn.Type.Results} {
				if list == nil {
					continue
				}
				for _, field := range list.List {
					if holds(field.Type, map[string]bool{}) {
						report("%s: %s takes or returns map[string]any; the wire shape belongs to cmd", dir, fn.Name.Name)
					}
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
		{"CL-273 nothing outside cmd imports a package under cmd/", "may import the driving adapter", []fixture{
			{"internal/mfp imports a cmd helper", map[string]string{"internal/mfp/a.go": src("mfp", `"`+fake+`cmd/envutil"`, `var _ = envutil.Get("MFP_FAKE_SERVER")`)}},
			{"a test file in internal/garmin imports cmd", map[string]string{"internal/garmin/a_test.go": src("garmin", `m "`+fake+`cmd/app"`, "var _ = m.Run")}},
			{"gconnect imports cmd", map[string]string{"gconnect/a.go": src("gconnect", `"`+fake+`cmd/app"`, "var _ = app.Run")}},
			{"internal/guard imports cmd", map[string]string{"internal/guard/a.go": src("guard", `"`+fake+`cmd"`, "var _ = cmd.X")}},
		}},
		{"CL-274 map[string]any cannot hide behind a named type of the inside", "map[string]any", []fixture{
			{"an alias in a signature", map[string]string{"internal/mfp/a.go": "package mfp\n\ntype Wire = map[string]any\n", "internal/mfp/b.go": "package mfp\n\nfunc Probe(w Wire) Wire { return w }\n"}},
			{"an unexported alias in a signature", map[string]string{"internal/mfp/a.go": "package mfp\n\ntype wire = map[string]any\n\nfunc Probe(w wire) {}\n"}},
			{"a defined type", map[string]string{"internal/garmin/a.go": "package garmin\n\ntype Wire map[string]interface{}\n"}},
			{"a field of an exported struct", map[string]string{"internal/mfp/a.go": "package mfp\n\ntype ExportRequest struct{ Extra map[string]any }\n"}},
			{"a chain of unexported types behind an exported func", map[string]string{"gconnect/a.go": "package gconnect\n\ntype raw = map[string]any\ntype box struct{ r []raw }\n\nfunc Open() *box { return nil }\n"}},
			{"a method on an exported type", map[string]string{"internal/garmin/a.go": "package garmin\n\ntype opts struct{ m map[string]any }\ntype C struct{}\n\nfunc (C) Set(o opts) {}\n"}},
		}},
		{"CL-275 the other host reads of os and syscall happen only in cmd, and UserHomeDir only in guard", "outside cmd/", func() []fixture {
			var out []fixture
			// Listed here, not read from hostNames: a name dropped from that map must fail this test.
			for _, n := range []string{"UserCacheDir", "UserConfigDir", "TempDir", "NewFile", "Hostname", "Getwd", "Executable"} {
				out = append(out, fixture{"os." + n, map[string]string{"internal/garmin/a.go": src("garmin", `"os"`, "var _ = os."+n)}})
			}
			return append(out,
				fixture{"os.UserHomeDir outside guard", map[string]string{"internal/mfp/a.go": src("mfp", `"os"`, "var _ = os.UserHomeDir")}},
				fixture{"os.UserCacheDir under an alias", map[string]string{"internal/mfp/a.go": src("mfp", `o "os"`, "var _ = o.UserCacheDir")}},
				fixture{"syscall.Getwd", map[string]string{"internal/mfp/a.go": src("mfp", `"syscall"`, "var _ = syscall.Getwd")}},
			)
		}()},
		{"CL-276 cgo is not allowed anywhere in the module", "cgo", []fixture{
			{"internal/mfp", map[string]string{"internal/mfp/a.go": src("mfp", `"C"`, "var _ = C.getenv")}},
			{"gconnect", map[string]string{"gconnect/a.go": src("gconnect", `"C"`, "var _ = C.getenv")}},
			{"cmd", map[string]string{"cmd/app/main.go": src("main", `"C"`, "var _ = C.getenv")}},
			{"a test file", map[string]string{"internal/garmin/a_test.go": src("garmin", `"C"`, "var _ = C.getenv")}},
		}},
		{"CL-277 no //go:linkname directive in a non-test file", "go:linkname", []fixture{
			{"internal/garmin", map[string]string{"internal/garmin/a.go": "package garmin\n\nimport _ \"unsafe\"\n\n//go:linkname getenv syscall.Getenv\nfunc getenv(string) (string, bool)\n"}},
			{"cmd", map[string]string{"cmd/app/main.go": "package main\n\n//go:linkname getenv syscall.Getenv\nfunc getenv(string) (string, bool)\n"}},
		}},
		{"CL-278 unsafe is imported only in cmd", "unsafe outside cmd/", []fixture{
			{"internal/mfp", map[string]string{"internal/mfp/a.go": src("mfp", `_ "unsafe"`, "")}},
			{"internal/guard under an alias", map[string]string{"internal/guard/a.go": src("guard", `u "unsafe"`, "var _ = u.Pointer(nil)")}},
		}},
		{"CL-279 nothing outside cmd writes to stdout or stderr without naming os", "the standard streams", []fixture{
			{"fmt.Print", map[string]string{"internal/garmin/a.go": src("garmin", `"fmt"`, `func f() { fmt.Print("x") }`)}},
			{"fmt.Printf", map[string]string{"internal/mfp/a.go": src("mfp", `"fmt"`, `func f() { fmt.Printf("%d", 1) }`)}},
			{"fmt.Println", map[string]string{"gconnect/a.go": src("gconnect", `"fmt"`, `func f() { fmt.Println("x") }`)}},
			{"fmt.Println under an alias", map[string]string{"internal/guard/a.go": src("guard", `f "fmt"`, `var p = f.Println`)}},
			{"an import of log", map[string]string{"internal/garmin/a.go": src("garmin", `"log"`, `func f() { log.Print("x") }`)}},
			{"an import of log/slog", map[string]string{"internal/mfp/a.go": src("mfp", `"log/slog"`, `func f() { slog.Info("x") }`)}},
			{"the print builtin", map[string]string{"internal/garmin/a.go": "package garmin\n\nfunc f() { print(\"x\") }\n"}},
			{"the println builtin", map[string]string{"gconnect/a.go": "package gconnect\n\nfunc f() { println(\"x\") }\n"}},
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
		// It also pins the sanctioned cases: os.UserHomeDir in internal/guard, unsafe in cmd, linkname
		// and unsafe in a test file, and an unexported map alias that no exported name uses.
		bad := run(t, map[string]string{
			"cmd/app/main.go":            src("main", `"os"`+"\n"+`"`+fake+`internal/protocol"`+"\n"+`"`+fake+`internal/mfp"`, `var _ = os.Getenv("X")`+"\nvar _ protocol.Emitter\nvar _ = mfp.Export\nfunc decode(map[string]any) {}"),
			"internal/mfp/a.go":          src("mfp", `"os"`+"\n"+`"`+fake+`internal/guard"`, "var _ = os.ReadFile\nvar _ = guard.InsideHome\nfunc export(req map[string]any) {}"),
			"internal/mfp/a_test.go":     src("mfp", `"os"`, `var _ = os.Getenv("HOME")`),
			"internal/garmin/a.go":       src("garmin", `"`+fake+`gconnect"`+"\n"+`"`+fake+`internal/atomicfile"`, "var _ gconnect.Hosts\nvar _ = atomicfile.WriteFile"),
			"gconnect/a.go":              src("gconnect", `"net/http"`, "var _ http.Header"),
			"gconnect/gconnecttest/a.go": src("gconnecttest", `"`+fake+`gconnect"`, "var _ gconnect.Hosts"),
			"internal/guard/a.go":        src("guard", `"os"`, "var _ = os.UserHomeDir"),
			"internal/mfp/testdata/x.go": src("x", `"os"`, `var _ = os.Getenv("X")`),
			"cmd/app/unsafe.go":          src("main", `_ "unsafe"`, ""),
			"internal/mfp/b_test.go":     "package mfp\n\nimport _ \"unsafe\"\n\n//go:linkname now time.now\nfunc now() (int64, int32, int64)\n",
			"gconnect/gconnecttest/b.go": "package gconnecttest\n\ntype obj = map[string]any\n\nfunc send(o obj) {}\n",
			"cmd/app/print.go":           src("main", `"fmt"`+"\n"+`"log"`, `func f() { fmt.Println("x"); log.Print("x"); println("x") }`),
			"internal/garmin/b_test.go":  src("garmin", `"fmt"`+"\n"+`"log"`, `func f() { fmt.Println("x"); log.Print("x"); println("x") }`),
			"internal/garmin/b.go":       src("garmin", `"fmt"`, `var _ = fmt.Sprintf("%d", 1)`),
		})
		if len(bad) != 0 {
			t.Fatalf("the clean fixture failed: %v", bad)
		}
	})
}
