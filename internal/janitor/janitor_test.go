package janitor

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestAPanicOfATaskIsAnErrorAndNotACrash runs a task on a nil connection, which panics inside the task, as a bug in a task would
func TestAPanicOfATaskIsAnErrorAndNotACrash(t *testing.T) {
	n, err := runContained(context.Background(), nil, Task{Name: "broken", Query: "DELETE FROM x"})
	if !errors.Is(err, errTaskPanicked) || n != 0 {
		t.Fatalf("got %d rows and error %v, want 0 rows and the panic error", n, err)
	}
}

// TestRunSurvivesARoundThatPanics gives the janitor no pool, so the first round panics at once. Run must contain it and still stop
// when its context ends.
func TestRunSurvivesARoundThatPanics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})

	go func() {
		New(nil, time.Hour).Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a round that panicked")
	}
}
