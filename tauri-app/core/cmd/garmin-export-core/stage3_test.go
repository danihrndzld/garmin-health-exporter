package main

import (
	"strings"
	"testing"
)

func TestTZName(t *testing.T) {
	t.Run("EG-607 tzName is $TZ when set, else the zone behind /etc/localtime, never Local", func(t *testing.T) {
		t.Setenv("TZ", "America/Guatemala")
		if tzName() != "America/Guatemala" {
			t.Errorf("got %q", tzName())
		}
		t.Setenv("TZ", "")
		if n := tzName(); n == "" || n == "Local" {
			t.Errorf("got %q", n)
		}
	})
}

func TestWarningsCut(t *testing.T) {
	t.Run("BVA-603 warnings keep 200 of 201 items and 500 of 501 UTF-16 units; a missing or non list value is an empty list", func(t *testing.T) {
		list := make([]any, 201)
		for i := range list {
			list[i] = strings.Repeat("é", 501)
		}
		w := warnings(map[string]any{"warnings": list})
		if len(w) != 200 || len([]rune(w[0])) != 500 {
			t.Errorf("len %d, first has %d runes", len(w), len([]rune(w[0])))
		}
		for _, req := range []map[string]any{{}, {"warnings": "x"}, {"warnings": nil}} {
			if w := warnings(req); w == nil || len(w) != 0 {
				t.Errorf("%v -> %v", req, w)
			}
		}
	})
}
