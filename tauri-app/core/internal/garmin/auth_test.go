package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin/garmintest"
)

// Tests that sign in point GARMIN_FAKE_SERVER at a fake, so they cannot use t.Parallel.
func fakeGarmin(t *testing.T) *garmintest.Server {
	t.Helper()
	srv := garmintest.New(nil)
	t.Cleanup(srv.Close)
	t.Setenv("GARMIN_FAKE_SERVER", srv.URL())
	return srv
}

func posts(calls []garmintest.Call) int {
	n := 0
	for _, c := range calls {
		if c.Method == "POST" {
			n++
		}
	}
	return n
}

func tokensFileOf(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoginDecisionTable(t *testing.T) {
	cases := []struct {
		id                 string
		set                func(*garmintest.Control)
		password           string
		wantCode, wantKind string // wantCode "" means success
		widgetPosts        int
		diCalls            int // -1: not checked
	}{
		{"DT-R211 ports DT-R1: mobile and DI succeed", nil, "right-password", "", "di", 0, 1},
		{"DT-R212 ports DT-R2: wrong password stops the chain", nil, "nope", "GARMIN_BAD_PASSWORD", "", 0, 0},
		{"DT-R213 ports DT-R3: MFA stops the chain", func(c *garmintest.Control) { c.MobileLogin = "mfa" }, "right-password", "GARMIN_MFA", "", 0, 0},
		{"DT-R214 ports DT-R4: 429 on mobile, legacy signs in", func(c *garmintest.Control) { c.MobileLogin = "429" }, "right-password", "", "legacy", 1, 0},
		{"DT-R215 ports DT-R5: Cloudflare 403 on mobile, legacy signs in", func(c *garmintest.Control) { c.MobileLogin = "cloudflare" }, "right-password", "", "legacy", 1, 0},
		{"DT-R216 ports DT-R6: captcha on mobile, legacy signs in", func(c *garmintest.Control) { c.MobileLogin = "captcha" }, "right-password", "", "legacy", 1, 0},
		{"DT-R217 ports DT-R7: DI refused for every client id, legacy signs in", func(c *garmintest.Control) { c.DIAuth = "fail" }, "right-password", "", "legacy", 1, 4},
		{"DT-R218 ports DT-R8: DI answers 429, no legacy", func(c *garmintest.Control) { c.DIAuth = "429" }, "right-password", "GARMIN_RATE_LIMITED", "", 0, 1},
		{"DT-R220 ports DT-R10: both logins blocked (403)", func(c *garmintest.Control) { c.MobileLogin = "cloudflare"; c.WidgetStatus = 403 }, "right-password", "GARMIN_BLOCKED", "", 1, 0},
	}
	srv := fakeGarmin(t)
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			srv.Reset()
			if c.set != nil {
				c.set(srv.Control())
			}
			dir := t.TempDir()
			var logs []string
			a := &Auth{DataDir: dir, Log: func(m string) { logs = append(logs, m) }}
			tok, err := a.Login(context.Background(), "e2e@example.invalid", c.password)
			if c.wantCode == "" {
				if err != nil {
					t.Fatalf("login failed: %v", err)
				}
				if (c.wantKind == "di") != (tok.DI != nil) {
					t.Fatalf("kind: got DI=%v", tok.DI != nil)
				}
				if _, err := LoadTokens(dir); err != nil {
					t.Fatalf("tokens.json not written: %v", err)
				}
			} else {
				var le *LoginError
				if !errors.As(err, &le) || le.Code != c.wantCode {
					t.Fatalf("got %v, want %s", err, c.wantCode)
				}
				if strings.Contains(le.Msg, c.password) {
					t.Fatal("the error holds the password")
				}
			}
			for _, l := range logs {
				if strings.Contains(l, c.password) {
					t.Fatalf("a log line holds the password: %q", l)
				}
			}
			if n := posts(srv.Calls("/sso/signin")); n != c.widgetPosts {
				t.Fatalf("widget sign in posts: %d, want %d", n, c.widgetPosts)
			}
			if c.widgetPosts == 0 && len(srv.Calls("/sso/embed")) != 0 {
				t.Fatal("a widget request was sent")
			}
			if n := len(srv.Calls("/mobile/api/login")); n != 1 {
				t.Fatalf("mobile logins: %d, want 1", n)
			}
			if n := len(srv.Calls("/di-oauth2-service/oauth/token")); c.diCalls >= 0 && n != c.diCalls {
				t.Fatalf("diauth calls: %d, want %d", n, c.diCalls)
			}
			_, cerr := os.Stat(filepath.Join(dir, "sso-cooldown.json"))
			if cooled := cerr == nil; cooled != (c.wantCode == "GARMIN_RATE_LIMITED") {
				t.Fatalf("cooldown file exists: %v", cooled)
			}
		})
	}
}

