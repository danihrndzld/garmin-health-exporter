package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/report"
)

// buildReport decodes the request through a typed struct; an absent version or os reads "?", as
// report.js defaulted them. A log line that is not {ts, type, msg} strings is an invalid request.
func buildReport(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	in := struct {
		Error   string        `json:"error"`
		Log     []report.Line `json:"log"`
		Version string        `json:"version"`
		OS      string        `json:"os"`
	}{Version: "?", OS: "?"}
	b, _ := json.Marshal(req)
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, errors.New("Invalid report request.")
	}
	return report.Build(in.Error, in.Log, in.Version, in.OS), nil
}
