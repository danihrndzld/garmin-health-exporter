package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

func inputError(msg string) error { return &garmin.CodeError{Code: "GARMIN_INPUT", Msg: msg} }

// garminHosts points gconnect at GARMIN_FAKE_SERVER when it is a loopback URL, else at the real Garmin.
func garminHosts() gconnect.Hosts {
	if f := fakeServer("GARMIN_FAKE_SERVER"); f != "" {
		return gconnect.SameHost(f)
	}
	return gconnect.Hosts{}
}

func dataDir(req map[string]any) (string, error) {
	dir, err := guard.InsideHome(str(req, "dataDir"))
	if err != nil {
		return "", inputError("dataDir: " + err.Error())
	}
	return dir, nil
}

// garminStatus reads tokens.json once and never sends a request.
func garminStatus(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	dir, err := dataDir(req)
	if err != nil {
		return nil, err
	}
	return garmin.StatusOf(dir, time.Now()), nil
}

func garminDisconnect(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	dir, err := dataDir(req)
	if err != nil {
		return nil, err
	}
	if err := garmin.Delete(dir); err != nil {
		return nil, &garmin.CodeError{Code: "GARMIN_ERROR", Msg: err.Error()}
	}
	return garmin.StatusOf(dir, time.Now()), nil
}

func garminConnect(ctx context.Context, req map[string]any, out *protocol.Emitter) (any, error) {
	email, password := str(req, "email"), str(req, "password")
	dir, err := guard.InsideHome(str(req, "dataDir"))
	if err == nil {
		err = guard.Credentials(email, password)
	}
	if err != nil {
		return nil, inputError(err.Error())
	}
	a := &gconnect.Auth{Hosts: garminHosts(), Log: func(m string) { out.Log("dim", m) }}
	return garmin.Connect(ctx, a, dir, email, password, time.Now())
}

func clearCache(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	dir, err := dataDir(req)
	if err != nil {
		return nil, err
	}
	// tokens.json and mfp_session.json stay: clearing the cache must not sign anyone out.
	err = os.Remove(filepath.Join(dir, "garmin_cache.db"))
	if os.IsNotExist(err) {
		err = nil
	}
	for _, d := range []string{"garmin-cache", "mfp-cache"} {
		if e := os.RemoveAll(filepath.Join(dir, d)); err == nil {
			err = e
		}
	}
	if err != nil {
		return nil, err // no errorCode, as before
	}
	return map[string]any{}, nil
}

func export(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	var err error
	start, end := str(req, "startDate"), str(req, "endDate")
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
		return nil, inputError(err.Error())
	}
	if on, ok := req["garmin"].(bool); !ok || on {
		return nil, &garmin.CodeError{Code: "GARMIN_ERROR", Msg: "The Go Garmin export is not ready yet."}
	}
	// Garmin off: only the folder, for mfp-export to fill. No tokens are read and no request is sent.
	dir, err := garmin.PrepareDir(str(req, "outputDir"), end)
	if err != nil {
		return nil, inputError("outputDir: " + err.Error())
	}
	return map[string]any{"path": dir, "jsonPath": nil, "startDate": start, "endDate": end}, nil
}
