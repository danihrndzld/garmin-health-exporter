package garmin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Written by electron-app/garmin/auth.js:_writeTokens; Go must read both shapes as they are.
const bunDI = `{"di":{"access_token":"a.b.c","refresh_token":"refresh-1","client_id":"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2","expires_at":1760540000,"refresh_token_expires_at":1763132000}}`
const bunLegacy = `{"oauth1":{"oauth_token":"ot","oauth_token_secret":"os"},"oauth2":{"access_token":"legacy-1","token_type":"Bearer","refresh_token":"lr","expires_at":1760540000,"refresh_token_expires_at":1763132000},"consumer":{"consumer_key":"ck","consumer_secret":"cs"}}`

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStatusDecisionTable(t *testing.T) {
	t.Parallel()
	now := time.Unix(1760000000, 0)
	cases := []struct {
		id, body           string
		connected, expired bool
		kind               string
	}{
		{"DT-R201 no file", "", false, false, ""},
		{"DT-R202 corrupt file", "{", false, false, ""},
		{"DT-R203 DI, refresh token valid", bunDI, true, false, "di"},
		{"DT-R204 DI, refresh token past its expiry", `{"di":{"access_token":"x","refresh_token":"r","client_id":"c","expires_at":1,"refresh_token_expires_at":1759999999}}`, true, true, "di"},
		{"DT-R205 DI without a refresh token", `{"di":{"access_token":"x","client_id":"c","expires_at":1}}`, true, true, "di"},
		{"DT-R206 DI with no known refresh lifetime", `{"di":{"access_token":"x","refresh_token":"r","client_id":"c","expires_at":1,"refresh_token_expires_at":null}}`, true, false, "di"},
		{"DT-R207 legacy, old OAuth2 dates do not expire it", bunLegacy, true, false, "legacy"},
		{"DT-R208 expired marker", `{"expired_at":"2026-10-01T00:00:00Z"}`, true, true, ""},
		{"DT-R209 legacy without consumer is incomplete", `{"oauth1":{"oauth_token":"ot","oauth_token_secret":"os"},"oauth2":{"access_token":"x"}}`, false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if c.body != "" {
				write(t, dir, c.body)
			}
			s := StatusOf(dir, now)
			kind := ""
			if s.Kind != nil {
				kind = *s.Kind
			}
			if s.Connected != c.connected || s.Expired != c.expired || kind != c.kind {
				t.Fatalf("got %+v kind=%q", s, kind)
			}
		})
	}
}

func TestSaveKeepsAuthJsShapeAndMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, bunDI)
	tok, err := LoadTokens(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tok.Save(dir); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "tokens.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("CL-211 mode %v", info.Mode().Perm())
	}
	var back map[string]map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	_ = json.Unmarshal(b, &back)
	for _, k := range []string{"access_token", "refresh_token", "client_id", "expires_at", "refresh_token_expires_at"} {
		if _, ok := back["di"][k]; !ok {
			t.Errorf("CL-212 di.%s missing after a Go save", k)
		}
	}
}

func TestMarkExpiredDropsEverySecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, bunLegacy)
	if err := MarkExpired(dir, time.Unix(1760000000, 0)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	for _, secret := range []string{"legacy-1", "lr", "ot", "os", "cs"} {
		if strings.Contains(string(b), `"`+secret+`"`) {
			t.Errorf("EG-211 marker still holds %q: %s", secret, b)
		}
	}
	if _, err := LoadTokens(dir); err == nil {
		t.Error("EG-212 an expired marker must not load as a login")
	}
}

func TestDeleteMissingFileIsFine(t *testing.T) {
	t.Parallel()
	if err := Delete(t.TempDir()); err != nil {
		t.Fatalf("EG-213 %v", err)
	}
}

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
		if got := FakeServer(); got != want {
			t.Errorf("%q -> %q", v, got)
		}
	}
}
