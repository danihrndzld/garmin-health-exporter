// Package protocol is the stdin/stdout contract of garmin-export-core:
// one JSON request line in; JSON lines out with kind log, progress or result.
package protocol

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"
)

type Emitter struct {
	Out io.Writer
	mu  sync.Mutex
}

func (e *Emitter) write(v map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, err := json.Marshal(v)
	if err != nil { // a NaN would otherwise drop the line, and a lost result reads as a crash
		b, _ = json.Marshal(map[string]any{"kind": v["kind"], "ok": false, "error": "Could not encode the answer: " + err.Error()})
	}
	e.Out.Write(append(b, '\n'))
}

func (e *Emitter) Log(kind, msg string) {
	e.write(map[string]any{"kind": "log", "type": kind, "msg": msg, "ts": time.Now().Format("15:04:05")})
}

// Warn is a warn log line that also carries a stable errorCode for the UI.
func (e *Emitter) Warn(msg, code string) {
	e.write(map[string]any{"kind": "log", "type": "warn", "msg": msg, "errorCode": code, "ts": time.Now().Format("15:04:05")})
}

func (e *Emitter) Progress(phase string, current, total int) {
	e.write(map[string]any{"kind": "progress", "phase": phase, "current": current, "total": total})
}

// Result writes one result line from parts, each a map or a struct that marshals to a JSON
// object; later keys win. Going through a map keeps the keys sorted, as they always were.
func (e *Emitter) Result(parts ...any) {
	out := map[string]any{}
	for _, p := range parts {
		var m map[string]any
		b, err := json.Marshal(p)
		if err == nil {
			err = json.Unmarshal(b, &m)
		}
		if err != nil { // a NaN would otherwise drop the line, and a lost result reads as a crash
			out = map[string]any{"ok": false, "error": "Could not encode the answer: " + err.Error()}
			break
		}
		for k, x := range m {
			out[k] = x
		}
	}
	out["kind"] = "result" // a payload key must never turn the result into another kind
	e.write(out)
}

// ReadRequest reads the first line only: Tauri's shell Child can write but cannot close stdin.
func ReadRequest(r io.Reader) (map[string]any, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return nil, err
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return nil, err
	}
	return req, nil
}
