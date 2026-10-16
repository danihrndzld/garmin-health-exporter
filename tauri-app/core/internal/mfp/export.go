package mfp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

var csvFiles = []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"}

func fail(code, msg string) map[string]any {
	return map[string]any{"ok": false, "errorCode": code, "error": msg}
}

// Export runs the mfp-export command. It never touches garmin_cache.db.
func Export(ctx context.Context, req map[string]any, out *protocol.Emitter, opts Options) map[string]any {
	str := func(k string) string { s, _ := req[k].(string); return s }
	start, end := str("startDate"), str("endDate")
	if err := guard.ValidateRange(start, end, 90); err != nil {
		return fail("MFP_INPUT", err.Error())
	}
	dataDir, err := guard.InsideHome(str("dataDir"))
	if err != nil {
		return fail("MFP_INPUT", "dataDir: "+err.Error())
	}
	csvDir, err := guard.InsideHome(str("csvDir"))
	if err != nil {
		return fail("MFP_INPUT", "csvDir: "+err.Error())
	}

	// A previous run's files must never survive a failed or MFP-less run into the LLM pack.
	for _, f := range csvFiles {
		if err := os.Remove(filepath.Join(csvDir, f)); err != nil && !os.IsNotExist(err) {
			return fail("MFP_ERROR", "Could not remove the old "+f+": "+err.Error())
		}
	}

	sessPath := filepath.Join(dataDir, "mfp_session.json")
	sess, err := LoadSession(sessPath)
	if err != nil {
		return fail("MFP_SESSION", "Connect MyFitnessPal first.")
	}
	client := NewClient(sess, opts)
	sessionLost := false
	// Save rotated cookies even when the run stops early, but never after an expired
	// session: the page that says so may set cookies and would refresh saved_at.
	defer func() {
		if client.SessionChanged() && !sessionLost {
			_ = client.Session().Save(sessPath)
		}
	}()

	mapErr := func(err error) map[string]any {
		switch {
		case errors.Is(err, ErrSession):
			sessionLost = true
			return fail("MFP_SESSION", "The MyFitnessPal session expired. Reconnect MyFitnessPal.")
		case errors.Is(err, ErrRateLimited):
			return fail("MFP_RATE_LIMITED", "MyFitnessPal is rate limiting requests (HTTP 429). The days downloaded so far are cached; try again later.")
		default: // the raw error is for the log; the UI gets one short line
			out.Log("warn", "MyFitnessPal: "+err.Error())
			return fail("MFP_ERROR", "MyFitnessPal did not answer as expected. See the log for details.")
		}
	}

	out.Log("dim", "MyFitnessPal: signing in with the saved session")
	if err := client.Auth(ctx); err != nil {
		return mapErr(err)
	}
	goals, err := client.Goals(ctx, end)
	if errors.Is(err, ErrSession) || errors.Is(err, ErrRateLimited) {
		return mapErr(err)
	}
	if err != nil { // goals are optional context
		out.Log("warn", "MyFitnessPal goals unavailable: "+err.Error())
		goals = Goals{}
	}

	cache := Cache{Dir: filepath.Join(dataDir, "mfp-cache")}
	dates := guard.Dates(start, end)
	days := make([]DayData, 0, len(dates))
	fetched := 0
	for i, d := range dates {
		items, final, ok := cache.Get(d)
		if !ok || !final {
			items, err = client.Diary(ctx, d)
			if err != nil {
				return mapErr(err)
			}
			fetched++
			if err := cache.Put(d, items, time.Now()); err != nil {
				out.Log("warn", "MyFitnessPal cache write failed for "+d+": "+err.Error())
			}
		}
		days = append(days, DayData{Date: d, Items: items})
		out.Progress("mfp", i+1, len(dates))
	}

	logged, err := WriteFiles(csvDir, days, goals)
	if err != nil {
		return fail("MFP_ERROR", "Could not write the MyFitnessPal CSV files: "+err.Error())
	}
	out.Log("success", fmt.Sprintf("MyFitnessPal: food logged on %d of %d days (%d downloaded, %d from the cache)", logged, len(dates), fetched, len(dates)-fetched))
	return map[string]any{"ok": true, "days": len(dates), "loggedDays": logged, "csvDir": csvDir,
		"files": csvFiles}
}
