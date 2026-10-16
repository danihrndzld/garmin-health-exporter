package mfp

import (
	"encoding/json"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/atomicfile"
	"net/http"
	"os"
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
	return atomicfile.WriteFile(path, b)
}

// Merge applies Set-Cookie headers from MFP and reports whether anything changed.
func (s *Session) Merge(set []*http.Cookie) bool {
	changed := false
	for _, nc := range set {
		// An empty value or a deletion (MaxAge < 0) never blanks a stored cookie.
		if nc.Value == "" || nc.MaxAge < 0 {
			continue
		}
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
		if !found {
			var exp int64 // 0 means a session cookie
			switch {
			case nc.MaxAge > 0:
				exp = time.Now().Add(time.Duration(nc.MaxAge) * time.Second).Unix()
			case !nc.Expires.IsZero():
				exp = nc.Expires.Unix()
			}
			s.Cookies = append(s.Cookies, Cookie{Name: nc.Name, Value: nc.Value, Domain: ".myfitnesspal.com", Path: "/", Expires: exp})
			changed = true
		}
		if nc.Name == sessionCookie && !nc.Expires.IsZero() {
			s.SessionExpires = nc.Expires.Unix()
		}
	}
	return changed
}