func TestLoginRateLimitedTwiceStartsCooldown(t *testing.T) {
	t.Run("DT-R219 ports DT-R9: two 429s start the cooldown and the next login sends nothing", func(t *testing.T) {
		srv := fakeGarmin(t)
		srv.Control().MobileLogin = "429"
		srv.Control().Widget429 = true
		dir := t.TempDir()
		a := &Auth{DataDir: dir}
		_, err := a.Login(context.Background(), "e2e@example.invalid", "right-password")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != "GARMIN_RATE_LIMITED" {
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
		_, err = a.Login(context.Background(), "e2e@example.invalid", "right-password")
		if !errors.As(err, &le) || le.Code != "GARMIN_RATE_LIMITED" || !strings.Contains(le.Msg, "15 more minutes") {
			t.Fatalf("DT-R219 second login: %v", err)
		}
		if n := len(srv.Calls("")); n != 0 {
			t.Fatalf("DT-R219 %d requests during the cooldown", n)
		}
	})
}

func TestLoginCooldownWrittenByBun(t *testing.T) {
	srv := fakeGarmin(t)
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
		_, err := (&Auth{DataDir: dir}).Login(context.Background(), "e2e@example.invalid", "right-password")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != "GARMIN_RATE_LIMITED" || !strings.Contains(le.Msg, "10 more minutes") {
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
		_, err := (&Auth{DataDir: dir}).Login(context.Background(), "e2e@example.invalid", "right-password")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != "GARMIN_RATE_LIMITED" || !strings.Contains(le.Msg, "15 more minutes") {
			t.Fatalf("got %v", err)
		}
		later := &Auth{DataDir: dir, Now: func() time.Time { return time.Now().Add(15*time.Minute + time.Second) }}
		if _, err := later.Login(context.Background(), "e2e@example.invalid", "right-password"); err != nil {
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
		srv := fakeGarmin(t)
		srv.Control().TokenTTL = 600 // inside the 15 minute margin
		dir := t.TempDir()
		a := &Auth{DataDir: dir}
		before, err := a.Login(context.Background(), "e2e@example.invalid", "right-password")
		if err != nil {
			t.Fatal(err)
		}
		srv.Reset()
		got, err := a.Fresh(context.Background())
		if err != nil {
			t.Fatalf("ST-T201 ports ST-T1: %v", err)
		}
		calls := srv.Calls("/di-oauth2-service/oauth/token")
		if len(calls) != 1 || !strings.Contains(calls[0].Body, "grant_type=refresh_token") {
			t.Fatalf("ST-T201 refresh calls: %+v", calls)
		}
		onDisk, _ := LoadTokens(dir)
		if got.DI.AccessToken == before.DI.AccessToken || onDisk.DI.AccessToken != got.DI.AccessToken {
			t.Fatal("ST-T201 tokens.json does not hold the new access token")
		}
		if onDisk.SavedAt != before.SavedAt {
			t.Fatalf("ST-T201 a refresh changed saved_at: %q to %q", before.SavedAt, onDisk.SavedAt)
		}
	})
}

func TestFreshRenewsLegacyThroughOAuth1(t *testing.T) {
	t.Run("ST-T202 ports ST-T2: an expired legacy token renews through the OAuth1 exchange", func(t *testing.T) {
		srv := fakeGarmin(t)
		dir := t.TempDir()
		past := itoa(time.Now().Unix() - 10)
		write(t, dir, `{"oauth1":{"oauth_token":"ot","oauth_token_secret":"os"},"oauth2":{"access_token":"old","expires_at":`+past+`,"refresh_token_expires_at":`+past+`},"consumer":{"consumer_key":"ck","consumer_secret":"cs"}}`)
		got, err := (&Auth{DataDir: dir}).Fresh(context.Background())
		if err != nil || got.OAuth2.AccessToken == "old" {
			t.Fatalf("ST-T202 ports ST-T2: %v", err)
		}
		if len(srv.Calls("/oauth-service/oauth/exchange/user/2.0")) != 1 || len(srv.Calls("/mobile/api/login")) != 0 {
			t.Fatal("ST-T202 the legacy token was not renewed through the OAuth1 exchange alone")
		}
	})
}

func TestFreshKeepsTokensOnServerOrNetworkError(t *testing.T) {
	srv := fakeGarmin(t)
	srv.Control().TokenTTL = 600
	dir := t.TempDir()
	a := &Auth{DataDir: dir}
	if _, err := a.Login(context.Background(), "e2e@example.invalid", "right-password"); err != nil {
		t.Fatal(err)
	}
	before := tokensFileOf(t, dir)
	srv.Control().RefreshFail = 500
	for _, step := range []string{"ST-T203 a 500 on the refresh grant", "ST-T203 a closed network"} {
		t.Run(step, func(t *testing.T) {
			_, err := a.Fresh(context.Background())
			if err == nil || errors.Is(err, ErrSessionDead) || errors.Is(err, ErrNoLogin) {
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
	srv := fakeGarmin(t)
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
			srv.Control().RefreshFail = c.status
			dir := t.TempDir()
			write(t, dir, c.body)
			_, err := (&Auth{DataDir: dir}).Fresh(context.Background())
			if err == nil || errors.Is(err, ErrSessionDead) != c.dead {
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

func TestRefreshWithoutRefreshTokenSendsNothing(t *testing.T) {
	srv := fakeGarmin(t)
	dir := t.TempDir()
	write(t, dir, `{"di":{"access_token":"x","client_id":"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2","expires_at":1}}`)
	t.Run("ST-T211 a DI tokens.json without refresh_token is dead and sends nothing", func(t *testing.T) {
		_, err := (&Auth{DataDir: dir}).Fresh(context.Background())
		if !errors.Is(err, ErrSessionDead) {
			t.Fatalf("got %v, want ErrSessionDead", err)
		}
		if n := len(srv.Calls("")); n != 0 {
			t.Fatalf("%d requests", n)
		}
	})
}

func TestJWTHelperReadable(t *testing.T) {
	t.Run("EG-221 ports EG-06: the JWT claims are readable", func(t *testing.T) {
		c := jwtClaims(garmintest.JWT(map[string]any{"exp": 5, "client_id": "X"}))
		if c.ClientID != "X" || c.Exp != 5 {
			t.Fatalf("EG-221 ports EG-06: got %+v", c)
		}
	})
}

func TestSignOAuth1MatchesAuthJs(t *testing.T) {
	t.Run("CL-222 the OAuth1 signature matches auth.js", func(t *testing.T) {
		params := map[string]string{
			"oauth_consumer_key": "ck", "oauth_nonce": "n", "oauth_signature_method": "HMAC-SHA1",
			"oauth_timestamp": "1", "oauth_version": "1.0", "ticket": "ST-1",
			"login-url": "https://sso.garmin.com/sso/embed", "accepts-mfa-tokens": "true",
		}
		// node -e "console.log(require('./electron-app/garmin/auth').signOAuth1(...))", see the task 3 brief.
		if got := signOAuth1("GET", "https://x/oauth-service/oauth/preauthorized", params, "cs", ""); got != "vuDWLkAGwPDSl8r0q3ly059MkjE=" {
			t.Fatalf("CL-222 signature %s", got)
		}
	})
}

func TestHeadersByKind(t *testing.T) {
	t.Run("CL-223 DI tokens send the native headers", func(t *testing.T) {
		h := Headers(&Tokens{DI: &DI{AccessToken: "a"}}, "a")
		if h.Get("X-Garmin-Client-Platform") != "Android" || h.Get("User-Agent") != "GCM-Android-5.23" || h.Get("Authorization") != "Bearer a" {
			t.Fatalf("CL-223 DI headers %v", h)
		}
	})
	t.Run("CL-224 legacy tokens send the connectmobile user agent", func(t *testing.T) {
		h := Headers(&Tokens{OAuth1: &OAuth1{}, OAuth2: &OAuth2{}, Consumer: &Consumer{}}, "b")
		if h.Get("User-Agent") != "com.garmin.android.apps.connectmobile" || h.Get("X-Garmin-Client-Platform") != "" || h.Get("Authorization") != "Bearer b" {
			t.Fatalf("CL-224 legacy headers %v", h)
		}
	})
}

func TestSocialProfileNeedsAnIssuedToken(t *testing.T) {
	t.Run("CL-225 the fake accepts only an issued token", func(t *testing.T) {
		srv := fakeGarmin(t)
		a := &Auth{DataDir: t.TempDir()}
		tok, err := a.Login(context.Background(), "e2e@example.invalid", "right-password")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			access string
			want   int
		}{{tok.DI.AccessToken, 200}, {"unknown", 401}} {
			req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL()+"/userprofile-service/socialProfile", nil)
			req.Header = Headers(tok, c.access)
			resp, err := a.client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("CL-225 token %q answered %d, want %d", c.access, resp.StatusCode, c.want)
			}
		}
	})
}
