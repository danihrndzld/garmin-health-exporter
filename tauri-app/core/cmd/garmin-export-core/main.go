// garmin-export-core: the Go backend for Garmin Export. Stage 1 serves the
// MyFitnessPal export; the Garmin commands move here in stage 2.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

var loopback = regexp.MustCompile(`^http://(127\.0\.0\.1|localhost):\d+$`)

// garminHosts points gconnect at GARMIN_FAKE_SERVER when it is a loopback URL, else at the real Garmin.
func garminHosts() gconnect.Hosts {
	if v := os.Getenv("GARMIN_FAKE_SERVER"); loopback.MatchString(v) {
		return gconnect.SameHost(v)
	}
	return gconnect.Hosts{}
}

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
		a := &gconnect.Auth{Hosts: garminHosts(), Log: func(m string) { out.Log("dim", m) }}
		s, err := garmin.Connect(context.Background(), a, dataDir, email, password, time.Now())
		if ce := (*garmin.CodeError)(nil); errors.As(err, &ce) {
			out.Result(map[string]any{"ok": false, "errorCode": ce.Code, "error": ce.Msg})
			return
		}
		out.Result(map[string]any{"ok": true, "connected": s.Connected, "expired": s.Expired, "savedAt": s.SavedAt, "loginKind": s.Kind})
	case "export":
		bad := func(msg string) { out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_INPUT", "error": msg}) }
		start, _ := req["startDate"].(string)
		end, _ := req["endDate"].(string)
		if req["startDate"] != nil || req["endDate"] != nil {
			err = guard.ValidateRange(start, end, 90)
			if err == nil {
				err = guard.NotFuture(end, time.Now())
			}
		} else {
			n, ok := req["daysBack"].(float64)
			if !ok || n != float64(int(n)) {
				n = 0 // DaysBack rejects it with the usual sentence
			}
			start, end, err = guard.DaysBack(int(n), time.Now())
		}
		if err == nil && req["refreshWindow"] != nil {
			rw, ok := req["refreshWindow"].(float64)
			if !ok || rw != float64(int(rw)) {
				rw = 0
			}
			err = guard.RefreshWindow(int(rw))
		}
		if err != nil {
			bad(err.Error())
			return
		}
		if on, ok := req["garmin"].(bool); !ok || on {
			out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_ERROR", "error": "The Go Garmin export is not ready yet."})
			return
		}
		// Garmin off: only the folder, for mfp-export to fill. No tokens are read and no request is sent.
		outputDir, _ := req["outputDir"].(string)
		dir, err := garmin.PrepareDir(outputDir, end)
		if err != nil {
			bad("outputDir: " + err.Error())
			return
		}
		out.Result(map[string]any{"ok": true, "path": dir, "jsonPath": nil, "startDate": start, "endDate": end})
	default:
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
	}
}
