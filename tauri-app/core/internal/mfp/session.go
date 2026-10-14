package mfp

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const sessionCookie = "__Secure-next-auth.session-token"

// Cookie and Session match the JSON that Rust writes in mfp_session.json.
type Cookie struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Domain  string `json:"domain"`
	Path    string `json:"path"`
	Expires int64  `json:"expires"`
}

type Session struct {
	Cookies        []Cookie `json:"cookies"`
	UserAgent      string   `json:"user_agent"`
	SavedAt        string   `json:"saved_at"`
	SessionExpires int64    `json:"session_expires"`
}

func LoadSession(path string) (*Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrSession
	}
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return nil, ErrSession
	}
	for _, c := range s.Cookies {
		if c.Name == sessionCookie && c.Value != "" {
			return &s, nil
		}
	}
	return nil, ErrSession
}

// Save writes through a temporary file and a rename, so a killed process never
// leaves a half written session.
func (s *Session) Save(path string) error {
	s.SavedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// Merge applies Set-Cookie headers from MFP and reports whether anything changed.
func (s *Session) Merge(set []*http.Cookie) bool {
	changed := false
	for _, nc := range set {
		found := false
		for i := range s.Cookies {
			if s.Cookies[i].Name == nc.Name {
				found = true
				if s.Cookies[i].Value != nc.Value {
					s.Cookies[i].Value = nc.Value
					changed = true
				}
				if !nc.Expires.IsZero() {
					s.Cookies[i].Expires = nc.Expires.Unix()
				}
			}
		}
		if !found && nc.Value != "" {
			s.Cookies = append(s.Cookies, Cookie{Name: nc.Name, Value: nc.Value, Domain: ".myfitnesspal.com", Path: "/", Expires: nc.Expires.Unix()})
			changed = true
		}
		if nc.Name == sessionCookie && !nc.Expires.IsZero() {
			s.SessionExpires = nc.Expires.Unix()
		}
	}
	return changed
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(errors.New("could not replace "+filepath.Base(path)), err)
	}
	return nil
}
