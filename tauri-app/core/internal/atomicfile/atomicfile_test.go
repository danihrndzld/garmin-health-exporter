package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	t.Run("EG-201 a failed write keeps the old file and leaves no temporary file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.json")
		if err := WriteFile(p, []byte("old")); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o500); err != nil { // the temporary file cannot be created
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
		if err := WriteFile(p, []byte("new")); err == nil {
			t.Skip("directory permissions are not enforced here (running as root?)")
		}
		os.Chmod(dir, 0o700)
		if b, _ := os.ReadFile(p); string(b) != "old" {
			t.Fatalf("old file damaged: %q", b)
		}
		if m, _ := filepath.Glob(filepath.Join(dir, ".tmp-*")); len(m) != 0 {
			t.Fatalf("temporary files left: %v", m)
		}
	})
	t.Run("EG-202 the file is replaced with mode 0600", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "f.json")
		for _, body := range []string{"one", "two"} {
			if err := WriteFile(p, []byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("stat %v %v", st, err)
		}
		if b, _ := os.ReadFile(p); string(b) != "two" {
			t.Fatalf("got %q", b)
		}
	})
}
