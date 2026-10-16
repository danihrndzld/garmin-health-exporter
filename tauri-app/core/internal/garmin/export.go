package garmin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/atomicfile"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
)

// mfpFiles are the files mfp-export writes. A copy, not an import: internal/garmin must not depend on internal/mfp.
var mfpFiles = []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"}

// Reporter receives the log and progress lines; *protocol.Emitter satisfies it.
type Reporter interface {
	Log(kind, msg string)
	Warn(msg, code string)
	Progress(phase string, current, total int)
}

// ExportRequest is an export with Garmin on. cmd has already run guard on it.
type ExportRequest struct {
	StartDate, EndDate string
	RefreshWindow      int
	OutputDir, DataDir string
	Hosts              gconnect.Hosts // zero value: the real Garmin
	Gap                time.Duration  // gconnect.Client.Gap: 0 means 500 ms; -1 only with the loopback fake
	Now                func() time.Time
}

type ExportResult struct {
	Path       string   `json:"path"`
	JSONPath   *string  `json:"jsonPath"` // null with garmin:false
	StartDate  string   `json:"startDate"`
	EndDate    string   `json:"endDate"`
	Days       int      `json:"days,omitempty"`
	Activities int      `json:"activities,omitempty"`
	Files      []string `json:"files,omitempty"`
}

const expiredMsg = "The Garmin session expired. Reconnect Garmin."

