// garmin-export-core: the Go backend for Garmin Export. Stage 1 serves the
// MyFitnessPal export; the Garmin commands move here in stage 2.
package main

import (
	"context"
	"os"
	"time"

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
	default:
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
	}
}
