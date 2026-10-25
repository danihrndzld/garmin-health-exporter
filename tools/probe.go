package main

// probe checks Garmin's Personal API with the key in .env (API_KEY="...").
//
// It trades the key for a 24 hour access token (one POST), then sends one read only GET per
// path the app uses, on personal.connectapi.garmin.com, 0.5 s apart, through the same gconnect
// client the app uses. It prints a table of status codes and writes .tmp/personal_api_probe.json.
// It never prints the key or the token, and it never signs in with a password.
//
// Exit 0 when every path the app depends on in API key mode answers 200, 1 otherwise.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danihrndzld/gconnect"
)

// probeHosts and probeGap are the real Garmin and gconnect's 0.5 s pause; probe_test.go
// points them at the gconnecttest fake.
var (
	probeHosts gconnect.Hosts
	probeGap   time.Duration
)

// probeRow is one gconnect endpoint and the start of its range: "s" 90 days, "s31", "s28",
// or "" for one day endpoints (Date is the end date).
type probeRow struct {
	name, start string
	needed      bool // the app asks it in API key mode; the others are login only and answer 404
}

var probeRows = []probeRow{
	// used by the app with the key (gconnect PersonalRanges and the shared paths)
	{"personal_daily_summaries", "s", true},
	{"personal_sleep", "s31", true},
	{"personal_hrv", "s", true},
	{"personal_body_battery", "s31", true},
	{"personal_fitness_age", "", true},
	{"daily_steps", "s28", true},
	{"agg_weigh_ins", "s", true},
	{"activities_by_date", "s", true},
	// connectapi paths the app asks with the email login; 404 here means "login only"
	{"training_readiness", "", false},
	{"training_status", "", false},
	{"endurance_score", "", false},
	{"max_metrics", "", false},
	{"respiration", "", false},
	{"stress", "", false},
	{"body_battery_events", "", false},
	{"blood_pressure", "s", false},
	{"race_predictions", "", false},
	{"goals", "", false},
	{"social_profile", "", false},
}

// readAPIKey reads API_KEY from <root>/.env by its name only.
func readAPIKey() (string, bool) {
	raw, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, _ := strings.Cut(line, "=")
		if strings.TrimSpace(k) == "API_KEY" {
			return strings.Trim(strings.Trim(strings.TrimSpace(v), `"`), "'"), true
		}
	}
	return "", false
}

func shape(body []byte) string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return fmt.Sprintf("%d bytes, not JSON", len(body))
	}
	switch x := v.(type) {
	case []any:
		return fmt.Sprintf("list of %d", len(x))
	case map[string]any:
		if d, ok := x["data"].([]any); ok {
			return fmt.Sprintf("data: list of %d", len(d))
		}
		// The first 5 keys in the order Garmin sent them.
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.Token()
		var keys []string
		for dec.More() && len(keys) < 5 {
			t, _ := dec.Token()
			keys = append(keys, fmt.Sprint(t))
			var skip json.RawMessage
			dec.Decode(&skip)
		}
		return "keys: " + strings.Join(keys, ", ")
	case nil:
		return "NoneType"
	case string:
		return "str"
	case bool:
		return "bool"
	default:
		return "float"
	}
}

func probeMain(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	endFlag := fs.String("end", time.Now().AddDate(0, 0, -1).Format("2006-01-02"), "end date, YYYY-MM-DD")
	days := fs.Int("days", 90, "range for the 90 day endpoints")
	if rc := parseFlags(fs, args); rc >= 0 {
		return rc
	}
	end, err := time.Parse("2006-01-02", *endFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "--end: %v\n", err)
		return 2
	}
	span := func(n int) string { return end.AddDate(0, 0, -(min(n, *days) - 1)).Format("2006-01-02") }
	starts := map[string]string{"s": span(90), "s31": span(31), "s28": span(28)}

	key, ok := readAPIKey()
	if !ok {
		fmt.Fprintln(os.Stderr, "API_KEY is missing from .env. Create a key at https://connect.garmin.com/app/settings/personalApiToken")
		return 1
	}
	ctx := context.Background()
	auth := &gconnect.Auth{Hosts: probeHosts}
	tok, err := auth.ExchangeAPIKey(ctx, key)
	if err != nil {
		if errors.Is(err, gconnect.ErrSessionDead) {
			fmt.Fprintln(os.Stderr, "Garmin refused the key (wrong, expired or deleted): "+err.Error())
		} else {
			fmt.Fprintln(os.Stderr, "The token request failed: "+err.Error())
		}
		return 1
	}
	fmt.Printf("token: OK, expires %s\n", time.Unix(tok.Personal.ExpiresAt, 0).Format("2006-01-02 15:04"))

	client := &gconnect.Client{Auth: auth, Tokens: tok, Gap: probeGap}
	type row struct {
		Name   string `json:"name"`
		Status int    `json:"status"`
		Needed bool   `json:"needed"`
		Shape  string `json:"shape"`
	}
	var rows []row
	var failed []string
	for _, pr := range probeRows {
		ep, ok := gconnect.Lookup(pr.name)
		if !ok {
			fmt.Fprintf(os.Stderr, "gconnect has no endpoint %q\n", pr.name)
			return 1
		}
		p := gconnect.Params{Date: *endFlag, End: *endFlag, Start: starts[pr.start], DisplayName: "x"}
		res := client.Get(ctx, ep, p)
		r := row{Name: pr.name, Status: res.Status, Needed: pr.needed}
		if res.Status == 200 {
			if res.Data != nil {
				r.Shape = shape(res.Data)
			} else if res.Code == "BAD_JSON" {
				r.Shape = "not JSON"
			} else {
				r.Shape = "0 bytes, not JSON"
			}
		} else if res.Status == 0 && res.Code != "" {
			r.Shape = res.Code // no answer: NETWORK, TIMEOUT, or the client stopped the run
		}
		rows = append(rows, r)
		if pr.needed && res.Status != 200 {
			failed = append(failed, pr.name)
		}
		label := "login  "
		if pr.needed {
			label = "needed "
		}
		fmt.Printf("%-26s %d  %s %s\n", pr.name, res.Status, label, r.Shape)
	}

	out := filepath.Join(root, ".tmp", "personal_api_probe.json")
	_ = os.Mkdir(filepath.Dir(out), 0o755)
	data, _ := json.MarshalIndent(struct {
		ProbedAt string `json:"probed_at"`
		End      string `json:"end"`
		Rows     []row  `json:"rows"`
	}{time.Now().Format("2006-01-02T15:04:05"), *endFlag, rows}, "", "  ")
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\nwrote %s\n", rel(out))
	if len(failed) > 0 {
		fmt.Println("API key mode is missing: " + strings.Join(failed, ", "))
		return 1
	}
	return 0
}
