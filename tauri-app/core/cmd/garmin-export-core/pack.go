package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/pack"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

// buildPack ports cli.js:pack. Build deletes and recreates llm-* subfolders, so only an existing
// folder inside $HOME whose name starts with csv_ is accepted, and that is checked before anything else.
// InsideHome resolves symlinks, so the name and the type checked are those of the real folder.
func buildPack(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	csvDir, err := guard.InsideHome(str(req, "csvDir"))
	if err == nil {
		var st os.FileInfo
		if st, err = os.Stat(csvDir); err == nil && (!st.IsDir() || !strings.HasPrefix(filepath.Base(csvDir), "csv_")) {
			err = errors.New("not an export folder")
		}
	}
	if err != nil {
		return nil, errors.New("csvDir must be an export csv_ folder within your home directory.")
	}
	start, end := str(req, "startDate"), str(req, "endDate")
	if !isoDate.MatchString(start) || !isoDate.MatchString(end) {
		return nil, errors.New("startDate and endDate must be YYYY-MM-DD.")
	}
	return pack.Build(packOptions(csvDir, start, end, req), str(req, "lang"), str(req, "mode"))
}
