package main

import (
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
)

// No t.Parallel: t.Setenv forbids it.
func TestFakeServerAcceptsLoopbackOnly(t *testing.T) {
	for v, want := range map[string]string{
		"http://127.0.0.1:4100":   "http://127.0.0.1:4100", // EP-211
		"http://localhost:9":      "http://localhost:9",    // EP-212
		"https://127.0.0.1:4100":  "",                      // EP-213
		"http://evil.com:80":      "",                      // EP-214
		"http://127.0.0.1:4100/x": "",                      // EP-215
		"":                        "",                      // EP-216
	} {
		t.Setenv("GARMIN_FAKE_SERVER", v)
		wantHosts := gconnect.Hosts{}
		if want != "" {
			wantHosts = gconnect.SameHost(want)
		}
		if got := garminHosts(); got != wantHosts {
			t.Errorf("%q -> %+v", v, got)
		}
	}
}
