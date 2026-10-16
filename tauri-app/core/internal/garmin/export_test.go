package garmin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareDirMakesTheEndDateFolderAndDropsStaleMfpFiles(t *testing.T) {
	home, _ := os.UserHomeDir()
	out, err := os.MkdirTemp(home, ".garmin-prepare-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	t.Run("CL-241 PrepareDir returns csv_<endDate> inside outputDir and removes the stale MFP files there", func(t *testing.T) {
		stale := filepath.Join(out, "csv_2026-10-07")
		os.MkdirAll(stale, 0o755)
		os.WriteFile(filepath.Join(stale, "mfp_food.csv"), []byte("old\n"), 0o644)
		dir, err := PrepareDir(out, "2026-10-07")
		if err != nil || dir != stale {
			t.Fatalf("PrepareDir = %q %v", dir, err)
		}
		if _, err := os.Stat(filepath.Join(stale, "mfp_food.csv")); !os.IsNotExist(err) {
			t.Fatal("stale MFP file kept")
		}
	})
	t.Run("CL-242 PrepareDir creates a folder that does not exist yet", func(t *testing.T) {
		dir, err := PrepareDir(filepath.Join(out, "new"), "2026-10-08")
		if err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s: %v", dir, err)
		}
	})
	t.Run("EP-221 PrepareDir rejects a folder outside $HOME", func(t *testing.T) {
		if _, err := PrepareDir("/tmp", "2026-10-07"); err == nil {
			t.Fatal("a folder outside $HOME was accepted")
		}
	})
}
