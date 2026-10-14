package mfp

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeNewCookieWithoutExpiresStoresZero(t *testing.T) {
	s := testSession()
	if !s.Merge([]*http.Cookie{{Name: "other", Value: "v"}}) {
		t.Fatal("new cookie not reported as changed")
	}
	if last := s.Cookies[len(s.Cookies)-1]; last.Name != "other" || last.Expires != 0 {
		t.Fatalf("new cookie = %+v", last)
	}
}

func TestMergeDeletionNeverBlanksTheSession(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mfp_session.json")
	s := testSession()
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if s.Merge([]*http.Cookie{{Name: sessionCookie, Value: "", MaxAge: -1}}) {
		t.Fatal("a deletion was reported as a change")
	}
	if s.Cookies[0].Value != "s1" {
		t.Fatalf("session blanked: %+v", s.Cookies)
	}
	back, err := LoadSession(p)
	if err != nil || back.Cookies[0].Value != "s1" {
		t.Fatalf("session not loadable: %+v %v", back, err)
	}
}

func TestLoadSessionMissingOrCorruptIsErrSession(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSession(filepath.Join(dir, "none.json")); err == nil {
		t.Fatal("missing file accepted")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	if _, err := LoadSession(bad); err == nil {
		t.Fatal("corrupt file accepted")
	}
	noToken := filepath.Join(dir, "notoken.json")
	os.WriteFile(noToken, []byte(`{"cookies":[{"name":"x","value":"y"}]}`), 0o600)
	if _, err := LoadSession(noToken); err == nil {
		t.Fatal("file without the session token accepted")
	}
}

func TestSaveKeepsTheOldFileWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mfp_session.json")
	s := testSession()
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o500) // the temporary file cannot be created
	defer os.Chmod(dir, 0o700)
	s.Cookies[0].Value = "changed"
	if err := s.Save(p); err == nil {
		t.Fatal("expected a write error")
	}
	back, err := LoadSession(p)
	if err != nil || back.Cookies[0].Value != "s1" {
		t.Fatalf("old file damaged: %+v %v", back, err)
	}
}
