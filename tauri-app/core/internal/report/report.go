// Package report builds the two bug report links (a prefilled GitHub issue and a mailto),
// scrubbed before anything leaves the machine. Port of report.js, ported from the JavaScript sidecar (removed in stage 3; see tag archive/electron-app and git history).
package report

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
	"github.com/danihrndzld/gconnect"
)

const (
	Repo         = "danihrndzld/garmin-health-exporter"
	Email        = "danielernesto@ufm.edu"
	issueURLMax  = 7500 // GitHub rejects much longer prefilled URLs
	mailtoURLMax = 1800 // most mail clients cap the whole URL near 2 KB
)

var (
	kvRe    = regexp.MustCompile(`\b([\w-]+)=([^\s&"']+)`)
	emailRe = regexp.MustCompile(`[\w.+-]+@[\w-]+(\.[\w-]+)+`)
	homeRe  = regexp.MustCompile(`/Users/[^/\s]+`)
)

type Line struct {
	TS   string `json:"ts"`
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

type Result struct {
	IssueURL  string `json:"issueUrl"`
	MailtoURL string `json:"mailtoUrl"`
}

// Scrub ports scrub: redaction, key=value secrets, emails, home folders.
func Scrub(s string) string {
	s = gconnect.Redact(s)
	s = kvRe.ReplaceAllStringFunc(s, func(m string) string {
		k := m[:strings.IndexByte(m, '=')]
		if gconnect.IsSecretKey(k) {
			return k + "=REDACTED"
		}
		return m
	})
	s = emailRe.ReplaceAllString(s, "[email]")
	return homeRe.ReplaceAllString(s, "~")
}

// EncodeURIComponent keeps A-Z a-z 0-9 - _ . ! ~ * ' ( ) and writes every other UTF-8 byte as %XX.
// url.QueryEscape writes a space as + and escapes !*'(); url.PathEscape keeps $&+:=@.
// An invalid byte becomes U+FFFD, where node would throw on the lone surrogate it stands for.
func EncodeURIComponent(s string) string {
	var b strings.Builder
	var buf [utf8.UTFMax]byte
	for _, r := range s {
		for _, c := range buf[:utf8.EncodeRune(buf[:], r)] {
			if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}

// fit ports fit: drop lines from the top until the encoded URL fits.
func fit(prefix, head string, lines []string, max int) string {
	tail := lines
	build := func() string {
		end := ""
		if len(tail) > 0 {
			end = "\n```\n"
		}
		return prefix + EncodeURIComponent(head+strings.Join(tail, "\n")+end)
	}
	u := build()
	for len(tail) > 0 && len(u) > max {
		tail = tail[1:]
		u = build()
	}
	return u
}

// Build ports buildReport: an empty error reads Unknown error, the last 200 lines, every piece scrubbed.
func Build(errText string, log []Line, version, osName string) Result {
	if errText == "" {
		errText = "Unknown error"
	}
	e := Scrub(errText)
	if len(log) > 200 {
		log = log[len(log)-200:]
	}
	lines := make([]string, 0, len(log))
	for _, l := range log {
		lines = append(lines, Scrub(l.TS)+" ["+Scrub(l.Type)+"] "+Scrub(l.Msg))
	}
	title := jsjson.SliceUTF16("Export failed: "+e, 120)
	app := "v" + Scrub(version) + " · " + Scrub(osName)
	head := "**Error:** " + e + "\n\n**App:** " + app + "\n\n**Log (newest last):**\n```\n"
	mailHead := "Error: " + e + "\nApp: " + app + "\n\nLog:\n```\n"
	return Result{
		IssueURL:  fit("https://github.com/"+Repo+"/issues/new?title="+EncodeURIComponent(title)+"&body=", head, lines, issueURLMax),
		MailtoURL: fit("mailto:"+Email+"?subject="+EncodeURIComponent("Garmin Export: "+title)+"&body=", mailHead, lines, mailtoURLMax),
	}
}
