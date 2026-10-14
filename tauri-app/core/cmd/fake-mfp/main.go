// fake-mfp serves the test fixtures on a free loopback port and prints its URL.
// Test helper only; it is never bundled with the app.
package main

import (
	"fmt"
	"os"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fake-mfp <testdata dir>")
		os.Exit(2)
	}
	srv := mfptest.NewServer(os.Args[1])
	fmt.Println(srv.URL)
	select {}
}
