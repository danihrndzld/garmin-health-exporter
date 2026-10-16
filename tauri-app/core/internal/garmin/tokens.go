// Package garmin signs in to Garmin Connect and exports its data. tokens.json keeps the
// exact shapes electron-app/garmin/auth.js writes, so Bun and Go read each other's files.
package garmin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/atomicfile"
)

type DI struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	ClientID              string `json:"client_id"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt *int64 `json:"refresh_token_expires_at"` // null when Garmin sends no lifetime
}

type OAuth1 struct {
	Token  string `json:"oauth_token"`
	Secret string `json:"oauth_token_secret"`
}

type OAuth2 struct {
	AccessToken           string `json:"access_token"`
	TokenType             string `json:"token_type"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt int64  `json:"refresh_token_expires_at"`
}

type Consumer struct {
	Key    string `json:"consumer_key"`
	Secret string `json:"consumer_secret"`
}

type Tokens struct {
	DI        *DI       `json:"di,omitempty"`
	OAuth1    *OAuth1   `json:"oauth1,omitempty"`
	OAuth2    *OAuth2   `json:"oauth2,omitempty"`
	Consumer  *Consumer `json:"consumer,omitempty"`
	SavedAt   string    `json:"saved_at,omitempty"`
	ExpiredAt string    `json:"expired_at,omitempty"`
}

type Status struct {
	Connected bool    `json:"connected"`
	Expired   bool    `json:"expired"`
	SavedAt   *string `json:"savedAt"`
	Kind      *string `json:"kind"`
}

var ErrNoLogin = errors.New("no Garmin login")

const tokensFile = "tokens.json"

func readRaw(dataDir string) (*Tokens, bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, tokensFile))
	if err != nil {
		return nil, false
	}
	var t Tokens
	if json.Unmarshal(b, &t) != nil {
		return nil, false
	}
	return &t, true
}

func (t *Tokens) isDI() bool     { return t.DI != nil && t.DI.AccessToken != "" }
func (t *Tokens) isLegacy() bool { return t.OAuth1 != nil && t.OAuth2 != nil && t.Consumer != nil }

// LoadTokens returns ErrNoLogin when the file is missing, corrupt, incomplete or an expired marker.
func LoadTokens(dataDir string) (*Tokens, error) {
	t, ok := readRaw(dataDir)
	if !ok || t.ExpiredAt != "" || !(t.isDI() || t.isLegacy()) {
		return nil, ErrNoLogin
	}
	return t, nil
}

// Save creates dataDir (0700) when missing and writes tokens.json atomically with mode 0600 and stamps SavedAt when empty.
func (t *Tokens) Save(dataDir string) error {
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
	return atomicfile.WriteFile(filepath.Join(dataDir, tokensFile), b)
}

// MarkExpired replaces the login with a marker that holds no secret.
func MarkExpired(dataDir string, now time.Time) error {
	m := Tokens{ExpiredAt: now.UTC().Format(time.RFC3339), SavedAt: now.UTC().Format(time.RFC3339)}
	if old, ok := readRaw(dataDir); ok && old.SavedAt != "" {
		m.SavedAt = old.SavedAt
	}
	return m.Save(dataDir)
}

// Delete removes tokens.json; a missing file is not an error.
func Delete(dataDir string) error {
	if err := os.Remove(filepath.Join(dataDir, tokensFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// StatusOf reads tokens.json once and never sends a request.
func StatusOf(dataDir string, now time.Time) Status {
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
	case t.isDI():
		kind("di")
		s.Expired = t.DI.RefreshToken == "" ||
			(t.DI.RefreshTokenExpiresAt != nil && *t.DI.RefreshTokenExpiresAt <= now.Unix())
	case t.isLegacy():
		kind("legacy") // the OAuth1 token mints new OAuth2 tokens, so old OAuth2 dates do not expire it
	}
	return s
}
