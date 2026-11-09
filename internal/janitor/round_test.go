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
