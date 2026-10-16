package mfp

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
)

func TestAuthAnswerClasses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		setup   func(s *mfptest.Server)
		wantErr error  // matched with errors.Is; nil with wantMsg "" means success
		wantMsg string // substring expected in the error text for other failures
	}{
		{"EP-118 a JSON token answer signs in", func(s *mfptest.Server) {}, nil, ""},
		{"EP-119 an HTML page with HTTP 200 means the session expired", func(s *mfptest.Server) { s.AuthHTML = true }, ErrSession, ""},
		{"EP-120 a JSON answer without access_token means the session expired", func(s *mfptest.Server) { s.AuthRaw = `{"user_id":"42"}` }, ErrSession, ""},
		{"EP-121 a JSON content type with a broken body means the session expired", func(s *mfptest.Server) { s.AuthRaw = `{broken` }, ErrSession, ""},
		{"EP-122 HTTP 429 is a rate limit", func(s *mfptest.Server) { s.AuthStatus = 429 }, ErrRateLimited, ""},
		{"EP-123 HTTP 403 (a bot challenge) is not an expired session", func(s *mfptest.Server) { s.AuthStatus = 403 }, nil, "403"},
		{"EP-124 HTTP 503 is not an expired session", func(s *mfptest.Server) { s.AuthStatus = 503 }, nil, "503"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, srv := newTestClient(t)
			tc.setup(srv)
			err := c.Auth(context.Background())
			switch {
			case tc.wantErr == nil && tc.wantMsg == "":
				if err != nil {
					t.Fatal(err)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			default:
				if err == nil || errors.Is(err, ErrSession) || errors.Is(err, ErrRateLimited) || !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("err = %v, want a generic error mentioning %s", err, tc.wantMsg)
				}
			}
		})
	}
}

// Decision table over the diary answer (C1) and the answer of the token refresh that follows a 401 (C2).
func TestDiaryDecisionTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		setup      func(s *mfptest.Server)
		wantErr    error
		wantGeneri string // substring of a generic error
		diaryCalls int
		authCalls  int
	}{
		{"DT-R101 200 returns the day with no refresh", func(s *mfptest.Server) {}, nil, "", 1, 1},
		{"DT-R102 401 then a good refresh retries once and succeeds", func(s *mfptest.Server) { s.Diary401Once = true }, nil, "", 2, 2},
		{"DT-R103 401 again after a good refresh is an expired session, with no third try", func(s *mfptest.Server) { s.DiaryStatus = 401 }, ErrSession, "", 2, 2},
		{"DT-R104 401 and a refresh that answers HTML is an expired session, with no retry", func(s *mfptest.Server) { s.Diary401Once, s.AuthHTML = true, true }, ErrSession, "", 1, 2},
		{"DT-R105 429 stops with a rate limit and is never retried", func(s *mfptest.Server) { s.Diary429 = true }, ErrRateLimited, "", 1, 1},
		{"DT-R106 500 is a generic error, not a session or rate limit error", func(s *mfptest.Server) { s.DiaryStatus = 500 }, nil, "500", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, srv := newTestClient(t)
			ctx := context.Background()
			if err := c.Auth(ctx); err != nil {
				t.Fatal(err)
			}
			tc.setup(srv)
			items, err := c.Diary(ctx, "2026-10-05")
			switch {
			case tc.wantErr == nil && tc.wantGeneri == "":
				if err != nil || len(items) != 3 {
					t.Fatalf("items = %d, err = %v", len(items), err)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			default:
				if err == nil || errors.Is(err, ErrSession) || errors.Is(err, ErrRateLimited) || !strings.Contains(err.Error(), tc.wantGeneri) {
					t.Fatalf("err = %v", err)
				}
			}
			if got := srv.CallCount("/v2/diary?"); got != tc.diaryCalls {
				t.Errorf("diary calls = %d, want %d", got, tc.diaryCalls)
			}
			if got := srv.CallCount("/user/auth_token"); got != tc.authCalls {
				t.Errorf("auth_token calls = %d, want %d", got, tc.authCalls)
			}
		})
	}
}

