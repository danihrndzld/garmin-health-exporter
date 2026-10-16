package garmin

import (
	"os"
	"path/filepath"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
)

// mfpFiles are the files mfp-export writes. A copy, not an import: internal/garmin must not depend on internal/mfp.
var mfpFiles = []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"}

// PrepareDir makes csv_<endDate> inside outputDir for a run without Garmin and
// removes MyFitnessPal files an earlier run left there, so they never reach the LLM pack.
func PrepareDir(outputDir, endDate string) (string, error) {
	out, err := guard.InsideHome(outputDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(out, "csv_"+endDate)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, f := range mfpFiles {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return dir, nil
}
