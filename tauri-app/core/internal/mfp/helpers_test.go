package mfp

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

func testSession() *Session {
	return &Session{UserAgent: "test-agent", Cookies: []Cookie{{Name: "__Secure-next-auth.session-token", Value: "s1", Domain: ".myfitnesspal.com", Path: "/"}}}
}

func newTestClient(t *testing.T) (*Client, *mfptest.Server) {
	t.Helper()
	srv := mfptest.NewServer("testdata")
	t.Cleanup(srv.Close)
	return NewClient(testSession(), Options{Base: srv.URL}), srv
}

// homeDirs makes a data dir and a csv dir inside $HOME: the guard rejects t.TempDir().
func homeDirs(t *testing.T) (dataDir, csvDir string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".mfp-export-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	dataDir, csvDir = filepath.Join(root, "data"), filepath.Join(root, "csv_2026-10-07")
	for _, d := range []string{dataDir, csvDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return
}

// exportEnv is one isolated run of mfp-export: fake server, dirs inside $HOME and a saved session.
type exportEnv struct {
	srv             *mfptest.Server
	dataDir, csvDir string
	sessPath        string
}

func newExportEnv(t *testing.T) *exportEnv {
	t.Helper()
	srv := mfptest.NewServer("testdata")
	t.Cleanup(srv.Close)
	dataDir, csvDir := homeDirs(t)
	e := &exportEnv{srv: srv, dataDir: dataDir, csvDir: csvDir, sessPath: filepath.Join(dataDir, "mfp_session.json")}
	if err := testSession().Save(e.sessPath); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *exportEnv) req(start, end string) map[string]any {
	return map[string]any{"cmd": "mfp-export", "startDate": start, "endDate": end, "dataDir": e.dataDir, "csvDir": e.csvDir}
}

// run drives Export like the binary does: a request map in, protocol lines and a result out.
func (e *exportEnv) run(t *testing.T, req map[string]any) (map[string]any, []map[string]any) {
	t.Helper()
	return runExport(t, context.Background(), e.srv, req)
}

func runExport(t *testing.T, ctx context.Context, srv *mfptest.Server, req map[string]any) (map[string]any, []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	res := Export(ctx, req, &protocol.Emitter{Out: &buf}, Options{Base: srv.URL})
	var lines []map[string]any
	if s := bytes.TrimSpace(buf.Bytes()); len(s) > 0 {
		for _, l := range bytes.Split(s, []byte("\n")) {
			var m map[string]any
			if err := json.Unmarshal(l, &m); err != nil {
				t.Fatalf("protocol line %q is not JSON: %v", l, err)
			}
			lines = append(lines, m)
		}
	}
	return res, lines
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// readCSV returns the header and the rows of a CSV file.
func readCSV(t *testing.T, path string) ([]string, [][]string) {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	rows, err := csv.NewReader(fh).ReadAll()
	if err != nil {
		t.Fatalf("%s is not valid CSV: %v", filepath.Base(path), err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s is empty", filepath.Base(path))
	}
	return rows[0], rows[1:]
}

// readCSVMaps returns each row as a column-name map.
func readCSVMaps(t *testing.T, path string) []map[string]string {
	t.Helper()
	h, rows := readCSV(t, path)
	var out []map[string]string
	for _, r := range rows {
		m := map[string]string{}
		for i, name := range h {
			m[name] = r[i]
		}
		out = append(out, m)
	}
	return out
}

func parseItems(t *testing.T, raw ...string) []DiaryItem {
	t.Helper()
	var items []DiaryItem
	for _, r := range raw {
		var it DiaryItem
		if err := json.Unmarshal([]byte(r), &it); err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	return items
}

func fixtureGoals() Goals {
	f := func(v float64) *float64 { return &v }
	return Goals{EnergyKcal: f(2200), Protein: f(150), Carbohydrate: f(220), Fat: f(73), Sodium: f(2300), Sugar: f(50)}
}

func nilEmitter(buf *bytes.Buffer) *protocol.Emitter { return &protocol.Emitter{Out: buf} }