func TestRotatedCookiesAreMergedAndSaved(t *testing.T) {
	t.Parallel()
	c, srv := newTestClient(t)
	srv.SetCookie = "__Secure-next-auth.session-token=s2; Path=/; Secure; HttpOnly"
	if err := c.Auth(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Run("ST-T101 a valid session that receives a new token is merged and flagged for rewriting", func(t *testing.T) {
		if !c.SessionChanged() || c.Session().Cookies[0].Value != "s2" {
			t.Fatalf("cookie not merged: %+v", c.Session().Cookies)
		}
	})
	t.Run("ST-T102 the rotated session survives a save and a reload", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "mfp_session.json")
		if err := c.Session().Save(p); err != nil {
			t.Fatal(err)
		}
		back, err := LoadSession(p)
		if err != nil || back.Cookies[0].Value != "s2" {
			t.Fatalf("reload: %+v %v", back, err)
		}
	})
}

func TestRequestShape(t *testing.T) {
	t.Parallel()
	c, srv := newTestClient(t)
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Diary(ctx, "2026-10-05"); err != nil { // the fake answers 401 unless Bearer, mfp-client-id and mfp-user-id are right
		t.Fatalf("diary rejected by the fake: %v", err)
	}
	t.Run("CL-108 every request carries the User-Agent saved with the session", func(t *testing.T) {
		agents := srv.Agents()
		if len(agents) != 2 {
			t.Fatalf("agents = %v", agents)
		}
		for _, ua := range agents {
			if ua != "test-agent" {
				t.Fatalf("request sent User-Agent %q", ua)
			}
		}
	})
	t.Run("CL-109 the diary query has entry_date and exactly the three confirmed types", func(t *testing.T) {
		var q url.Values
		for _, call := range srv.Calls {
			if strings.HasPrefix(call, "/v2/diary?") {
				q, _ = url.ParseQuery(strings.TrimPrefix(call, "/v2/diary?"))
			}
		}
		if q.Get("entry_date") != "2026-10-05" || q.Get("types") != "food_entry,exercise_entry,steps_aggregate" {
			t.Fatalf("query = %v", q)
		}
	})
	t.Run("CL-110 auth_token is called with refresh=true", func(t *testing.T) {
		if srv.CallCount("/user/auth_token?refresh=true") != 1 {
			t.Fatalf("calls = %v", srv.Calls)
		}
	})
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

func TestPacing(t *testing.T) {
	t.Run("BVA-118 a request starts no sooner than the gap after the previous one ended", func(t *testing.T) {
		t.Parallel()
		srv := mfptest.NewServer("testdata")
		t.Cleanup(srv.Close)
		tt := &timedTransport{}
		c := NewClient(testSession(), Options{Base: srv.URL, Gap: 50 * time.Millisecond, HTTP: &http.Client{Transport: tt}})
		ctx := context.Background()
		if err := c.Auth(ctx); err != nil {
			t.Fatal(err)
		}
		for _, d := range []string{"2026-10-05", "2026-10-06"} {
			if _, err := c.Diary(ctx, d); err != nil {
				t.Fatal(err)
			}
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
	})
	t.Run("BVA-119 the default gap against the real service is one second", func(t *testing.T) {
		if g := NewClient(testSession(), Options{}).opts.Gap; g != time.Second {
			t.Fatalf("gap = %v", g)
		}
	})
}

func TestGoals(t *testing.T) {
	t.Parallel()
	t.Run("CL-111 the goals map to goal energy, protein, carbohydrates, fat, sodium and sugar", func(t *testing.T) {
		c, _ := newTestClient(t)
		ctx := context.Background()
		if err := c.Auth(ctx); err != nil {
			t.Fatal(err)
		}
		g, err := c.Goals(ctx, "2026-10-05")
		if err != nil {
			t.Fatal(err)
		}
		for name, p := range map[string]struct {
			got  *float64
			want float64
		}{"energy": {g.EnergyKcal, 2200}, "protein": {g.Protein, 150}, "carbs": {g.Carbohydrate, 220}, "fat": {g.Fat, 73}, "sodium": {g.Sodium, 2300}, "sugar": {g.Sugar, 50}} {
			if p.got == nil || *p.got != p.want {
				t.Errorf("%s = %v, want %v", name, p.got, p.want)
			}
		}
	})
	bad := []struct{ name, body string }{
		{"EG-113 goals that are not JSON are an error the caller can ignore", "{not json"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "goals.json"), []byte(tc.body), 0o600)
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
		})
	}
	t.Run("EP-125 a goal energy in kilojoules is converted to kcal", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "goals.json"), []byte(`{"items":[{"default_goal":{"energy":{"unit":"kilojoules","value":9205}}}]}`), 0o600)
		srv := mfptest.NewServer(dir)
		t.Cleanup(srv.Close)
		c := NewClient(testSession(), Options{Base: srv.URL})
		if err := c.Auth(context.Background()); err != nil {
			t.Fatal(err)
		}
		g, err := c.Goals(context.Background(), "2026-10-05")
		if err != nil || g.EnergyKcal == nil || math.Round(*g.EnergyKcal) != 2200 {
			t.Fatalf("goals = %+v, err = %v", g, err)
		}
	})
	t.Run("EG-114 an empty goals list gives empty goals, not an error", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "goals.json"), []byte(`{"items":[]}`), 0o600)
		srv := mfptest.NewServer(dir)
		t.Cleanup(srv.Close)
		c := NewClient(testSession(), Options{Base: srv.URL})
		if err := c.Auth(context.Background()); err != nil {
			t.Fatal(err)
		}
		g, err := c.Goals(context.Background(), "2026-10-05")
		if err != nil || g.EnergyKcal != nil || g.Protein != nil {
			t.Fatalf("goals = %+v, err = %v", g, err)
		}
	})
}

