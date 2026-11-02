package report

import (
	"testing"
	"time"
)

func TestBoundsOfAWeek(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)

	tests := []struct {
		period  string
		start   time.Time
		wantErr bool
	}{
		{"2026-W01", time.Date(2025, 12, 29, 0, 0, 0, 0, wib), false},
		{"2026-W37", time.Date(2026, 9, 7, 0, 0, 0, 0, wib), false},
		{"2027-W53", time.Time{}, true},
		{"2026-W53", time.Date(2026, 12, 28, 0, 0, 0, 0, wib), false},
		{"2020-W53", time.Date(2020, 12, 28, 0, 0, 0, 0, wib), false},
	}

	for _, tt := range tests {
		start, end, err := bounds(tt.period, Week, wib)

		if tt.wantErr {
			if err == nil {
				t.Errorf("%s gave %v, want an error", tt.period, start)
			}

			continue
		}

		if err != nil || !start.Equal(tt.start) || !end.Equal(tt.start.AddDate(0, 0, 7)) || start.Weekday() != time.Monday {
			t.Errorf("%s gave %v to %v %v", tt.period, start, end, err)
		}
	}
}

func TestWeeksListTheNewestFirstAndStopAtTheFirstData(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, wib)
	first := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)

	got := weeks(&first, now, wib)
	want := []string{"2026-W45", "2026-W44", "2026-W43", "2026-W42"}

	if len(got) != len(want) {
		t.Fatalf("weeks are %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("weeks are %v, want %v", got, want)
		}
	}

	if only := weeks(nil, now, wib); len(only) != 1 || only[0] != "2026-W45" {
		t.Fatalf("with no data the weeks are %v", only)
	}
}

func TestAWeekAcrossTheNewYearUsesTheIsoYear(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)

	// 1 January 2027 is a Friday, so it is in the last week of 2026
	if got := weekLabel(weekStart(time.Date(2027, 1, 1, 12, 0, 0, 0, wib))); got != "2026-W53" {
		t.Fatalf("the label is %s, want 2026-W53", got)
	}
}
