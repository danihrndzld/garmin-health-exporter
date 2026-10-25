package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/atomicfile"
	"github.com/danihrndzld/gconnect"
)

const (
	ssoCooldownS = 15 * 60 // after a 429 on a sign in, refuse new sign ins for this long
	cooldownFile = "sso-cooldown.json"
)

// CodeError is a failure the UI names by Code (GARMIN_INPUT, GARMIN_BAD_PASSWORD, ...).
type CodeError struct{ Code, Msg string }

func (e *CodeError) Error() string     { return e.Msg }
func (e *CodeError) ErrorCode() string { return e.Code }

var loginCodes = map[gconnect.LoginCode]string{
	gconnect.BadPassword: "GARMIN_BAD_PASSWORD",
	gconnect.MFARequired: "GARMIN_MFA",
	gconnect.RateLimited: "GARMIN_RATE_LIMITED",
	gconnect.Blocked:     "GARMIN_BLOCKED",
	gconnect.LoginFailed: "GARMIN_ERROR",
}

// loginErrorCode maps a gconnect sign in failure to its GARMIN code; a code this app does not
// know yet is GARMIN_ERROR, never an empty errorCode.
func loginErrorCode(c gconnect.LoginCode) string {
	if code, ok := loginCodes[c]; ok {
		return code
	}
	return "GARMIN_ERROR"
}

// Connect signs in once and saves tokens.json. A rate limited sign in writes
// sso-cooldown.json, and Connect sends nothing while it holds. Every failure is a *CodeError.
func Connect(ctx context.Context, a *gconnect.Auth, dataDir, email, password string, now time.Time) (Status, error) {
	if left := cooldownLeft(dataDir, now); left > 0 {
		return Status{}, &CodeError{"GARMIN_RATE_LIMITED", fmt.Sprintf("Garmin rate limited the last sign in. Wait %d more minutes before trying again.", (left+59)/60)}
	}
	t, err := a.Login(ctx, email, password)
	if err != nil {
		le := &gconnect.LoginError{Code: gconnect.LoginFailed, Msg: err.Error()}
		errors.As(err, &le)
		if le.Code == gconnect.RateLimited {
			startCooldown(dataDir, now)
		}
		msg := le.Msg
		if le.Code == gconnect.MFARequired { // the library's text is neutral; name this app
			msg = "This account uses two step verification, which Garmin Export does not support yet."
		}
		return Status{}, &CodeError{loginErrorCode(le.Code), msg}
	}
	if err := (&tokenFile{Tokens: *t}).save(dataDir); err != nil {
		return Status{}, &CodeError{"GARMIN_ERROR", "could not save the Garmin login: " + err.Error()}
	}
	return StatusOf(dataDir, now), nil
}

// keyRefusedMsg names where a new key comes from; Garmin shows a key only once.
const keyRefusedMsg = "Garmin did not accept this API key. Check it, or create a new one at connect.garmin.com/app/settings/personalApiToken."

// ConnectAPIKey trades the key for an access token once, which proves Garmin accepts it, and
// saves garmin-api-key.json. tokens.json is left alone. Every failure is a *CodeError.
func ConnectAPIKey(ctx context.Context, a *gconnect.Auth, dataDir, key string, now time.Time) (Status, error) {
	t, err := a.ExchangeAPIKey(ctx, key)
	if errors.Is(err, gconnect.ErrSessionDead) {
		return Status{}, &CodeError{"GARMIN_BAD_KEY", keyRefusedMsg}
	}
	if err != nil {
		return Status{}, &CodeError{"GARMIN_ERROR", err.Error()}
	}
	if err := (&tokenFile{Tokens: *t}).saveAs(dataDir, apiKeyFile); err != nil {
		return Status{}, &CodeError{"GARMIN_ERROR", "could not save the Garmin API key: " + err.Error()}
	}
	return StatusOf(dataDir, now), nil
}

// One process per export, so the cooldown after a 429 lives in a file Bun and Go share.
func cooldownLeft(dataDir string, now time.Time) int64 {
	var cd struct{ Until float64 }
	b, err := os.ReadFile(filepath.Join(dataDir, cooldownFile))
	if err != nil || json.Unmarshal(b, &cd) != nil {
		return 0
	}
	left := int64(cd.Until) - now.Unix()
	if left > ssoCooldownS {
		// A bad clock or edit cannot block longer: write the cap back, or the JS read-time cap
		// would block again on every later read.
		startCooldown(dataDir, now)
		return ssoCooldownS
	}
	return max(0, left)
}

func startCooldown(dataDir string, now time.Time) {
	b := []byte(`{"until":` + strconv.FormatInt(now.Unix()+ssoCooldownS, 10) + `}`)
	if os.MkdirAll(dataDir, 0o700) == nil {
		_ = atomicfile.WriteFile(filepath.Join(dataDir, cooldownFile), b) // best effort
	}
}