func TestUserIDForms(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, userID, want string }{
		{"EG-115 a numeric user_id (not a string) is sent as is", "123456789", "123456789"},
		{"EG-116 a string user_id loses its quotes in the mfp-user-id header", `"42"`, "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, srv := newTestClient(t)
			srv.UserID = tc.userID
			ctx := context.Background()
			if err := c.Auth(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Diary(ctx, "2026-10-05"); err != nil { // the fake answers 401 on a wrong mfp-user-id
				t.Fatal(err)
			}
			if c.userID != tc.want {
				t.Fatalf("userID = %q", c.userID)
			}
		})
	}
}

func TestCookieHeader(t *testing.T) {
	t.Parallel()
	now := time.Now().Unix()
	cases := []struct {
		name    string
		cookies []Cookie
		want    string
	}{
		{"EP-138 an expired cookie is not sent", []Cookie{{Name: "old", Value: "1", Expires: now - 60}}, ""},
		{"EP-139 a cookie that has not expired is sent", []Cookie{{Name: "live", Value: "2", Expires: now + 3600}}, "live=2"},
		{"EP-140 a session cookie (Expires 0) is sent", []Cookie{{Name: "session", Value: "3"}}, "session=3"},
		{"EP-141 cookies are joined with a semicolon in stored order", []Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}, "a=1; b=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := NewClient(&Session{Cookies: tc.cookies}, Options{Base: "http://127.0.0.1:1"})
			if got := c.cookieHeader(); got != tc.want {
				t.Fatalf("cookieHeader = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestACookieValueWithLineBreaksNeverReachesTheServer(t *testing.T) {
	t.Parallel()
	srv := mfptest.NewServer("testdata")
	t.Cleanup(srv.Close)
	s := testSession()
	s.Cookies[0].Value = "abc\r\nX-Injected: 1"
	t.Run("EG-117 a malformed cookie value fails the request and nothing is sent", func(t *testing.T) {
		if err := NewClient(s, Options{Base: srv.URL}).Auth(context.Background()); err == nil {
			t.Fatal("a cookie with CRLF was sent")
		}
		if n := srv.CallCount("/"); n != 0 {
			t.Fatalf("server saw %d requests", n)
		}
	})
}
