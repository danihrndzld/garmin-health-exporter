package garmin

import (
	"os"
	"regexp"
)

var loopback = regexp.MustCompile(`^http://(127\.0\.0\.1|localhost):\d+$`)

// FakeServer returns GARMIN_FAKE_SERVER when it is a loopback URL, else "".
// It is read on every request, so it can never redirect traffic elsewhere.
func FakeServer() string {
	v := os.Getenv("GARMIN_FAKE_SERVER")
	if loopback.MatchString(v) {
		return v
	}
	return ""
}
