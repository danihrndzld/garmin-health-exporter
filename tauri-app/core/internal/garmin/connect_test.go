package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect/gconnecttest"
)

func fakeGarmin(t *testing.T) (*gconnecttest.Server, *gconnect.Auth) {
	t.Helper()
	srv := gconnecttest.New()
	t.Cleanup(srv.Close)
	return srv, &gconnect.Auth{Hosts: srv.Hosts()}
}

func tokensFileOf(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func connect(a *gconnect.Auth, dir string, now time.Time) error {
	_, err := Connect(context.Background(), a, dir, "e2e@example.invalid", "right-password", now)
	return err
}

func codeOf(err error) string {
	var ce *CodeError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestConnectMapsLoginCodesAndWritesFiles(t *testing.T) {
	srv, a := fakeGarmin(t)
	t.Run("DT-R223 Connect answers the GARMIN code of each sign in result, saves tokens.json only on success and writes the cooldown only on a rate limit", func(t *testing.T) {
		for _, c := range []struct {
			set      func(*gconnecttest.Control)
			password string
			code     string
		}{
			{nil, "right-password", ""},
			{nil, "nope", "GARMIN_BAD_PASSWORD"},
			{func(c *gconnecttest.Control) { c.MobileLogin = "mfa" }, "right-password", "GARMIN_MFA"},
			{func(c *gconnecttest.Control) { c.DIAuth = "fail" }, "right-password", ""},
			{func(c *gconnecttest.Control) { c.DIAuth = "429" }, "right-password", "GARMIN_RATE_LIMITED"},
			{func(c *gconnecttest.Control) { c.MobileLogin = "cloudflare"; c.WidgetStatus = 403 }, "right-password", "GARMIN_BLOCKED"},
		} {
			srv.Reset()
			if c.set != nil {
				srv.Set(c.set)
			}
			dir := t.TempDir()
			s, err := Connect(context.Background(), a, dir, "e2e@example.invalid", c.password, time.Now())
			if codeOf(err) != c.code || (err == nil) != (c.code == "") {
				t.Fatalf("%s: got %v, want %q", c.code, err, c.code)
			}
			if _, err := LoadTokens(dir); (err == nil) != (c.code == "") || s.Connected != (c.code == "") {
				t.Fatalf("%s: tokens.json %v, status %+v", c.code, err, s)
			}
			_, cerr := os.Stat(filepath.Join(dir, "sso-cooldown.json"))
			if cooled := cerr == nil; cooled != (c.code == "GARMIN_RATE_LIMITED") {
				t.Fatalf("%s: cooldown file exists: %v", c.code, cooled)
			}
		}
	})
}

func TestLoginCodeMapping(t *testing.T) {
	t.Run("DT-R224 every LoginCode maps to its GARMIN code, and an unknown one to GARMIN_ERROR", func(t *testing.T) {
		for c, want := range map[gconnect.LoginCode]string{
			gconnect.BadPassword: "GARMIN_BAD_PASSWORD", gconnect.MFARequired: "GARMIN_MFA", gconnect.RateLimited: "GARMIN_RATE_LIMITED",
			gconnect.Blocked: "GARMIN_BLOCKED", gconnect.LoginFailed: "GARMIN_ERROR", "new_code": "GARMIN_ERROR", "": "GARMIN_ERROR",
		} {
			if got := loginErrorCode(c); got != want {
				t.Errorf("%q -> %q, want %q", c, got, want)
			}
		}
	})
}

func TestLoginRateLimitedTwiceStartsCooldown(t *testing.T) {
	t.Run("DT-R219 ports DT-R9: two 429s start the cooldown and the next login sends nothing", func(t *testing.T) {
		srv, a := fakeGarmin(t)
		srv.Set(func(c *gconnecttest.Control) { c.MobileLogin, c.Widget429 = "429", true })
		dir := t.TempDir()
		err := connect(a, dir, time.Now())
		if codeOf(err) != "GARMIN_RATE_LIMITED" {
			t.Fatalf("DT-R219 ports DT-R9: got %v", err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "sso-cooldown.json"))
		if err != nil {
			t.Fatalf("DT-R219 no cooldown file: %v", err)
		}
		var cd struct{ Until int64 }
		_ = json.Unmarshal(b, &cd)
		if d := cd.Until - time.Now().Unix(); d < 895 || d > 900 {
			t.Fatalf("DT-R219 cooldown until is %d s ahead", d)
		}
		srv.Reset()
		err = connect(a, dir, time.Now())
		if codeOf(err) != "GARMIN_RATE_LIMITED" || !strings.Contains(err.Error(), "15 more minutes") {
			t.Fatalf("DT-R219 second login: %v", err)
		}
		if n := len(srv.Calls("")); n != 0 {
			t.Fatalf("DT-R219 %d requests during the cooldown", n)
		}
	})
}

func TestLoginCooldownWrittenByBun(t *testing.T) {
	srv, a := fakeGarmin(t)
	now := time.Now()
	cooldown := func(dir string, until int64) {
		if err := os.WriteFile(filepath.Join(dir, "sso-cooldown.json"), []byte(`{"until":`+itoa(until)+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("DT-R221 ports DT-R11: a cooldown file written by Bun blocks Go", func(t *testing.T) {
		srv.Reset()
		dir := t.TempDir()
		cooldown(dir, now.Unix()+600)
		err := connect(a, dir, time.Now())
		if codeOf(err) != "GARMIN_RATE_LIMITED" || !strings.Contains(err.Error(), "10 more minutes") {
			t.Fatalf("got %v", err)
		}
		if n := len(srv.Calls("")); n != 0 {
			t.Fatalf("%d requests during the cooldown", n)
		}
	})
	t.Run("DT-R222 a cooldown a year ahead blocks for at most 15 minutes", func(t *testing.T) {
		srv.Reset()
		dir := t.TempDir()
		cooldown(dir, now.Unix()+365*86400)
		err := connect(a, dir, time.Now())
		if codeOf(err) != "GARMIN_RATE_LIMITED" || !strings.Contains(err.Error(), "15 more minutes") {
			t.Fatalf("got %v", err)
		}
		if err := connect(a, dir, time.Now().Add(15*time.Minute+time.Second)); err != nil {
			t.Fatalf("15 minutes later: %v", err)
		}
		if len(srv.Calls("/mobile/api/login")) != 1 {
			t.Fatal("15 minutes later the login sent no request")
		}
	})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestFreshRefreshesDIBeforeExpiry(t *testing.T) {
	t.Run("ST-T201 ports ST-T1: a DI token near expiry is refreshed once", func(t *testing.T) {
		srv, a := fakeGarmin(t)
		srv.Set(func(c *gconnecttest.Control) { c.TokenTTL = 600 }) // inside the 15 minute margin
		dir := t.TempDir()
		if err := connect(a, dir, time.Now()); err != nil {
			t.Fatal(err)
		}
		before, _ := readRaw(dir)
		srv.Reset()
		got, err := Fresh(context.Background(), a, dir)
		if err != nil {
			t.Fatalf("ST-T201 ports ST-T1: %v", err)
		}
		calls := srv.Calls("/di-oauth2-service/oauth/token")
		if len(calls) != 1 || !strings.Contains(calls[0].Body, "grant_type=refresh_token") {
			t.Fatalf("ST-T201 refresh calls: %+v", calls)
		}
		onDisk, _ := readRaw(dir)
		if got.DI.AccessToken == before.DI.AccessToken || onDisk.DI.AccessToken != got.DI.AccessToken {
			t.Fatal("ST-T201 tokens.json does not hold the new access token")
		}
		if onDisk.SavedAt != before.SavedAt {
			t.Fatalf("ST-T201 a refresh changed saved_at: %q to %q", before.SavedAt, onDisk.SavedAt)
		}
	})
}

func TestFreshKeepsTokensOnServerOrNetworkError(t *testing.T) {
	srv, a := fakeGarmin(t)
	srv.Set(func(c *gconnecttest.Control) { c.TokenTTL = 600 })
	dir := t.TempDir()
	if err := connect(a, dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	before := tokensFileOf(t, dir)
	srv.Set(func(c *gconnecttest.Control) { c.RefreshFail = 500 })
	for _, step := range []string{"ST-T203 a 500 on the refresh grant", "ST-T212 a closed network"} {
		t.Run(step, func(t *testing.T) {
			_, err := Fresh(context.Background(), a, dir)
			if err == nil || errors.Is(err, gconnect.ErrSessionDead) || errors.Is(err, ErrNoLogin) {
				t.Fatalf("%s: got %v, want a plain error", step, err)
			}
			if !bytes.Equal(tokensFileOf(t, dir), before) {
				t.Fatalf("%s changed tokens.json", step)
			}
		})
		srv.Close()
	}
}

func TestRefreshDeadOnlyOnProvenAnswers(t *testing.T) {
	srv, a := fakeGarmin(t)
	legacy := `{"oauth1":{"oauth_token":"ot","oauth_token_secret":"os"},"oauth2":{"access_token":"old","expires_at":1,"refresh_token_expires_at":1},"consumer":{"consumer_key":"ck","consumer_secret":"cs"}}`
	di := `{"di":{"access_token":"x","refresh_token":"refresh-0","client_id":"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2","expires_at":1}}`
	cases := []struct {
		id, body string
		status   int
		dead     bool
	}{
		{"ST-T204 the DI refresh grant answers 400", di, 400, true},
		{"ST-T205 the DI refresh grant answers 401", di, 401, true},
		{"ST-T206 the DI refresh grant answers 403", di, 403, false},
		{"ST-T207 the DI refresh grant answers 429", di, 429, false},
		{"ST-T208 the legacy exchange answers 401", legacy, 401, true},
		{"ST-T209 the legacy exchange answers 400", legacy, 400, false},
		{"ST-T210 the legacy exchange answers 500", legacy, 500, false},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			srv.Reset()
			srv.Set(func(k *gconnecttest.Control) { k.RefreshFail = c.status })
			dir := t.TempDir()
			write(t, dir, c.body)
			_, err := Fresh(context.Background(), a, dir)
			if err == nil || errors.Is(err, gconnect.ErrSessionDead) != c.dead {
				t.Fatalf("got %v, dead want %v", err, c.dead)
			}
			if c.dead {
				if err := MarkExpired(dir, time.Now()); err != nil {
					t.Fatal(err)
				}
				if _, err := LoadTokens(dir); !errors.Is(err, ErrNoLogin) {
					t.Fatalf("after the marker: %v", err)
				}
			} else if string(tokensFileOf(t, dir)) != c.body {
				t.Fatal("tokens.json changed")
			}
		})
	}
}
