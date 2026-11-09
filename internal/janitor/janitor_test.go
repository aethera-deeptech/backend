package janitor

import (
	"context"
	"errors"
	"testing"
)

// TestAPanicOfATaskIsAnErrorAndNotACrash runs a task on a nil connection, which panics inside the task, as a bug in a task would
func TestAPanicOfATaskIsAnErrorAndNotACrash(t *testing.T) {
	n, err := runContained(context.Background(), nil, Task{Name: "broken", Query: "DELETE FROM x"})
	if !errors.Is(err, errTaskPanicked) || n != 0 {
		t.Fatalf("got %d rows and error %v, want 0 rows and the panic error", n, err)
	}
}
