// Package garmin is the app side of Garmin: tokens.json, garmin-api-key.json, the sign in
// cooldown and the export. tokens.json keeps the exact shapes electron-app/garmin/auth.js writes, so Bun and
// Go read each other's files. The Garmin API itself is in gconnect.
package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/atomicfile"
	"github.com/danihrndzld/gconnect"
)

// tokenFile is tokens.json: Garmin's tokens plus saved_at and the expired marker. The
// embedded fields marshal at the top level, so the bytes match auth.js.
type tokenFile struct {
	gconnect.Tokens
	SavedAt   string `json:"saved_at,omitempty"`
	ExpiredAt string `json:"expired_at,omitempty"`
}

// Status describes tokens.json (the email sign in) at the top level, as before the API key,
// and garmin-api-key.json under APIKey.
type Status struct {
	Connected bool      `json:"connected"`
	Expired   bool      `json:"expired"`
	SavedAt   *string   `json:"savedAt"`
	Kind      *string   `json:"loginKind"` // never "kind": the protocol sets that to "result"
	APIKey    KeyStatus `json:"apiKey"`
}

// KeyStatus describes garmin-api-key.json. Garmin does not say when a key expires, so
// Expired turns true only after Garmin refused it.
type KeyStatus struct {
	Connected bool    `json:"connected"`
	Expired   bool    `json:"expired"`
	SavedAt   *string `json:"savedAt"`
}

var ErrNoLogin = errors.New("no Garmin login")

const (
	tokensFile = "tokens.json"
	apiKeyFile = "garmin-api-key.json" // its own file: a dead key must never replace the email sign in
)

func readRaw(dataDir string) (*tokenFile, bool) { return readFile(dataDir, tokensFile) }

func readFile(dataDir, name string) (*tokenFile, bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, name))
	if err != nil {
		return nil, false
	}
	var t tokenFile
	if json.Unmarshal(b, &t) != nil {
		return nil, false
	}
	return &t, true
}

// loadFile returns ErrNoLogin when the file is missing, corrupt, incomplete or an expired marker.
func loadFile(dataDir string) (*tokenFile, error) {
	t, ok := readRaw(dataDir)
	if !ok || t.ExpiredAt != "" || t.Kind() == gconnect.KindNone {
		return nil, ErrNoLogin
	}
	return t, nil
}

// LoadTokens returns ErrNoLogin when the file is missing, corrupt, incomplete or an expired marker.
func LoadTokens(dataDir string) (*gconnect.Tokens, error) {
	t, err := loadFile(dataDir)
	if err != nil {
		return nil, err
	}
	return &t.Tokens, nil
}

func (t *tokenFile) save(dataDir string) error { return t.saveAs(dataDir, tokensFile) }

// saveAs creates dataDir (0700) when missing and writes name atomically with mode 0600 and stamps SavedAt when empty.
func (t *tokenFile) saveAs(dataDir, name string) error {
	if t.SavedAt == "" {
		t.SavedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(dataDir, name), b)
}

// MarkExpired replaces the login with a marker that holds no secret.
func MarkExpired(dataDir string, now time.Time) error { return markExpired(dataDir, tokensFile, now) }

func markExpired(dataDir, name string, now time.Time) error {
	m := tokenFile{ExpiredAt: now.UTC().Format(time.RFC3339), SavedAt: now.UTC().Format(time.RFC3339)}
	if old, ok := readFile(dataDir, name); ok && old.SavedAt != "" {
		m.SavedAt = old.SavedAt
	}
	return m.saveAs(dataDir, name)
}

// Delete removes tokens.json; a missing file is not an error.
func Delete(dataDir string) error { return remove(dataDir, tokensFile) }

// DeleteAPIKey removes garmin-api-key.json; a missing file is not an error.
func DeleteAPIKey(dataDir string) error { return remove(dataDir, apiKeyFile) }

func remove(dataDir, name string) error {
	if err := os.Remove(filepath.Join(dataDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// StatusOf reads tokens.json and garmin-api-key.json once each and never sends a request.
func StatusOf(dataDir string, now time.Time) Status {
	s := loginStatus(dataDir, now)
	if k, ok := readFile(dataDir, apiKeyFile); ok && (k.ExpiredAt != "" || k.Kind() == gconnect.KindPersonal) {
		s.APIKey = KeyStatus{Connected: true, Expired: k.ExpiredAt != ""}
		if k.SavedAt != "" {
			s.APIKey.SavedAt = &k.SavedAt
		}
	}
	return s
}

func loginStatus(dataDir string, now time.Time) Status {
	t, ok := readRaw(dataDir)
	if !ok {
		return Status{}
	}
	var s Status
	if t.SavedAt != "" {
		s.SavedAt = &t.SavedAt
	}
	kind := func(k string) { s.Connected, s.Kind = true, &k }
	switch {
	case t.ExpiredAt != "":
		s.Connected, s.Expired = true, true
	case t.Kind() == gconnect.KindDI:
		kind("di")
		s.Expired = t.DI.RefreshToken == "" ||
			(t.DI.RefreshTokenExpiresAt != nil && *t.DI.RefreshTokenExpiresAt <= now.Unix())
	case t.Kind() == gconnect.KindLegacy:
		kind("legacy") // the OAuth1 token mints new OAuth2 tokens, so old OAuth2 dates do not expire it
	}
	return s
}

const refreshMargin = 15 * time.Minute

// Fresh loads tokens.json and refreshes it refreshMargin before expiry, keeping saved_at.
// A failed refresh leaves tokens.json as it is; on gconnect.ErrSessionDead the caller writes
// the expired marker.
func Fresh(ctx context.Context, a *gconnect.Auth, dataDir string) (*gconnect.Tokens, error) {
	f, err := loadFile(dataDir)
	if err != nil {
		return nil, err
	}
	t, refreshed, err := a.Fresh(ctx, &f.Tokens, refreshMargin)
	if err != nil || !refreshed {
		return t, err
	}
	f.Tokens = *t
	if err := f.save(dataDir); err != nil {
		return nil, err
	}
	return t, nil
}
