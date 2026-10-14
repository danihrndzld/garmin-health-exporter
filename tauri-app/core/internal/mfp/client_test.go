package mfp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
)

func testSession() *Session {
	return &Session{UserAgent: "test-agent", Cookies: []Cookie{{Name: "__Secure-next-auth.session-token", Value: "s1", Domain: ".myfitnesspal.com", Path: "/"}}}
}

func newTestClient(t *testing.T) (*Client, *mfptest.Server) {
	srv := mfptest.NewServer("testdata")
	t.Cleanup(srv.Close)
	return NewClient(testSession(), Options{Base: srv.URL}), srv
}

func TestAuthThenDiary(t *testing.T) {
	c, srv := newTestClient(t)
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := c.Diary(ctx, "2026-10-05")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Food.Description != `Pan "Bimbo", integral` {
		t.Fatalf("items = %+v", items)
	}
	if srv.CallCount("/v2/diary?") != 1 {
		t.Fatalf("calls = %v", srv.Calls)
	}
}

func TestExpiredSessionIsErrSession(t *testing.T) {
	c, srv := newTestClient(t)
	srv.AuthHTML = true
	if err := c.Auth(context.Background()); !errors.Is(err, ErrSession) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiary401RefreshesOnceAndRetries(t *testing.T) {
	c, srv := newTestClient(t)
	ctx := context.Background()
	_ = c.Auth(ctx)
	srv.Diary401Once = true
	if _, err := c.Diary(ctx, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	if srv.CallCount("/user/auth_token") != 2 {
		t.Fatalf("expected one refresh, calls = %v", srv.Calls)
	}
}

func TestDiary429StopsWithErrRateLimited(t *testing.T) {
	c, srv := newTestClient(t)
	ctx := context.Background()
	_ = c.Auth(ctx)
	srv.Diary429 = true
	if _, err := c.Diary(ctx, "2026-10-05"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if srv.CallCount("/v2/diary?") != 1 {
		t.Fatal("a 429 must not be retried")
	}
}

func TestRotatedCookiesAreMergedAndSaved(t *testing.T) {
	c, srv := newTestClient(t)
	srv.SetCookie = "__Secure-next-auth.session-token=s2; Path=/; Secure; HttpOnly"
	if err := c.Auth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !c.SessionChanged() || c.Session().Cookies[0].Value != "s2" {
		t.Fatalf("cookie not merged: %+v", c.Session().Cookies)
	}
	p := filepath.Join(t.TempDir(), "mfp_session.json")
	if err := c.Session().Save(p); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	back, err := LoadSession(p)
	if err != nil || back.Cookies[0].Value != "s2" {
		t.Fatalf("reload: %+v %v", back, err)
	}
}

func TestRequestsCarryTheSessionUserAgent(t *testing.T) {
	c, srv := newTestClient(t)
	_ = srv
	if c.userAgent() != "test-agent" {
		t.Fatal("user agent not taken from the session")
	}
}

func TestFakeServerEnvAcceptsOnlyLoopback(t *testing.T) {
	for v, want := range map[string]string{
		"http://127.0.0.1:8080":       "http://127.0.0.1:8080",
		"http://localhost:9":          "http://localhost:9",
		"http://127.0.0.1:1@evil.com": "",
		"https://127.0.0.1:8080":      "",
		"http://127.0.0.1:8080/x":     "",
		"http://example.com:80":       "",
	} {
		t.Setenv("MFP_FAKE_SERVER", v)
		if got := FakeServer(); got != want {
			t.Errorf("%q -> %q, want %q", v, got, want)
		}
	}
}