// removeMFP deletes MyFitnessPal files an earlier run left in dir, so they never reach the LLM pack.
func removeMFP(dir string) error {
	for _, f := range mfpFiles {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// PrepareDir makes csv_<endDate> inside outputDir for a run without Garmin and
// removes MyFitnessPal files an earlier run left there, so they never reach the LLM pack.
func PrepareDir(outputDir, endDate string) (string, error) {
	out, err := guard.InsideHome(outputDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(out, "csv_"+endDate)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, removeMFP(dir)
}

// stopErr maps the client's stop to the code the UI shows. A dead login is marked expired;
// a refresh that failed without proving it dead keeps tokens.json.
func stopErr(c *gconnect.Client, dataDir string, out Reporter) error {
	s := c.Stopped()
	if s == nil {
		return nil
	}
	switch s.Reason {
	case gconnect.StopSessionDead:
		if err := MarkExpired(dataDir, time.Now()); err != nil { // the next run finds the login dead again
			out.Warn("Could not mark the Garmin login as expired: "+err.Error(), "SAVE_FAILED")
		}
		return &CodeError{"GARMIN_SESSION", expiredMsg}
	case gconnect.StopRateLimited:
		return &CodeError{"GARMIN_RATE_LIMITED", s.Msg}
	case gconnect.StopBlocked:
		return &CodeError{"GARMIN_BLOCKED", s.Msg}
	}
	return &CodeError{"GARMIN_ERROR", s.Msg}
}

// parse turns a raw answer into a JavaScript value; nothing (nil) is null, as client.safe returns.
func parse(raw json.RawMessage) jsjson.Value {
	if raw == nil {
		return null
	}
	v, err := jsjson.Parse(raw)
	if err != nil {
		return null
	}
	return v
}

func obj(o *jsjson.Obj) jsjson.Value { return jsjson.Value{Kind: jsjson.Object, O: o} }

func endpoint(name string) gconnect.Endpoint { ep, _ := gconnect.Lookup(name); return ep }

// dailySteps ports fetchDailyStepsChunked (exporter.js:50-77): 28 day chunks, concatenated.
func dailySteps(ctx context.Context, c *gconnect.Client, start, end string) jsjson.Value {
	out := jsjson.Value{Kind: jsjson.Array, A: []jsjson.Value{}}
	s, _ := time.Parse("2006-01-02", start)
	e, _ := time.Parse("2006-01-02", end)
	for cur := s; !cur.After(e); {
		capped := cur.AddDate(0, 0, 27)
		if capped.After(e) {
			capped = e
		}
		chunk := parse(c.Safe(ctx, endpoint("daily_steps"), gconnect.Params{Start: cur.Format("2006-01-02"), End: capped.Format("2006-01-02")}))
		if chunk.Kind == jsjson.Array {
			out.A = append(out.A, chunk.A...)
		} else if v := chunk.Get("values"); v.Kind == jsjson.Array {
			out.A = append(out.A, v.A...)
		}
		cur = capped.AddDate(0, 0, 1)
		if c.Stopped() != nil {
			break
		}
	}
	return out
}

// Export ports exporter.js:exportHealth (lines 138-452) for a saved login: it never signs in.
func Export(ctx context.Context, r ExportRequest, out Reporter) (*ExportResult, error) {
	bad := func(msg string) (*ExportResult, error) { return nil, &CodeError{"GARMIN_INPUT", msg} }
	if err := guard.ValidateRange(r.StartDate, r.EndDate, 90); err != nil {
		return bad(err.Error())
	}
	if err := guard.RefreshWindow(r.RefreshWindow); err != nil {
		return bad(err.Error())
	}
	var err error
	if r.OutputDir, err = guard.InsideHome(r.OutputDir); err != nil {
		return bad("outputDir: " + err.Error())
	}
	if r.DataDir, err = guard.InsideHome(r.DataDir); err != nil {
		return bad("dataDir: " + err.Error())
	}
	if r.Now == nil {
		r.Now = time.Now
	}

	// 1. Auth (exporter.js:153-175): the saved login only. A refresh happens inside the client.
	f, ok := readRaw(r.DataDir)
	switch {
	case ok && f.ExpiredAt != "":
		return nil, &CodeError{"GARMIN_SESSION", expiredMsg}
	case !ok || f.Kind() == gconnect.KindNone:
		return nil, &CodeError{"GARMIN_SESSION", "Connect Garmin first."}
	}
	out.Log("dim", "Using the saved Garmin login")

	// 2. Client (exporter.js:177-190): lines with a code are warnings for the bug report.
	c := &gconnect.Client{
		Auth:     &gconnect.Auth{Hosts: r.Hosts},
		Tokens:   &f.Tokens,
		OnTokens: func(t *gconnect.Tokens) error { f.Tokens = *t; return f.save(r.DataDir) },
		Gap:      r.Gap,
		Log: func(kind, msg, code string) {
			if code != "" {
				out.Warn(msg, code)
			} else {
				out.Log(kind, msg)
			}
		},
	}
	cache := Cache{Dir: filepath.Join(r.DataDir, "garmin-cache")}

	// 4. Display name (exporter.js:202-214). The JS logged it; Go does not, the bug report would send it out.
	displayName, err := c.Profile(ctx)
	if err != nil {
		out.Log("warn", "Could not get display name: "+err.Error())
		if err := stopErr(c, r.DataDir, out); err != nil {
			return nil, err
		}
	}

	now := r.Now()
	dates := guard.Dates(r.StartDate, r.EndDate)
	slices.Reverse(dates) // newest first, as generateDateRange
	out.Log("info", fmt.Sprintf("Pulling %d days: %s -> %s", len(dates), r.StartDate, r.EndDate))

	// 5. Daily endpoints (exporter.js:248-303). The day file keeps earlier answers this run did not replace.
	daily := jsjson.NewObj()
	for i, date := range dates {
		day := jsjson.NewObj()
		entries := cache.Day(date)
		useCache := !InRefreshWindow(date, r.RefreshWindow, now)
		for _, ep := range gconnect.Daily {
			if ep.AliasOf != "" && slices.Contains(day.Keys, ep.AliasOf) {
				day.Set(ep.Name, day.Get(ep.AliasOf))
				continue
			}
			if e, ok := entries[ep.Name]; useCache && ok && Trust(e, date) {
				day.Set(ep.Name, parse(e.Data))
				continue
			}
			raw := c.Safe(ctx, ep, gconnect.Params{Date: date, DisplayName: displayName})
			day.Set(ep.Name, parse(raw))
			if raw != nil { // a null answer is never stored
				entries[ep.Name] = Entry{FetchedAt: now.UTC().Format(time.RFC3339), Data: raw}
			}
		}
		if err := cache.PutDay(date, entries); err != nil {
			out.Log("warn", fmt.Sprintf("Cache write failed for %s: %v", date, err))
		}
		daily.Set(date, obj(day))
		out.Progress("daily", i+1, len(dates))
		out.Log("dim", "  "+date)
		if err := stopErr(c, r.DataDir, out); err != nil {
			return nil, err
		}
	}

	// 6. Aggregated endpoints, always asked again (exporter.js:305-323).
	out.Log("info", "Pulling aggregated metrics...")
	aggregated := jsjson.NewObj()
	for i, ep := range gconnect.Aggregated {
		if ep.Name == "daily_steps" {
			aggregated.Set(ep.Name, dailySteps(ctx, c, r.StartDate, r.EndDate))
		} else {
			aggregated.Set(ep.Name, parse(c.Safe(ctx, ep, gconnect.Params{Start: r.StartDate, End: r.EndDate, DisplayName: displayName})))
		}
		out.Progress("aggregated", i+1, len(gconnect.Aggregated))
		if err := stopErr(c, r.DataDir, out); err != nil {
			return nil, err
		}
	}

	// 7. Activity list, 20 per page (exporter.js:325-345).
	out.Log("info", "Pulling activities...")
	var acts []jsjson.Value
	const pageSize = 20
	for start := 0; ; start += pageSize {
		page := parse(c.Safe(ctx, endpoint("activities_by_date"), gconnect.Params{Start: r.StartDate, End: r.EndDate, Offset: start, Limit: pageSize}))
		if page.Kind != jsjson.Array || len(page.A) == 0 {
			break
		}
		acts = append(acts, page.A...)
		if len(page.A) < pageSize {
			break
		}
	}
	if err := stopErr(c, r.DataDir, out); err != nil {
		return nil, err
	}
	out.Log("info", fmt.Sprintf("Found %d activities", len(acts)))

	// 8. Splits, cached forever once fetched (exporter.js:347-389).
	for i, act := range acts {
		name := act.Get("activityName")
		if !truthy(name) {
			name = act.Get("activityType").Get("typeKey")
		}
		label := "unknown"
		if truthy(name) {
			label = jsjson.Cell(name)
		}
		date := ""
		if t := act.Get("startTimeLocal"); truthy(t) {
			date, _, _ = strings.Cut(jsjson.Cell(t), " ")
		}
		out.Log("dim", fmt.Sprintf("  [%d/%d] %s | %s", i+1, len(acts), date, label))
		if id := act.Get("activityId"); truthy(id) {
			idText := jsjson.Cell(id)
			splits, ok := cache.Splits(idText)
			if !ok {
				splits = c.Safe(ctx, endpoint("activity_splits"), gconnect.Params{ActivityID: idText})
				if splits != nil {
					if err := cache.PutSplits(idText, splits); err != nil {
						out.Log("warn", fmt.Sprintf("Cache write failed for splits/%s: %v", idText, err))
					}
				}
			}
			act.O.Set("splits", parse(splits))
		}
		out.Progress("activities", i+1, len(acts))
		if err := stopErr(c, r.DataDir, out); err != nil {
			return nil, err
		}
	}

	// Goals (exporter.js:391-394).
	goals := parse(c.Safe(ctx, endpoint("goals"), gconnect.Params{}))
	if goals.Kind != jsjson.Array {
		goals = jsjson.Value{Kind: jsjson.Array, A: []jsjson.Value{}}
	}
	if err := stopErr(c, r.DataDir, out); err != nil {
		return nil, err
	}

	// 9-10. The JSON file, in the shape Bun's summary reads (exporter.js:396-415).
	exportDate := now.Format("2006-01-02")
	rng := jsjson.NewObj()
	rng.Set("start", jsjson.Str(r.StartDate))
	rng.Set("end", jsjson.Str(r.EndDate))
	rng.Set("days", jsjson.Num(float64(len(dates))))
	data := jsjson.NewObj()
	data.Set("export_date", jsjson.Str(exportDate))
	data.Set("date_range", obj(rng))
	data.Set("daily", obj(daily))
	data.Set("aggregated", obj(aggregated))
	data.Set("activities", jsjson.Value{Kind: jsjson.Array, A: append([]jsjson.Value{}, acts...)})
	data.Set("goals", goals)
	if err := os.MkdirAll(r.OutputDir, 0o755); err != nil {
		return nil, &CodeError{"GARMIN_ERROR", err.Error()}
	}
	jsonPath := filepath.Join(r.OutputDir, "garmin_health_"+exportDate+"_"+now.Format("15-04-05")+".json")
	if err := atomicfile.WriteFile(jsonPath, jsjson.Indent(obj(data))); err != nil {
		return nil, &CodeError{"GARMIN_ERROR", err.Error()}
	}
	out.Log("success", "JSON saved: "+jsonPath)

	// 11. CSV files (exporter.js:417-428), then the MyFitnessPal files of an earlier run.
	csvDir, files, err := WriteCSVs(obj(data), r.OutputDir)
	if err != nil {
		out.Log("warn", "CSV generation failed: "+err.Error())
		return nil, &CodeError{"GARMIN_ERROR", "CSV generation failed: " + err.Error()}
	}
	out.Progress("csv", 1, 1)
	out.Log("success", "CSVs written to "+csvDir)
	if err := removeMFP(csvDir); err != nil {
		out.Log("warn", "Could not remove an old MyFitnessPal file: "+err.Error())
	}

	out.Log("success", fmt.Sprintf("Done. %d days | %d activities", len(dates), len(acts)))
	return &ExportResult{Path: csvDir, JSONPath: &jsonPath, StartDate: r.StartDate, EndDate: r.EndDate,
		Days: len(dates), Activities: len(acts), Files: files}, nil
}
