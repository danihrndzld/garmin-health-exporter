package garmin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
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
	if err := (&tokenFile{Tokens: *tok}).save(dir); err != nil {
		t.Fatal(err)
	}
	t.Run("CL-211 tokens.json is saved with mode 0600", func(t *testing.T) {
		info, _ := os.Stat(filepath.Join(dir, "tokens.json"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	})
	t.Run("CL-212 a Go save keeps every DI and legacy key of auth.js, and a null refresh_token_expires_at stays null", func(t *testing.T) {
		var back map[string]map[string]any
		b, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
		_ = json.Unmarshal(b, &back)
		for _, k := range []string{"access_token", "refresh_token", "client_id", "expires_at", "refresh_token_expires_at"} {
			if _, ok := back["di"][k]; !ok {
				t.Errorf("di.%s missing after a Go save", k)
			}
		}
		// auth.js writes null when Garmin sends no refresh lifetime; the key must stay, as null.
		roundTrip := func(body string) map[string]map[string]any {
			d := t.TempDir()
			write(t, d, body)
			tok, err := LoadTokens(d)
			if err != nil {
				t.Fatal(err)
			}
			if err := (&tokenFile{Tokens: *tok}).save(d); err != nil {
				t.Fatal(err)
			}
			var out map[string]map[string]any
			_ = json.Unmarshal(tokensFileOf(t, d), &out)
			return out
		}
		di := roundTrip(strings.Replace(bunDI, `"refresh_token_expires_at":1763132000`, `"refresh_token_expires_at":null`, 1))["di"]
		if v, ok := di["refresh_token_expires_at"]; !ok || v != nil {
			t.Errorf("di.refresh_token_expires_at = %v (present %v), want null", v, ok)
		}
		legacy := roundTrip(bunLegacy)
		var want map[string]map[string]any
		_ = json.Unmarshal([]byte(bunLegacy), &want)
		for _, part := range []string{"oauth1", "oauth2", "consumer"} {
			if !reflect.DeepEqual(legacy[part], want[part]) {
				t.Errorf("%s = %v after a Go save, want %v", part, legacy[part], want[part])
			}
		}
		if _, ok := legacy["di"]; ok {
			t.Error("a legacy save wrote a di key")
		}
	})
}

func TestMarkExpiredDropsEverySecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, bunLegacy)
	if err := MarkExpired(dir, time.Unix(1760000000, 0)); err != nil {
		t.Fatal(err)
	}
	t.Run("EG-211 the expired marker holds no secret", func(t *testing.T) {
		b, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
		for _, secret := range []string{"legacy-1", "lr", "ot", "os", "cs"} {
			if strings.Contains(string(b), `"`+secret+`"`) {
				t.Errorf("marker still holds %q: %s", secret, b)
			}
		}
	})
	t.Run("EG-212 an expired marker does not load as a login", func(t *testing.T) {
		if _, err := LoadTokens(dir); err == nil {
			t.Error("LoadTokens accepted the expired marker")
		}
	})
}

func TestDeleteMissingFileIsFine(t *testing.T) {
	t.Parallel()
	t.Run("EG-213 deleting a missing tokens.json is not an error", func(t *testing.T) {
		if err := Delete(t.TempDir()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSaveCreatesDataDir(t *testing.T) {
	t.Parallel()
	t.Run("CL-221 save makes a missing dataDir with mode 0700", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "fresh", "data")
		if err := (&tokenFile{Tokens: gconnect.Tokens{DI: &gconnect.DI{AccessToken: "a"}}}).save(dir); err != nil {
			t.Fatalf("Save on a missing dataDir: %v", err)
		}
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("dataDir %v %v", info, err)
		}
	})
}
