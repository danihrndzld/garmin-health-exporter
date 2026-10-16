package report

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

type input struct {
	Error   string `json:"error"`
	Log     []Line `json:"log"`
	Version string `json:"version"`
	OS      string `json:"os"`
}

func load(t *testing.T) (input, map[string]string) {
	t.Helper()
	var in input
	var want map[string]string
	b, err := os.ReadFile("testdata/input.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile("testdata/golden.json")
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	return in, want
}

func TestReportURLsEqualTheJavaScript(t *testing.T) {
	t.Run("AC-604 both URLs equal golden.json byte for byte", func(t *testing.T) {
		in, want := load(t)
		r := Build(in.Error, in.Log, in.Version, in.OS)
		if r.IssueURL != want["issueUrl"] {
			t.Errorf("issueUrl differs at %d:\n got %s\nwant %s", firstDiff(r.IssueURL, want["issueUrl"]), r.IssueURL, want["issueUrl"])
		}
		if r.MailtoURL != want["mailtoUrl"] {
			t.Errorf("mailtoUrl differs at %d:\n got %s\nwant %s", firstDiff(r.MailtoURL, want["mailtoUrl"]), r.MailtoURL, want["mailtoUrl"])
		}
	})
}

func TestNewestLinesKeptWithinTheLimits(t *testing.T) {
	t.Run("EG-604 300 lines: line 299 stays, line 0 goes, the limits hold and no secret, email or home folder leaks", func(t *testing.T) {
		in, _ := load(t)
		r := Build(in.Error, in.Log, in.Version, in.OS)
		if len(r.IssueURL) > 7500 || len(r.MailtoURL) > 1800 {
			t.Fatalf("lengths %d %d", len(r.IssueURL), len(r.MailtoURL))
		}
		body, _ := url.QueryUnescape(strings.SplitN(r.IssueURL, "&body=", 2)[1])
		if !strings.Contains(body, "line 299") || strings.Contains(body, "line 0 ") {
			t.Error("the newest line must stay and the oldest must go")
		}
		for _, secret := range []string{"example.com", "hunter2", "/Users/test", "eyJhbGci", "a@b.com"} {
			if strings.Contains(body, secret) || strings.Contains(r.MailtoURL, secret) {
				t.Errorf("%q leaked", secret)
			}
		}
		if !strings.HasPrefix(r.IssueURL, "https://github.com/"+Repo+"/issues/new?title=") || !strings.HasPrefix(r.MailtoURL, "mailto:"+Email+"?subject=") {
			t.Error("prefixes")
		}
	})
}

func TestEncodeURIComponentMatchesJavaScript(t *testing.T) {
	t.Run("EP-602 the unreserved set, the space, every reserved character and a multibyte rune encode as node does", func(t *testing.T) {
		// node -e 'console.log(encodeURIComponent("a b!*'"'"'()~-_.é\n`$&+,/:;=?@#"))' on 2026-10-16
		if got := EncodeURIComponent("a b!*'()~-_.é\n`$&+,/:;=?@#"); got != "a%20b!*'()~-_.%C3%A9%0A%60%24%26%2B%2C%2F%3A%3B%3D%3F%40%23" {
			t.Errorf("got %s", got)
		}
		if got := EncodeURIComponent("·"); got != "%C2%B7" {
			t.Errorf("got %s", got)
		}
		if got := EncodeURIComponent("😀"); got != "%F0%9F%98%80" {
			t.Errorf("got %s", got)
		}
		// A Go string cannot hold a lone surrogate; an invalid byte stands for one and becomes U+FFFD.
		if got := EncodeURIComponent("a\xffb"); got != "a%EF%BF%BDb" {
			t.Errorf("got %s", got)
		}
	})
}

func TestTitleIsCutAt120UTF16Units(t *testing.T) {
	t.Run("EG-605 a 200 rune error gives a 120 unit title; an empty error reads Unknown error", func(t *testing.T) {
		long := strings.Repeat("é", 200)
		r := Build(long, nil, "1", "x")
		title, _ := url.QueryUnescape(strings.TrimPrefix(strings.SplitN(r.IssueURL, "&body=", 2)[0], "https://github.com/"+Repo+"/issues/new?title="))
		if want := "Export failed: " + strings.Repeat("é", 105); title != want {
			t.Errorf("title has %d runes", len([]rune(title)))
		}
		if r2 := Build("", nil, "1", "x"); !strings.Contains(r2.IssueURL, "Unknown%20error") {
			t.Error("an empty error reads Unknown error")
		}
		// "Export failed: " is 15 units, so the 53rd emoji is cut after its high half (node throws URIError there).
		r3 := Build(strings.Repeat("😀", 60), nil, "1", "x")
		if want := "title=" + EncodeURIComponent("Export failed: "+strings.Repeat("😀", 52)) + "%EF%BF%BD&body="; !strings.Contains(r3.IssueURL, want) {
			t.Errorf("a cut surrogate pair must end the title as U+FFFD: %s", r3.IssueURL[:200])
		}
	})
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
