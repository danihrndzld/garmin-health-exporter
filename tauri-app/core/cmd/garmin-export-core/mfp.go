package main

import (
	"context"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
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

// guardMFP runs the trust boundary checks and returns r with both dirs resolved inside $HOME.
// The messages match mfp.Export's own re-check, so the result line is the same whichever runs.
func guardMFP(r mfp.ExportRequest) (mfp.ExportRequest, error) {
	bad := func(msg string) (mfp.ExportRequest, error) { return r, &mfp.CodeError{Code: "MFP_INPUT", Msg: msg} }
	if err := guard.ValidateRange(r.StartDate, r.EndDate, 90); err != nil {
		return bad(err.Error())
	}
	var err error
	if r.DataDir, err = guard.InsideHome(r.DataDir); err != nil {
		return bad("dataDir: " + err.Error())
	}
	if r.CSVDir, err = guard.InsideHome(r.CSVDir); err != nil {
		return bad("csvDir: " + err.Error())
	}
	return r, nil
}

func mfpExport(ctx context.Context, req map[string]any, out *protocol.Emitter) (any, error) {
	r, err := guardMFP(mfpRequest(req))
	if err != nil {
		return nil, err
	}
	return mfp.Export(ctx, r, out)
}
