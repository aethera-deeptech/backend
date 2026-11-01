package device

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestClassify(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	old := now.Add(-16 * time.Minute)

	tests := map[string]struct {
		last    *time.Time
		watts   *float64
		p95     *float64
		anomaly *time.Time
		want    string
	}{
		"no reading":              {want: StatusOffline},
		"silent for too long":     {last: &old, watts: ptr(300.0), want: StatusOffline},
		"offline beats anomaly":   {last: &old, watts: ptr(300.0), anomaly: &recent, want: StatusOffline},
		"anomaly":                 {last: &recent, watts: ptr(300.0), anomaly: &recent, want: StatusAnomaly},
		"old anomaly":             {last: &recent, watts: ptr(300.0), anomaly: ptr(now.Add(-25 * time.Hour)), want: StatusOn},
		"off":                     {last: &recent, watts: ptr(0.4), want: StatusOff},
		"standby":                 {last: &recent, watts: ptr(3.0), want: StatusStandby},
		"on without history":      {last: &recent, watts: ptr(9.0), want: StatusOn},
		"standby under the level": {last: &recent, watts: ptr(40.0), p95: ptr(400.0), want: StatusStandby},
		"on at the level":         {last: &recent, watts: ptr(80.0), p95: ptr(400.0), want: StatusOn},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := Classify(now, tt.last, tt.watts, tt.p95, tt.anomaly); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWorkingLevelNeverDropsUnderTheStandbyBand(t *testing.T) {
	if got := WorkingLevel(ptr(10.0)); got != StandbyMaxW {
		t.Fatalf("a weak device has the level %v", got)
	}

	if got := WorkingLevel(nil); got != StandbyMaxW {
		t.Fatalf("no history has the level %v", got)
	}

	if got := WorkingLevel(ptr(1000.0)); got != 200 {
		t.Fatalf("1000 W has the level %v", got)
	}
}

func TestSignal(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		age  time.Duration
		hops int
		want string
	}{
		"fresh and near":    {age: time.Minute, hops: 1, want: "baik"},
		"fresh but relayed": {age: time.Minute, hops: 3, want: "cukup"},
		"a few minutes":     {age: 4 * time.Minute, want: "cukup"},
		"late":              {age: 10 * time.Minute, want: "lemah"},
		"silent":            {age: 20 * time.Minute, want: "tidak-ada"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			at := now.Add(-tt.age)
			if got := Signal(now, &at, tt.hops); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}

	if Signal(now, nil, 0) != "tidak-ada" {
		t.Fatal("no reading must have no signal")
	}
}

func TestNiceCeiling(t *testing.T) {
	for in, want := range map[float64]float64{0: 100, 12.5: 20, 20: 20, 21: 50, 99: 100, 101: 200, 0.3: 0.5, 4800: 5000} {
		if got := niceCeiling(in); got != want {
			t.Fatalf("niceCeiling(%v) = %v, want %v", in, got, want)
		}
	}
}
