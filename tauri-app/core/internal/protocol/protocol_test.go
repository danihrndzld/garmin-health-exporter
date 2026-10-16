package protocol

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
)

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %q is not a JSON object: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

func TestEmitterLineShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		emit  func(e *Emitter)
		check func(t *testing.T, m map[string]any)
	}{
		{"CL-101 a log line has kind, type, msg and a HH:MM:SS ts", func(e *Emitter) { e.Log("dim", "hello") }, func(t *testing.T, m map[string]any) {
			ts, _ := m["ts"].(string)
			if m["kind"] != "log" || m["type"] != "dim" || m["msg"] != "hello" || len(ts) != 8 {
				t.Fatalf("log line = %v", m)
			}
		}},
		{"CL-102 a progress line has kind, phase, current and total", func(e *Emitter) { e.Progress("mfp", 2, 5) }, func(t *testing.T, m map[string]any) {
			if m["kind"] != "progress" || m["phase"] != "mfp" || m["current"] != float64(2) || m["total"] != float64(5) {
				t.Fatalf("progress line = %v", m)
			}
		}},
		{"CL-103 a result line carries kind result plus the payload", func(e *Emitter) { e.Result(map[string]any{"ok": true, "days": 4}) }, func(t *testing.T, m map[string]any) {
			if m["kind"] != "result" || m["ok"] != true || m["days"] != float64(4) {
				t.Fatalf("result line = %v", m)
			}
		}},
		{"EG-178 a result that JSON cannot encode (NaN) still ends with a failed result line", func(e *Emitter) { e.Result(map[string]any{"ok": true, "x": math.NaN()}) }, func(t *testing.T, m map[string]any) {
			if m["kind"] != "result" || m["ok"] != false || m["error"] == nil {
				t.Fatalf("result line = %v", m)
			}
		}},
		{"EG-179 a payload key named kind cannot overwrite the result kind", func(e *Emitter) { e.Result(map[string]any{"ok": true, "kind": "log"}) }, func(t *testing.T, m map[string]any) {
			if m["kind"] != "result" || m["ok"] != true {
				t.Fatalf("result line = %v", m)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			tc.emit(&Emitter{Out: &buf})
			got := lines(t, &buf)
			if len(got) != 1 {
				t.Fatalf("got %d lines, want exactly one JSON object per call", len(got))
			}
			tc.check(t, got[0])
		})
	}
}

func TestReadRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantCmd string
		wantErr bool
	}{
		{"CL-104 only the first line is read", "{\"cmd\":\"mfp-export\"}\n{\"cmd\":\"other\"}\n", "mfp-export", false},
		{"CL-105 a request without a trailing newline is accepted", `{"cmd":"mfp-export"}`, "mfp-export", false},
		{"EG-105 invalid JSON is an error", "not json\n", "", true},
		{"EG-106 empty input is an error", "", "", true},
		{"EG-107 a JSON array instead of an object is an error", "[1,2]\n", "", true},
		{"EG-108 a blank first line is an error even if a valid line follows", "\n{\"cmd\":\"x\"}\n", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := ReadRequest(strings.NewReader(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error = %v", err, tc.wantErr)
			}
			if !tc.wantErr && req["cmd"] != tc.wantCmd {
				t.Fatalf("req = %v", req)
			}
		})
	}
}

func TestEmitterConcurrentWritersNeverInterleaveLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	e := &Emitter{Out: &buf}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				e.Log("dim", strings.Repeat("x", 200))
				e.Progress("mfp", i, 50)
			}
		}()
	}
	wg.Wait()
	t.Run("EG-109 800 lines written from 8 goroutines are all intact JSON objects", func(t *testing.T) {
		if got := lines(t, &buf); len(got) != 800 {
			t.Fatalf("got %d lines, want 800", len(got))
		}
	})
}
