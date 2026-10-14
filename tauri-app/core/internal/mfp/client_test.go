package mfp

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Diary(ctx, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	agents := srv.Agents()
	if len(agents) != 2 {
		t.Fatalf("agents = %v", agents)
	}
	for _, ua := range agents {
		if ua != "test-agent" {
			t.Fatalf("request sent User-Agent %q", ua)
		}
	}
}

// timedTransport records when each request starts and when its body is closed.
type timedTransport struct {
	mu     sync.Mutex
	starts []time.Time
	ends   []time.Time
}

func (tt *timedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tt.mu.Lock()
	tt.starts = append(tt.starts, time.Now())
	tt.mu.Unlock()
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = &endRecorder{ReadCloser: resp.Body, tt: tt}
	return resp, nil
}

type endRecorder struct {
	io.ReadCloser
	tt *timedTransport
}

func (e *endRecorder) Close() error {
	e.tt.mu.Lock()
	e.tt.ends = append(e.tt.ends, time.Now())
	e.tt.mu.Unlock()
	return e.ReadCloser.Close()
}

func TestRequestsArePacedFromTheEndOfTheLastOne(t *testing.T) {
	srv := mfptest.NewServer("testdata")
	t.Cleanup(srv.Close)
	tt := &timedTransport{}
	c := NewClient(testSession(), Options{Base: srv.URL, Gap: 50 * time.Millisecond, HTTP: &http.Client{Transport: tt}})
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Diary(ctx, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Diary(ctx, "2026-10-06"); err != nil {
		t.Fatal(err)
	}
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if len(tt.starts) != 3 || len(tt.ends) != 3 {
		t.Fatalf("starts = %d, ends = %d", len(tt.starts), len(tt.ends))
	}
	for i := 1; i < 3; i++ {
		if gap := tt.starts[i].Sub(tt.ends[i-1]); gap < 50*time.Millisecond {
			t.Errorf("request %d started %v after the previous one ended", i, gap)
		}
	}
}

func TestDefaultGapIsOneSecondAgainstTheRealService(t *testing.T) {
	t.Setenv("MFP_FAKE_SERVER", "")
	if g := NewClient(testSession(), Options{}).opts.Gap; g != time.Second {
		t.Fatalf("gap = %v", g)
	}
}

func TestGoalsFromTheFakeServer(t *testing.T) {
	c, _ := newTestClient(t)
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	g, err := c.Goals(ctx, "2026-10-05")
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, got *float64, want float64) {
		if got == nil || *got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("EnergyKcal", g.EnergyKcal, 2200)
	check("Protein", g.Protein, 150)
	check("Carbohydrate", g.Carbohydrate, 220)
	check("Fat", g.Fat, 73)
	check("Sodium", g.Sodium, 2300)
	check("Sugar", g.Sugar, 50)
}

func TestGoalsInvalidJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "goals.json"), []byte("{not json"), 0o600)
	srv := mfptest.NewServer(dir)
	t.Cleanup(srv.Close)
	c := NewClient(testSession(), Options{Base: srv.URL})
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Goals(ctx, "2026-10-05"); err == nil {
		t.Fatal("invalid goals JSON accepted")
	}
}

func TestKilojoulesAreConvertedToKcal(t *testing.T) {
	c, _ := newTestClient(t)
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := c.Diary(ctx, "2026-10-07")
	if err != nil {
		t.Fatal(err)
	}
	k := items[0].Nutrients.Energy.Kcal()
	if k == nil || math.Round(*k*10)/10 != 104.9 {
		t.Fatalf("kcal = %v", k)
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
