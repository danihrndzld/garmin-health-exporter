package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp"
	"github.com/danihrndzld/gconnect"
)

// No t.Parallel: t.Setenv forbids it.
func TestFakeServerAcceptsLoopbackOnly(t *testing.T) {
	for _, c := range []struct{ name, v, want string }{
		{"EP-211 http on 127.0.0.1 with a port is accepted", "http://127.0.0.1:4100", "http://127.0.0.1:4100"},
		{"EP-212 http on localhost with a port is accepted", "http://localhost:9", "http://localhost:9"},
		{"EP-213 https is rejected", "https://127.0.0.1:4100", ""},
		{"EP-214 a host that is not loopback is rejected", "http://evil.com:80", ""},
		{"EP-215 a URL with a path is rejected", "http://127.0.0.1:4100/x", ""},
		{"EP-216 an empty value means the real Garmin", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GARMIN_FAKE_SERVER", c.v)
			wantHosts := gconnect.Hosts{}
			if c.want != "" {
				wantHosts = gconnect.SameHost(c.want)
			}
			if got := garminHosts(); got != wantHosts {
				t.Errorf("%q -> %+v", c.v, got)
			}
		})
	}
}

// These tests set an environment variable, so t.Parallel is not allowed in them.
func TestFakeServerEnv(t *testing.T) {
	cases := []struct{ name, env, want string }{
		{"EP-126 http on 127.0.0.1 with a port is accepted", "http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"EP-127 http on localhost with a port is accepted", "http://localhost:9", "http://localhost:9"},
		{"EP-128 userinfo that hides another host is rejected", "http://127.0.0.1:1@evil.com", ""},
		{"EP-129 https is rejected", "https://127.0.0.1:8080", ""},
		{"EP-130 a path after the port is rejected", "http://127.0.0.1:8080/x", ""},
		{"EP-131 a non loopback host is rejected", "http://example.com:80", ""},
		{"EP-132 a host that only starts with localhost is rejected", "http://localhost.evil.com:80", ""},
		{"EP-133 the IPv6 loopback is rejected (the spec allows 127.0.0.1 and localhost only)", "http://[::1]:80", ""},
		{"EP-134 a loopback URL without a port is rejected", "http://127.0.0.1", ""},
		{"EP-135 an unset variable means the real service", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MFP_FAKE_SERVER", tc.env)
			if got := fakeServer("MFP_FAKE_SERVER"); got != tc.want {
				t.Fatalf("%q -> %q, want %q", tc.env, got, tc.want)
			}
		})
	}
	t.Run("EP-136 a rejected MFP_FAKE_SERVER leaves both hosts on the real service", func(t *testing.T) {
		t.Setenv("MFP_FAKE_SERVER", "http://example.com:80")
		if r := mfpRequest(map[string]any{}); r.Base != "" || r.Gap != time.Second {
			t.Fatalf("base = %q, gap = %v", r.Base, r.Gap)
		}
	})
	t.Run("EP-137 an accepted MFP_FAKE_SERVER redirects both hosts and drops the pause", func(t *testing.T) {
		t.Setenv("MFP_FAKE_SERVER", "http://127.0.0.1:4321")
		if r := mfpRequest(map[string]any{}); r.Base != "http://127.0.0.1:4321" || r.Gap != 0 {
			t.Fatalf("base = %q, gap = %v", r.Base, r.Gap)
		}
	})
}

func TestGuardMFP(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, ".guard-mfp-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ok := mfp.ExportRequest{StartDate: "2026-10-01", EndDate: "2026-10-07", DataDir: dir, CSVDir: dir}
	t.Run("EP-227 cmd refuses a bad range, a dataDir or a csvDir outside $HOME with MFP_INPUT before mfp.Export runs", func(t *testing.T) {
		for _, set := range []func(*mfp.ExportRequest){
			func(r *mfp.ExportRequest) { r.StartDate = "2026-07-01" },
			func(r *mfp.ExportRequest) { r.EndDate = "07/10/2026" },
			func(r *mfp.ExportRequest) { r.DataDir = "/etc" },
			func(r *mfp.ExportRequest) { r.CSVDir = "" },
		} {
			r := ok
			set(&r)
			var ce *mfp.CodeError
			if _, err := guardMFP(r); !errors.As(err, &ce) || ce.Code != "MFP_INPUT" {
				t.Errorf("%+v -> %v", r, err)
			}
		}
	})
	t.Run("CL-272 cmd passes mfp.Export the dirs that guard resolved", func(t *testing.T) {
		want, _ := filepath.EvalSymlinks(dir)
		r, err := guardMFP(ok)
		if err != nil || r.DataDir != want || r.CSVDir != want {
			t.Fatalf("%+v %v, want %s", r, err, want)
		}
	})
}
