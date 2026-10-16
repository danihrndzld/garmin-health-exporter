package garmin

import (
	"os"
	"testing"
)

func TestSummarizeTheFixture(t *testing.T) {
	t.Run("AC-605 the fixture gives 5 days ascending with the 5 fields as JSON, the first day's numbers, nulls where the path is missing, and the file's date_range", func(t *testing.T) {
		raw, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Summarize(raw)
		if err != nil || len(s.Days) != 5 {
			t.Fatalf("summary: %+v %v", s, err)
		}
		if start, end := s.Dates(); start != "2024-02-17" || end != "2024-02-21" {
			t.Fatalf("dates %s %s", start, end)
		}
		if string(s.Range) != `{"start":"2024-02-17","end":"2024-02-21","days":6}` { // the file's date_range as it is (jq checked 2026-10-16)
			t.Errorf("range %s", s.Range)
		}
		for i := 1; i < len(s.Days); i++ {
			if s.Days[i].Date < s.Days[i-1].Date {
				t.Fatal("days are not ascending")
			}
		}
		d := s.Days[0] // 2024-02-17 (jq checked 2026-10-16)
		if d.Date != "2024-02-17" || string(d.Steps) != "1403" || string(d.RHR) != "54" || string(d.SleepScore) != "84" || string(d.SleepSec) != "42146" || string(d.HRV) != "55" {
			t.Errorf("first day: %+v", d)
		}
		last := s.Days[4] // 2024-02-21: sleepTimeSeconds is null and sleepScores is absent
		if string(last.SleepSec) != "null" || string(last.SleepScore) != "null" {
			t.Errorf("last day: %+v", last)
		}
		// A day without a sleep section gives null, not a crash; a value of 0 stays 0.
		s2, _ := Summarize([]byte(`{"date_range":{"start":"2026-01-01","end":"2026-01-01"},"daily":{"2026-01-01":{"stats":{"totalSteps":0}}}}`))
		if string(s2.Days[0].SleepScore) != "null" || string(s2.Days[0].Steps) != "0" {
			t.Errorf("got %+v", s2.Days[0])
		}
	})
}

func TestSummarizeEdges(t *testing.T) {
	t.Run("EG-606 text that is not JSON is an error; a file without daily or date_range gives 0 days and a null range", func(t *testing.T) {
		if _, err := Summarize([]byte("{not json")); err == nil {
			t.Error("corrupt JSON accepted")
		}
		s, err := Summarize([]byte(`{}`))
		if err != nil || len(s.Days) != 0 || string(s.Range) != "null" {
			t.Errorf("got %+v %v", s, err)
		}
		if start, end := s.Dates(); start != "" || end != "" {
			t.Errorf("dates %q %q", start, end)
		}
	})
}
