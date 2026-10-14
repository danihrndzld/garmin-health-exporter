// Package mfptest is a fake MyFitnessPal for tests: auth_token, nutrient-goals and
// v2/diary, answered from testdata files. Days without a file answer {"items":[]}.
package mfptest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type Server struct {
	URL          string
	Calls        []string
	AuthHTML     bool   // answer auth_token with HTML, like an expired session
	Diary429     bool   // answer every diary request with 429
	Diary401Once bool   // answer the next diary request with 401
	SetCookie    string // Set-Cookie header sent with auth_token, to test rotation
	AuthStatus   int    // when set, answer auth_token with this status and an HTML body (e.g. a 403 challenge)
	UserID       string // JSON value of user_id in the auth_token answer; default "42" (a string)
	mu           sync.Mutex
	userAgents   []string
	srv          *httptest.Server
}

var day = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func NewServer(dir string) *Server {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/user/auth_token", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if s.SetCookie != "" && (s.AuthHTML || s.AuthStatus != 0) {
			w.Header().Add("Set-Cookie", s.SetCookie) // an expired-session page may set cookies too
		}
		if s.AuthStatus != 0 {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(s.AuthStatus)
			w.Write([]byte("<html>Just a moment...</html>"))
			return
		}
		if s.AuthHTML || !hasSession(r) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>Log in</html>"))
			return
		}
		if s.SetCookie != "" {
			w.Header().Add("Set-Cookie", s.SetCookie)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"test-token","user_id":` + s.userID() + `,"expires_in":3600}`))
	})
	mux.HandleFunc("/v2/nutrient-goals", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		serve(w, filepath.Join(dir, "goals.json"))
	})
	mux.HandleFunc("/v2/diary", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("mfp-client-id") != "mfp-main-js" || r.Header.Get("mfp-user-id") != strings.Trim(s.userID(), `"`) {
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("types") != "food_entry,exercise_entry,steps_aggregate" {
			w.Write([]byte(`{"items":[]}`)) // the real API also answers an unknown type with nothing
			return
		}
		s.mu.Lock()
		once := s.Diary401Once
		s.Diary401Once = false
		s.mu.Unlock()
		if once {
			w.WriteHeader(401)
			return
		}
		if s.Diary429 {
			w.WriteHeader(429)
			return
		}
		d := r.URL.Query().Get("entry_date")
		if !day.MatchString(d) {
			w.WriteHeader(400)
			return
		}
		serve(w, filepath.Join(dir, "diary", d+".json"))
	})
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return s
}

func (s *Server) userID() string {
	if s.UserID == "" {
		return `"42"`
	}
	return s.UserID
}

func hasSession(r *http.Request) bool {
	c, err := r.Cookie("__Secure-next-auth.session-token")
	return err == nil && c.Value != ""
}

func serve(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Type", "application/json")
	b, err := os.ReadFile(path)
	if err != nil {
		b = []byte(`{"items":[]}`)
	}
	w.Write(b)
}

func (s *Server) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, r.URL.Path+"?"+r.URL.RawQuery)
	s.userAgents = append(s.userAgents, r.UserAgent())
}

// Agents returns the User-Agent of every request, in order.
func (s *Server) Agents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.userAgents...)
}

func (s *Server) Close() { s.srv.Close() }

// CallCount counts recorded requests whose path starts with prefix.
func (s *Server) CallCount(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.Calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}
