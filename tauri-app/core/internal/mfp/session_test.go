package mfp

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSession(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string // "" means no file
		ok   bool
	}{
		{"EP-162 a file that does not exist is not a session", "", false},
		{"EP-163 a corrupt file is not a session", "{not json", false},
		{"EP-164 cookies without the session token are not a session", `{"cookies":[{"name":"x","value":"y"}]}`, false},
		{"EP-165 a session token with an empty value is not a session", `{"cookies":[{"name":"__Secure-next-auth.session-token","value":""}]}`, false},
		{"EP-166 a file with the session token is a session", `{"cookies":[{"name":"__Secure-next-auth.session-token","value":"abc"}],"user_agent":"UA"}`, true},
		{"EG-133 cookies set to null are not a session", `{"cookies":null,"user_agent":"UA"}`, false},
		{"EG-134 an empty JSON object is not a session", `{}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "mfp_session.json")
			if tc.body != "" {
				if err := os.WriteFile(p, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := LoadSession(p)
			if tc.ok != (err == nil) {
				t.Fatalf("session = %+v, err = %v", s, err)
			}
			if !tc.ok && err != ErrSession {
				t.Fatalf("err = %v, want ErrSession", err)
			}
		})
	}
}

func TestMergeSetCookie(t *testing.T) {
	t.Parallel()
	exp := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		set         []*http.Cookie
		wantChanged bool
		check       func(t *testing.T, s *Session)
	}{
		{"EP-167 a new value for a stored cookie replaces it", []*http.Cookie{{Name: sessionCookie, Value: "s2"}}, true,
			func(t *testing.T, s *Session) {
				if s.Cookies[0].Value != "s2" {
					t.Fatalf("cookies = %+v", s.Cookies)
				}
			}},
		{"EP-168 the same value is not a change, so no rewrite is needed", []*http.Cookie{{Name: sessionCookie, Value: "s1"}}, false,
			func(t *testing.T, s *Session) {}},
		{"EP-169 a cookie with a new name is appended", []*http.Cookie{{Name: "known_user", Value: "42"}}, true,
			func(t *testing.T, s *Session) {
				if len(s.Cookies) != 2 || s.Cookies[1].Name != "known_user" || s.Cookies[1].Domain != ".myfitnesspal.com" {
					t.Fatalf("cookies = %+v", s.Cookies)
				}
			}},
		{"EP-170 a deletion (MaxAge below zero) never blanks a stored cookie", []*http.Cookie{{Name: sessionCookie, Value: "", MaxAge: -1}}, false,
			func(t *testing.T, s *Session) {
				if s.Cookies[0].Value != "s1" {
					t.Fatalf("session blanked: %+v", s.Cookies)
				}
			}},
		{"EP-171 an empty value never blanks a stored cookie", []*http.Cookie{{Name: sessionCookie, Value: ""}}, false,
			func(t *testing.T, s *Session) {
				if s.Cookies[0].Value != "s1" {
					t.Fatalf("session blanked: %+v", s.Cookies)
				}
			}},
		{"EP-172 a rotated session token with Expires updates session_expires", []*http.Cookie{{Name: sessionCookie, Value: "s2", Expires: exp}}, true,
			func(t *testing.T, s *Session) {
				if s.SessionExpires != exp.Unix() || s.Cookies[0].Expires != exp.Unix() {
					t.Fatalf("session_expires = %d, cookie = %+v", s.SessionExpires, s.Cookies[0])
				}
			}},
		{"EG-135 a new cookie without Expires is stored as a session cookie (0)", []*http.Cookie{{Name: "other", Value: "v"}}, true,
			func(t *testing.T, s *Session) {
				if last := s.Cookies[len(s.Cookies)-1]; last.Name != "other" || last.Expires != 0 {
					t.Fatalf("new cookie = %+v", last)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := testSession()
			if got := s.Merge(tc.set); got != tc.wantChanged {
				t.Fatalf("Merge changed = %v, want %v", got, tc.wantChanged)
			}
			tc.check(t, s)
		})
	}
}

func TestSaveFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "mfp_session.json")
	s := testSession()
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	t.Run("CL-132 mfp_session.json has exactly the fields cookies, user_agent, saved_at, session_expires", func(t *testing.T) {
		b, _ := os.ReadFile(p)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"cookies", "user_agent", "saved_at", "session_expires"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("field %s missing in %s", k, b)
			}
		}
		if len(raw) != 4 {
			t.Errorf("unexpected fields in %s", b)
		}
	})
	t.Run("CL-133 mfp_session.json is mode 0600 and no temporary file stays behind", func(t *testing.T) {
		st, _ := os.Stat(p)
		entries, _ := os.ReadDir(dir)
		if st.Mode().Perm() != 0o600 || len(entries) != 1 {
			t.Fatalf("mode = %v, entries = %d", st.Mode().Perm(), len(entries))
		}
	})
}

func TestSaveKeepsTheOldFileWhenTheWriteFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "mfp_session.json")
	s := testSession()
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // the temporary file cannot be created
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	s.Cookies[0].Value = "changed"
	t.Run("EG-136 a failed write leaves the previous session loadable", func(t *testing.T) {
		if err := s.Save(p); err == nil {
			t.Skip("directory permissions are not enforced here (running as root?)")
		}
		back, err := LoadSession(p)
		if err != nil || back.Cookies[0].Value != "s1" {
			t.Fatalf("old file damaged: %+v %v", back, err)
		}
	})
}
