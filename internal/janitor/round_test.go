package janitor_test

import (
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/janitor"
	"github.com/aethera-deeptech/backend/internal/testutil"
)

// lockKey is the advisory lock of the janitor, the same number as in the package
const lockKey = 7261990001

// TestARoundLetsGoOfItsLockBeforeTheConnectionGoesBack runs a round on a real database and then takes the lock on three connections of
// the pool. A connection that still held the lock would make the next one fail, because the lock belongs to the session.
func TestARoundLetsGoOfItsLockBeforeTheConnectionGoesBack(t *testing.T) {
	e := testutil.New(t)

	task := janitor.Task{Name: "nothing", Query: "DELETE FROM rate_limit_events WHERE ctid IN (SELECT ctid FROM rate_limit_events WHERE false LIMIT $1)"}

	janitor.New(e.Pool, time.Hour, task).Round(t.Context())

	for i := range 3 {
		conn, err := e.Pool.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		defer conn.Release()

		var got bool
		if err := conn.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", int64(lockKey)).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if !got {
			t.Fatalf("connection %d could not take the lock after a round, so a round kept it", i)
		}

		if _, err := conn.Exec(t.Context(), "SELECT pg_advisory_unlock($1)", int64(lockKey)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestARoundWhoseConnectionDiesLeavesThePoolWorking ends the session of the round from inside the task. The unlock then fails, and the
// round has to close the connection and not give a dead one back, so the pool still answers afterwards.
func TestARoundWhoseConnectionDiesLeavesThePoolWorking(t *testing.T) {
	e := testutil.New(t)

	task := janitor.Task{Name: "ends its own session", Query: "SELECT pg_terminate_backend(pg_backend_pid()) WHERE $1::int > 0"}

	janitor.New(e.Pool, time.Hour, task).Round(t.Context())

	for range 3 {
		if err := e.Pool.Ping(t.Context()); err != nil {
			t.Fatalf("the pool does not answer after a round whose connection died %v", err)
		}
	}
}

func TestCloseBoundedClosesAHijackedConnection(t *testing.T) {
	e := testutil.New(t)

	conn, err := e.Pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	raw := conn.Hijack()

	janitor.CloseBounded(t.Context(), raw)

	if !raw.IsClosed() {
		t.Fatal("the connection is still open after the close")
	}
}
