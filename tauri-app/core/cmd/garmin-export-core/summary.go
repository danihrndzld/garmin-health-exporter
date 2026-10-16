package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/pack"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// warnings ports cli.js:cleanWarnings: at most 200 items of 500 UTF-16 units; anything that is not a list is empty.
func warnings(req map[string]any) []string {
	out := []string{}
	list, _ := req["warnings"].([]any)
	for _, w := range list[:min(200, len(list))] {
		var s string
		switch v := w.(type) { // String(x) of the JavaScript
		case string:
			s = v
		case nil:
			s = "null"
		case float64:
			s = jsjson.FormatNumber(v)
		case bool:
			s = fmt.Sprint(v)
		default:
			s = "[object Object]"
		}
		out = append(out, jsjson.SliceUTF16(s, 500))
	}
	return out
}

// tzName is what Intl.DateTimeFormat().resolvedOptions().timeZone gave the JavaScript. Go names the
// local zone "Local", so read $TZ, else where /etc/localtime points (macOS: .../zoneinfo/<name>),
// else the abbreviation. Only the Exported line of the context uses it.
func tzName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			return link[i+len("zoneinfo/"):]
		}
	}
	return time.Now().Format("MST")
}

func packOptions(csvDir, start, end string, req map[string]any) pack.Options {
	return pack.Options{CSVDir: csvDir, StartDate: start, EndDate: end, Warnings: warnings(req), Now: time.Now(), TZ: tzName()}
}

// summaryResult: a nil pointer marshals as null, which is what the UI expects for a missing part.
type summaryResult struct {
	Summary *garmin.Summary `json:"summary"`
	Pack    *pack.Preview   `json:"pack"`
}

// summary ports cli.js:summary, with its three request shapes and its sentences. Failures carry no errorCode.
func summary(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	if req["jsonPath"] == nil {
		// A run with Garmin off has no JSON: only the pack, from the csv folder and the range.
		csvDir, err := guard.InsideHome(str(req, "csvDir"))
		if err != nil {
			return nil, errors.New("csvDir must be a folder within your home directory.")
		}
		start, end := str(req, "startDate"), str(req, "endDate")
		if !isoDate.MatchString(start) || !isoDate.MatchString(end) {
			return nil, errors.New("startDate and endDate must be YYYY-MM-DD.")
		}
		p, err := pack.BuildPreview(packOptions(csvDir, start, end, req))
		if err != nil {
			return nil, err
		}
		return summaryResult{Pack: p}, nil
	}
	p, err := guard.InsideHome(str(req, "jsonPath"))
	if err != nil || !strings.HasSuffix(p, ".json") {
		return nil, errors.New("jsonPath must be a .json file within your home directory.")
	}
	raw, err := os.ReadFile(p)
	var s *garmin.Summary
	if err == nil {
		s, err = garmin.Summarize(raw)
	}
	if err != nil {
		return nil, errors.New("Could not read export.")
	}
	res := summaryResult{Summary: s}
	// A csvDir that is absent or outside $HOME gives no pack, not an error, as cli.js did.
	if csvDir, err := guard.InsideHome(str(req, "csvDir")); err == nil {
		start, end := s.Dates()
		if !isoDate.MatchString(start) || !isoDate.MatchString(end) {
			start, end = "", "" // no checked range, no pack: a date from the file never reaches a file name unchecked
		}
		if res.Pack, err = pack.BuildPreview(packOptions(csvDir, start, end, req)); err != nil {
			return nil, err
		}
	}
	return res, nil
}
