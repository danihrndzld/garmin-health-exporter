// garmin-export-core: the Go backend for Garmin Export. Stage 1 serves the
// MyFitnessPal export; the Garmin commands move here in stage 2.
package main

import (
	"context"
	"os"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

func main() {
	out := &protocol.Emitter{Out: os.Stdout}
	req, err := protocol.ReadRequest(os.Stdin)
	if err != nil {
		out.Result(map[string]any{"ok": false, "error": "Invalid request."})
		return
	}
	switch req["cmd"] {
	case "mfp-export":
		opts := mfp.Options{Gap: time.Second}
		if mfp.FakeServer() != "" {
			opts.Gap = 0 // only the loopback test server may skip the pause
		}
		out.Result(mfp.Export(context.Background(), req, out, opts))
	case "garmin-status", "garmin-disconnect":
		dir, _ := req["dataDir"].(string)
		dataDir, err := guard.InsideHome(dir)
		if err != nil {
			out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_INPUT", "error": "dataDir: " + err.Error()})
			return
		}
		if req["cmd"] == "garmin-disconnect" {
			if err := garmin.Delete(dataDir); err != nil {
				out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_ERROR", "error": err.Error()})
				return
			}
		}
		s := garmin.StatusOf(dataDir, time.Now())
		out.Result(map[string]any{"ok": true, "connected": s.Connected, "expired": s.Expired, "savedAt": s.SavedAt, "loginKind": s.Kind}) // not "kind": Emitter.Result sets that to "result"
	default:
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
	}
}
