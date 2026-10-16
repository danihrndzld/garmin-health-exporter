package main

import (
	"context"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

// mfpRequest decodes mfp-export. Only a loopback MFP_FAKE_SERVER replaces both hosts and drops the pause.
func mfpRequest(req map[string]any) mfp.ExportRequest {
	r := mfp.ExportRequest{StartDate: str(req, "startDate"), EndDate: str(req, "endDate"), DataDir: str(req, "dataDir"), CSVDir: str(req, "csvDir"), Gap: time.Second}
	if base := fakeServer("MFP_FAKE_SERVER"); base != "" {
		r.Base, r.Gap = base, 0
	}
	return r
}

// mfpExport validates its own input: mfp.Export runs guard before any request.
func mfpExport(ctx context.Context, req map[string]any, out *protocol.Emitter) (any, error) {
	return mfp.Export(ctx, mfpRequest(req), out)
}
