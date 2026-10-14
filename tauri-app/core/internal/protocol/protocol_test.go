package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitterWritesOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	e := Emitter{Out: &buf}
	e.Log("dim", "hello")
	e.Progress("mfp", 2, 5)
	e.Result(map[string]any{"ok": true})
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &last); err != nil || last["kind"] != "result" || last["ok"] != true {
		t.Fatalf("bad result line %q", lines[2])
	}
	var first map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	if first["kind"] != "log" || first["type"] != "dim" || first["msg"] != "hello" || first["ts"] == "" {
		t.Fatalf("bad log line %q", lines[0])
	}
}

func TestReadRequestUsesOnlyTheFirstLine(t *testing.T) {
	req, err := ReadRequest(strings.NewReader("{\"cmd\":\"mfp-export\"}\n{\"cmd\":\"other\"}\n"))
	if err != nil || req["cmd"] != "mfp-export" {
		t.Fatalf("got %v %v", req, err)
	}
	if _, err := ReadRequest(strings.NewReader("not json\n")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
