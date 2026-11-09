package relay

import (
	"context"
	"errors"
	"testing"
	"time"
)

func clock(t *testing.T, s string) time.Time {
	t.Helper()

	v, err := time.Parse("15:04", s)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestNewestFindsTheLastSwitchTimeSinceTheLastLook(t *testing.T) {
	on := clock(t, "08:00")
	d := due{offAt: clock(t, "17:00"), onAt: &on, days: "mon-fri", zone: "UTC"}

	// Tuesday 3 November 2026
	tests := []struct {
		name    string
		checked time.Time
		now     time.Time
		want    string
	}{
		{"off after 17:00", time.Date(2026, 11, 3, 16, 58, 0, 0, time.UTC), time.Date(2026, 11, 3, 17, 1, 0, 0, time.UTC), Off},
		{"on after 08:00", time.Date(2026, 11, 3, 7, 58, 0, 0, time.UTC), time.Date(2026, 11, 3, 8, 1, 0, 0, time.UTC), On},
		{"nothing between", time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 10, 5, 0, 0, time.UTC), ""},
		{"the exact minute counts", time.Date(2026, 11, 3, 16, 59, 0, 0, time.UTC), time.Date(2026, 11, 3, 17, 0, 0, 0, time.UTC), Off},
		{"a time that was seen is not used again", time.Date(2026, 11, 3, 17, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 17, 5, 0, 0, time.UTC), ""},
		{"Saturday has no switch", time.Date(2026, 11, 7, 16, 58, 0, 0, time.UTC), time.Date(2026, 11, 7, 17, 1, 0, 0, time.UTC), ""},
		{"a look older than the life of a command is cut", time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 17, 30, 0, 0, time.UTC), ""},
		{"a look that is too old is cut", time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 17, 1, 0, 0, time.UTC), Off},
	}

	for _, tt := range tests {
		d.checked = tt.checked

		got, _ := newest(d, tt.now)
		if got != tt.want {
			t.Errorf("%s gave %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNewestTakesTheNewerOfTwoTimesInOneWindow(t *testing.T) {
	on := clock(t, "17:05")
	d := due{offAt: clock(t, "17:00"), onAt: &on, days: "daily", zone: "UTC", checked: time.Date(2026, 11, 3, 16, 58, 0, 0, time.UTC)}

	if got, _ := newest(d, time.Date(2026, 11, 3, 17, 6, 0, 0, time.UTC)); got != On {
		t.Fatalf("got %q, want on", got)
	}

	if got, _ := newest(d, time.Date(2026, 11, 3, 17, 4, 0, 0, time.UTC)); got != Off {
		t.Fatalf("got %q, want off", got)
	}
}

func TestNewestUsesTheTimeZoneOfTheBusiness(t *testing.T) {
	d := due{offAt: clock(t, "17:00"), days: "daily", zone: "Asia/Jakarta"}

	// 17:00 in Jakarta is 10:00 in UTC
	d.checked = time.Date(2026, 11, 3, 9, 58, 0, 0, time.UTC)

	if got, _ := newest(d, time.Date(2026, 11, 3, 10, 1, 0, 0, time.UTC)); got != Off {
		t.Fatalf("got %q, want off", got)
	}

	d.checked = time.Date(2026, 11, 3, 16, 58, 0, 0, time.UTC)

	if got, _ := newest(d, time.Date(2026, 11, 3, 17, 1, 0, 0, time.UTC)); got != "" {
		t.Fatalf("17:00 UTC is not a switch time in Jakarta, got %q", got)
	}
}

// TestAPanicOfAClaimIsAnErrorAndNotACrash makes a claim on a nil connection, which panics inside the claim, as a bug in a claim would
func TestAPanicOfAClaimIsAnErrorAndNotACrash(t *testing.T) {
	s := NewScheduler(nil, time.Minute)

	ok, err := s.claimContained(context.Background(), nil, due{}, "off", time.Now())
	if ok || !errors.Is(err, errClaimPanicked) {
		t.Fatalf("got %v and error %v, want false and the panic error", ok, err)
	}
}

// TestRunSurvivesARoundThatPanics gives the scheduler no pool, so the first round panics at once. Run must contain it and still stop
// when its context ends.
func TestRunSurvivesARoundThatPanics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})

	go func() {
		NewScheduler(nil, time.Hour).Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a round that panicked")
	}
}

func TestAFailedUnlockClosesTheConnectionAndASuccessfulOneDoesNot(t *testing.T) {
	var closed int

	discard := func(context.Context) { closed++ }

	unlockOrDiscard(context.Background(), func(context.Context) error { return errors.New("conn busy") }, discard)

	if closed != 1 {
		t.Fatalf("a failed unlock closed the connection %d times, want 1", closed)
	}

	unlockOrDiscard(context.Background(), func(context.Context) error { return nil }, discard)

	if closed != 1 {
		t.Fatalf("a successful unlock closed the connection, the count is %d", closed)
	}
}
