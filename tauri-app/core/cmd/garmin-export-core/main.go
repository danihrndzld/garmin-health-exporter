// garmin-export-core: the Go backend for Garmin Export. Stage 1 serves the
// MyFitnessPal export; the Garmin commands move here in stage 2.
package main

import (
	"context"
	"os"
	"path/filepath"
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
	case "clear-cache":
		dir, _ := req["dataDir"].(string)
		dataDir, err := guard.InsideHome(dir)
		if err != nil {
			out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_INPUT", "error": "dataDir: " + err.Error()})
			return
		}
		// tokens.json and mfp_session.json stay: clearing the cache must not sign anyone out.
		err = os.Remove(filepath.Join(dataDir, "garmin_cache.db"))
		if os.IsNotExist(err) {
			err = nil
		}
		for _, d := range []string{"garmin-cache", "mfp-cache"} {
			if e := os.RemoveAll(filepath.Join(dataDir, d)); err == nil {
				err = e
			}
		}
		if err != nil {
			out.Result(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		out.Result(map[string]any{"ok": true})
	case "garmin-connect":
		dir, _ := req["dataDir"].(string)
		email, _ := req["email"].(string)
		password, _ := req["password"].(string)
		dataDir, err := guard.InsideHome(dir)
		if err == nil {
			err = guard.Credentials(email, password)
		}
		if err != nil {
			out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_INPUT", "error": err.Error()})
			return
		}
		a := &garmin.Auth{DataDir: dataDir, Log: func(m string) { out.Log("dim", m) }}
		if _, err := a.Login(context.Background(), email, password); err != nil {
			out.Result(garmin.LoginResult(err))
			return
		}
		s := garmin.StatusOf(dataDir, time.Now())
		out.Result(map[string]any{"ok": true, "connected": s.Connected, "expired": s.Expired, "savedAt": s.SavedAt, "loginKind": s.Kind})
	default:
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
	}
}
