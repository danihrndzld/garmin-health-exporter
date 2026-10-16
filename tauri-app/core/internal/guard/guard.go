// Package guard holds the checks that make this binary a trust boundary:
// paths must stay inside $HOME after symlinks, and ranges are short and valid.
package guard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const layout = "2006-01-02"

// InsideHome returns the resolved path when it is inside the user's home folder.
// A path that does not exist yet is resolved through its nearest existing parent.
func InsideHome(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	existing, rest := abs, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", errors.New("path has no existing parent")
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	resolved = filepath.Join(resolved, rest)
	// Compare with a trailing separator: /Users/dani-evil must not pass as /Users/dani.
	if resolved != home && !strings.HasPrefix(resolved, home+string(filepath.Separator)) {
		return "", errors.New("path must be within your home directory")
	}
	return resolved, nil
}

func ValidateRange(start, end string, maxDays int) error {
	s, err1 := time.Parse(layout, start)
	e, err2 := time.Parse(layout, end)
	if err1 != nil || err2 != nil {
		return errors.New("startDate and endDate must be YYYY-MM-DD")
	}
	if e.Before(s) {
		return errors.New("startDate must be on or before endDate")
	}
	if int(e.Sub(s).Hours()/24)+1 > maxDays {
		return fmt.Errorf("the range can be at most %d days", maxDays)
	}
	return nil
}

// Dates lists every date from start to end, inclusive, oldest first.
// It ignores parse errors: callers run ValidateRange first.
func Dates(start, end string) []string {
	s, _ := time.Parse(layout, start)
	e, _ := time.Parse(layout, end)
	var out []string
	for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(layout))
	}
	return out
}

// DaysBack returns the inclusive range that ends today (local time) and spans n days.
func DaysBack(n int, now time.Time) (start, end string, err error) {
	if n < 1 || n > 90 {
		return "", "", errors.New("daysBack must be an integer between 1 and 90")
	}
	today := now.In(time.Local)
	return today.AddDate(0, 0, -(n - 1)).Format(layout), today.Format(layout), nil
}

func RefreshWindow(n int) error {
	if n < 1 || n > 7 {
		return errors.New("refreshWindow must be an integer between 1 and 7")
	}
	return nil
}

// Credentials checks size only. Its errors are fixed sentences: they never echo the input.
func Credentials(email, password string) error {
	if email == "" || password == "" {
		return errors.New("email and password are required")
	}
	if len(email) > 320 {
		return errors.New("the email can be at most 320 bytes")
	}
	if len(password) > 1024 {
		return errors.New("the password can be at most 1024 bytes")
	}
	return nil
}

// NotFuture rejects an end date after today in local time. ISO dates compare as strings.
func NotFuture(end string, now time.Time) error {
	if end > now.In(time.Local).Format(layout) {
		return errors.New("the end date cannot be in the future")
	}
	return nil
}
