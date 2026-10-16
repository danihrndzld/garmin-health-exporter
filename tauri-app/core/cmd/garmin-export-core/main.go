// garmin-export-core: the Go backend for Garmin Export. main reads one request line, runs its
// command and writes the result line. The commands are in garmin.go, mfp.go, summary.go, pack.go and report.go.
package main

import (
	"context"
	"errors"
	"os"
	"regexp"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

// handler runs one command. It returns the result payload without ok, or an error.
type handler func(ctx context.Context, req map[string]any, out *protocol.Emitter) (any, error)

var commands = map[string]handler{
	"garmin-status":     garminStatus,
	"garmin-disconnect": garminDisconnect,
	"garmin-connect":    garminConnect,
	"clear-cache":       clearCache,
	"export":            export,
	"mfp-export":        mfpExport,
	"summary":           summary,
	"pack":              buildPack,
	"report":            buildReport,
}

// coded is a use case error that carries the errorCode the UI shows.
type coded interface{ ErrorCode() string }

var loopback = regexp.MustCompile(`^http://(127\.0\.0\.1|localhost):\d+$`)

// fakeServer returns the variable when it is a loopback URL, else "", so a test server can
// never redirect traffic elsewhere.
func fakeServer(name string) string {
	if v := os.Getenv(name); loopback.MatchString(v) {
		return v
	}
	return ""
}

func str(req map[string]any, key string) string { s, _ := req[key].(string); return s }

func main() {
	out := &protocol.Emitter{Out: os.Stdout}
	req, err := protocol.ReadRequest(os.Stdin)
	if err != nil {
		out.Result(map[string]any{"ok": false, "error": "Invalid request."})
		return
	}
	run, ok := commands[str(req, "cmd")]
	if !ok {
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
		return
	}
	v, err := run(context.Background(), req, out)
	if err == nil {
		out.Result(map[string]any{"ok": true}, v)
		return
	}
	fail := map[string]any{"ok": false, "error": err.Error()}
	if c := coded(nil); errors.As(err, &c) {
		fail["errorCode"] = c.ErrorCode()
	}
	out.Result(fail)
}
