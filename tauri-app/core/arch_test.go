package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const mod = "github.com/danihrndzld/garmin-health-exporter/tauri-app/core/"

func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// TestArchitectureRules fails on the first import edge that section 1.2 forbids.
func TestArchitectureRules(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		dir, isTest := filepath.ToSlash(filepath.Dir(path)), strings.HasSuffix(path, "_test.go")
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			local := strings.TrimPrefix(p, mod)
			switch {
			case strings.HasPrefix(dir, "gconnect") && !isStd(p) && !strings.HasPrefix(p, mod+"gconnect"):
				t.Errorf("%s: the library imports %s; gconnect is standard library only", path, p)
			case strings.HasPrefix(dir, "gconnect") && (p == "os" || p == "os/exec" || p == "path/filepath" || p == "io/fs"):
				t.Errorf("%s: the library must not touch the disk (%s)", path, p)
			case local == "internal/protocol" && !strings.HasPrefix(dir, "cmd/") && dir != "internal/protocol":
				t.Errorf("%s: only cmd imports internal/protocol", path)
			case strings.HasPrefix(dir, "internal/garmin") && local == "internal/mfp",
				strings.HasPrefix(dir, "internal/mfp") && local == "internal/garmin":
				t.Errorf("%s: internal/garmin and internal/mfp must not import each other", path)
			case (dir == "internal/jsjson" || dir == "internal/atomicfile" || dir == "internal/guard" || dir == "internal/protocol") && !isStd(p) && !isTest:
				t.Errorf("%s: %s is a leaf package; it imports %s", path, dir, p)
			}
		}
		if strings.HasPrefix(dir, "cmd/") || isTest {
			return nil
		}
		full, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(full, func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := s.X.(*ast.Ident); ok && x.Name == "os" {
					switch s.Sel.Name {
					case "Getenv", "LookupEnv", "Environ", "Args", "Stdin", "Stdout":
						t.Errorf("%s: os.%s outside cmd/", path, s.Sel.Name)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
