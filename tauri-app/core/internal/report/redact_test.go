package report

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/danihrndzld/gconnect"
)

// cases.json was written by the JavaScript redact.js (removed in stage 3; see tag archive/electron-app). The report URLs depend
// on the library giving the same output, so a gconnect bump that changes redaction fails here first.
func TestRedactionMatchesTheJavaScript(t *testing.T) {
	t.Run("AC-601 gconnect.Redact and RedactURL give the JavaScript output for every frozen case", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/cases.json")
		if err != nil {
			t.Fatal(err)
		}
		var cases []struct{ Fn, In, Out string }
		if err := json.Unmarshal(raw, &cases); err != nil {
			t.Fatal(err)
		}
		if len(cases) != 28 {
			t.Fatalf("%d cases, want 28", len(cases))
		}
		for _, c := range cases {
			got := gconnect.RedactURL(c.In)
			if c.Fn == "string" {
				got = gconnect.Redact(c.In)
			}
			if got != c.Out {
				t.Errorf("%s(%q)\n got %q\nwant %q", c.Fn, c.In, got, c.Out)
			}
		}
	})
}
